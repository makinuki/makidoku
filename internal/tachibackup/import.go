package tachibackup

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/makinuki/makidoku/internal/chapterrecog"
	"github.com/makinuki/makidoku/internal/identity"
)

// Summary reports what an import wrote. Merged counts are titles that already
// existed and were updated in place; deferred counts are titles linked to a
// placeholder source because no installed source matched.
type Summary struct {
	Categories      int `json:"categories"`
	Manga           int `json:"manga"`
	MergedManga     int `json:"mergedManga"`
	DeferredManga   int `json:"deferredManga"`
	SkippedManga    int `json:"skippedManga"`
	OutOfLibrary    int `json:"outOfLibrary"`
	Chapters        int `json:"chapters"`
	ReadChapters    int `json:"readChapters"`
	History         int `json:"history"`
	ReadingSessions int `json:"readingSessions"`
	Tracking        int `json:"tracking"`
	SkippedTracking int `json:"skippedTracking"`
	Feeds           int `json:"feeds"`
	SavedSearches   int `json:"savedSearches"`
	Merges          int `json:"merges"`
	Metadata        int `json:"metadata"`
}

// importTarget is the resolved destination for one title's writes: the local
// source, the translation rules its identifiers follow, and its front page.
// A deferred target has no installed source, so identifiers stay verbatim.
type importTarget struct {
	sourceID string
	kind     keyKind
	baseURL  string
	deferred bool
}

// Progress reports how far an import has advanced. Phase names the stage and
// Processed of Total counts the units that stage has completed.
type Progress struct {
	Phase     string `json:"phase"`
	Processed int    `json:"processed"`
	Total     int    `json:"total"`
}

// ProgressFunc receives import progress. It is called on the importing
// goroutine, so it must not block.
type ProgressFunc func(Progress)

// Import applies a plan in one transaction. Re-importing the same file merges
// onto the existing library rather than duplicating it.
func Import(db *sqlx.DB, plan *Plan) (Summary, error) {
	return ImportWithProgress(db, plan, nil)
}

// ImportWithProgress applies a plan and reports per-title progress. Progress
// is advisory: a failure after it has been reported still rolls the
// transaction back.
func ImportWithProgress(db *sqlx.DB, plan *Plan, progress ProgressFunc) (Summary, error) {
	var summary Summary
	if plan == nil || plan.backup == nil {
		return summary, errors.New("import plan is empty")
	}
	tx, err := db.Beginx()
	if err != nil {
		return summary, fmt.Errorf("begin import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	categoryIDs, err := importCategories(tx, plan.backup, &summary)
	if err != nil {
		return summary, err
	}
	if err := importFeeds(tx, plan, &summary); err != nil {
		return summary, err
	}

	total := len(plan.backup.Manga)
	for index := range plan.backup.Manga {
		manga := &plan.backup.Manga[index]
		resolved := plan.sources[manga.Source]
		if resolved == nil || resolved.skip {
			summary.SkippedManga++
			reportProgress(progress, "manga", index+1, total)
			continue
		}
		if !manga.Favorite {
			// A title outside the library still carries chapters, read state
			// and history, so it is restored without being added to the
			// library view.
			if plan.skipOutOfLibrary {
				summary.SkippedManga++
				reportProgress(progress, "manga", index+1, total)
				continue
			}
			summary.OutOfLibrary++
		}
		target := importTarget{sourceID: resolved.ref.ID, kind: resolved.kind, baseURL: resolved.ref.BaseURL}
		if resolved.deferred {
			target.sourceID, err = placeholderSource(tx, manga.Source, resolved.name)
			if err != nil {
				return summary, err
			}
			target.deferred = true
			summary.DeferredManga++
		}
		if err := importManga(tx, plan, manga, target, categoryIDs, &summary); err != nil {
			return summary, fmt.Errorf("import %q: %w", manga.Title, err)
		}
		reportProgress(progress, "manga", index+1, total)
	}

	if err := tx.Commit(); err != nil {
		return summary, fmt.Errorf("commit import: %w", err)
	}
	return summary, nil
}

// reportProgress hands one progress sample to the caller when it wants them.
func reportProgress(progress ProgressFunc, phase string, processed, total int) {
	if progress == nil {
		return
	}
	progress(Progress{Phase: phase, Processed: processed, Total: total})
}

// importCategories creates or updates the backup's categories and returns a
// lookup from backup category order to local category id. The writer records
// the category order on each title rather than the category identifier, so the
// order is the key the per-title assignment needs.
func importCategories(tx *sqlx.Tx, backup *Backup, summary *Summary) (map[int64]int64, error) {
	out := map[int64]int64{}
	for _, category := range backup.Categories {
		name := strings.TrimSpace(category.Name)
		if name == "" {
			continue
		}
		var id int64
		err := tx.Get(&id, `SELECT id FROM categories WHERE name=?`, name)
		if errors.Is(err, sql.ErrNoRows) {
			result, insertErr := tx.Exec(`INSERT INTO categories(name,sort_order,hidden) VALUES(?,?,?)`, name, category.Order, boolFlag(category.Hidden))
			if insertErr != nil {
				return nil, fmt.Errorf("create category %q: %w", name, insertErr)
			}
			if id, err = result.LastInsertId(); err != nil {
				return nil, fmt.Errorf("create category %q: %w", name, err)
			}
			summary.Categories++
		} else if err != nil {
			return nil, fmt.Errorf("read category %q: %w", name, err)
		} else if _, err := tx.Exec(`UPDATE categories SET sort_order=?, hidden=? WHERE id=?`, category.Order, boolFlag(category.Hidden), id); err != nil {
			return nil, fmt.Errorf("update category %q: %w", name, err)
		}
		out[category.Order] = id
	}
	return out, nil
}

// placeholderSource creates or updates the uninstalled source row that holds
// titles whose original source is not installed. It never appears in the
// source list because the engine only serves installed sources.
func placeholderSource(tx *sqlx.Tx, backupSourceID int64, name string) (string, error) {
	id := fmt.Sprintf("imported-%d", backupSourceID)
	label := strings.TrimSpace(name)
	if label == "" {
		label = fmt.Sprintf("Source %d", backupSourceID)
	}
	_, err := tx.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed,installed_at)
		VALUES(?,NULL,?,?,?,?,?,NULL,0,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name WHERE sources.installed=0`,
		id, label+" (imported)", "0", 1, "en", "", time.Now().Unix())
	if err != nil {
		return "", fmt.Errorf("create placeholder source: %w", err)
	}
	return id, nil
}

func importManga(tx *sqlx.Tx, plan *Plan, manga *Manga, target importTarget, categoryOrders map[int64]int64, summary *Summary) error {
	title := strings.TrimSpace(manga.Title)
	if title == "" {
		title = manga.URL
	}
	// The recorded locator belongs to the writing application; an installed
	// source expects its own identifier. A deferred title keeps the recorded
	// locator because there is nothing to translate it for yet.
	seriesID := manga.URL
	pageURL := strings.TrimSpace(manga.URL)
	if !target.deferred {
		seriesID = seriesKey(target.kind, manga.URL)
		pageURL = seriesPageURL(target.kind, manga.URL, seriesID, target.baseURL)
	}
	sourceID := target.sourceID
	mode, direction, fit := readerOverrides(manga)
	now := time.Now().Unix()
	createdAt := millisToSeconds(manga.DateAdded)
	if createdAt == 0 {
		createdAt = now
	}
	updatedAt := millisToSeconds(manga.LastModifiedAt)
	if updatedAt == 0 {
		updatedAt = createdAt
	}
	status := mangaStatus(manga.Status)
	genres := jsonArray(manga.Genre)
	libraryFlag := 0
	if manga.Favorite {
		libraryFlag = 1
	}
	// The custom fields are user overrides. A custom status of zero means the
	// writer carries no override, so it stays NULL.
	customTitle := optional(manga.CustomTitle)
	customArtist := optional(manga.CustomArtist)
	customAuthor := optional(manga.CustomAuthor)
	customDescription := optional(manga.CustomDescription)
	customGenres := jsonArray(manga.CustomGenre)
	customCover := optional(manga.CustomThumbnailURL)
	var customStatus *string
	if manga.CustomStatus != 0 {
		customStatus = stringPointer(mangaStatus(manga.CustomStatus))
	}
	notes := optional(manga.Notes)
	memo := memoText(manga.Memo)
	sourceVersion := int64Value(manga.Version)
	updateStrategy := updateStrategyText(manga.UpdateStrategy)
	favoriteModified := pointerSeconds(manga.FavoriteModifiedAt)
	excludedScanlators := jsonArray(manga.ExcludedScanlators)
	chapterFlags := int64Value(int64(manga.ChapterFlags))

	var mangaID string
	err := tx.Get(&mangaID, `SELECT manga_id FROM manga_sources WHERE source_id=? AND source_manga_id=?`, sourceID, seriesID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if mangaID, err = identity.New(); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manga(id,source_id,title,alt_titles,description,authors,artists,genres,status,cover_url,
			in_library,download_format,download_new_chapters,reader_mode,reader_direction,reader_fit,created_at,updated_at,
			custom_title,custom_artist,custom_author,custom_description,custom_genres,custom_status,custom_cover_url,
			notes,memo,source_version,update_strategy,favorite_modified_at,initialized,excluded_scanlators,chapter_flags)
			VALUES(?,?,?,NULL,?,?,?,?,?,?,?,'cbz',0,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			mangaID, sourceID, title, optional(manga.Description), jsonArray([]string{manga.Author}), jsonArray([]string{manga.Artist}), genres, status, manga.ThumbnailURL,
			libraryFlag, mode, direction, fit, createdAt, updatedAt,
			customTitle, customArtist, customAuthor, customDescription, customGenres, customStatus, customCover,
			notes, memo, sourceVersion, updateStrategy, favoriteModified, manga.Initialized, excludedScanlators, chapterFlags); err != nil {
			return fmt.Errorf("create manga: %w", err)
		}
	default:
		if err != nil {
			return err
		}
		summary.MergedManga++
		if _, err := tx.Exec(`UPDATE manga SET title=?, description=COALESCE(?,description), authors=COALESCE(?,authors),
			artists=COALESCE(?,artists), genres=COALESCE(?,genres), status=?, cover_url=CASE WHEN ?<>'' THEN ? ELSE cover_url END,
			in_library=MAX(in_library,?), reader_mode=COALESCE(?,reader_mode), reader_direction=COALESCE(?,reader_direction),
			reader_fit=COALESCE(?,reader_fit), updated_at=?,
			custom_title=COALESCE(?,custom_title), custom_artist=COALESCE(?,custom_artist), custom_author=COALESCE(?,custom_author),
			custom_description=COALESCE(?,custom_description), custom_genres=COALESCE(?,custom_genres), custom_status=COALESCE(?,custom_status),
			custom_cover_url=COALESCE(?,custom_cover_url), notes=COALESCE(?,notes), memo=COALESCE(?,memo),
			source_version=COALESCE(?,source_version), update_strategy=COALESCE(?,update_strategy),
			favorite_modified_at=COALESCE(?,favorite_modified_at), initialized=MAX(initialized,?),
			excluded_scanlators=COALESCE(?,excluded_scanlators), chapter_flags=COALESCE(?,chapter_flags)
			WHERE id=?`,
			title, optional(manga.Description), jsonArray([]string{manga.Author}), jsonArray([]string{manga.Artist}), genres, status,
			manga.ThumbnailURL, manga.ThumbnailURL, libraryFlag, mode, direction, fit, updatedAt,
			customTitle, customArtist, customAuthor, customDescription, customGenres, customStatus, customCover,
			notes, memo, sourceVersion, updateStrategy, favoriteModified, manga.Initialized, excludedScanlators, chapterFlags, mangaID); err != nil {
			return fmt.Errorf("update manga: %w", err)
		}
	}
	summary.Manga++

	primary := 1
	var primaryCount int
	if err := tx.Get(&primaryCount, `SELECT COUNT(*) FROM manga_sources WHERE manga_id=? AND is_primary=1`, mangaID); err != nil {
		return err
	}
	if primaryCount > 0 {
		primary = 0
	}
	if _, err := tx.Exec(`INSERT INTO manga_sources(manga_id,source_id,source_manga_id,url,is_primary,first_seen_at,last_seen_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(source_id,source_manga_id) DO UPDATE SET last_seen_at=excluded.last_seen_at`,
		mangaID, sourceID, seriesID, optional(pageURL), primary, now, now); err != nil {
		return fmt.Errorf("link manga source: %w", err)
	}
	// The backup records category order on each title, not the category id.
	// A title with no categories field carries no assignment.
	for _, categoryOrder := range manga.Categories {
		localID, ok := categoryOrders[categoryOrder]
		if !ok {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_categories(manga_id,category_id) VALUES(?,?)`, mangaID, localID); err != nil {
			return fmt.Errorf("link category: %w", err)
		}
	}

	chapterIDs, err := importChapters(tx, manga, mangaID, target, seriesID, now, summary)
	if err != nil {
		return err
	}
	if err := importHistory(tx, manga, mangaID, chapterIDs, summary); err != nil {
		return err
	}
	if err := importTracking(tx, manga, mangaID, summary); err != nil {
		return err
	}
	if err := importMerges(tx, plan, manga, mangaID, now, summary); err != nil {
		return err
	}
	if err := importFlatMetadata(tx, manga, mangaID, summary); err != nil {
		return err
	}
	return nil
}

// importChapters writes the chapters a title carries and returns a lookup from
// the recorded chapter locator to the local chapter id. seriesID is the
// translated series identifier, which a source-specific chapter key may embed.
func importChapters(tx *sqlx.Tx, manga *Manga, mangaID string, target importTarget, seriesID string, now int64, summary *Summary) (map[string]string, error) {
	chapterIDs := map[string]string{}
	sourceID := target.sourceID
	for _, chapter := range manga.Chapters {
		url := strings.TrimSpace(chapter.URL)
		if url == "" {
			continue
		}
		key := url
		if !target.deferred {
			key = chapterKey(target.kind, chapter.URL, seriesID, target.baseURL)
		}
		var chapterID string
		err := tx.Get(&chapterID, `SELECT chapter_id FROM chapter_sources WHERE source_id=? AND source_chapter_id=?`, sourceID, key)
		if errors.Is(err, sql.ErrNoRows) {
			if chapterID, err = identity.New(); err != nil {
				return nil, err
			}
			var declared *float64
			if chapter.ChapterNumber > 0 {
				value := float64(chapter.ChapterNumber)
				declared = &value
			}
			number := chapterrecog.Parse(manga.Title, chapter.Name, declared)
			var uploadedAt *int64
			if stamp := millisToSeconds(chapter.DateUpload); stamp > 0 {
				uploadedAt = &stamp
			}
			bookmark := 0
			if chapter.Bookmark {
				bookmark = 1
			}
			if _, err := tx.Exec(`INSERT INTO chapters(id,manga_id,source_id,chapter_number,volume,title,language,uploaded_at,scanlator,downloaded,download_path,
			bookmark,source_order,source_version,source_last_modified_at,source_fetched_at,memo)
			VALUES(?,?,?,?,NULL,?,NULL,?,?,0,NULL,?,?,?,?,?,?)`,
				chapterID, mangaID, sourceID, number, optional(chapter.Name), uploadedAt, optional(chapter.Scanlator),
				bookmark, int64Value(chapter.SourceOrder), int64Value(chapter.Version),
				derefSeconds(chapter.LastModifiedAt), derefSeconds(chapter.DateFetch), memoText(chapter.Memo)); err != nil {
				return nil, fmt.Errorf("create chapter: %w", err)
			}
			if _, err := tx.Exec(`INSERT INTO chapter_sources(chapter_id,source_id,source_chapter_id,first_seen_at,last_seen_at)
			VALUES(?,?,?,?,?)`, chapterID, sourceID, key, now, now); err != nil {
				return nil, fmt.Errorf("link chapter source: %w", err)
			}
			summary.Chapters++
		} else if err != nil {
			return nil, err
		} else {
			// Merge onto a stored chapter the way the writing application does: a
			// bookmark is sticky, the highest source order wins, and the earliest
			// upload date is kept. The remaining source fields fill gaps.
			var uploadedAt *int64
			if stamp := millisToSeconds(chapter.DateUpload); stamp > 0 {
				uploadedAt = &stamp
			}
			bookmark := 0
			if chapter.Bookmark {
				bookmark = 1
			}
			if _, err := tx.Exec(`UPDATE chapters SET
			bookmark=MAX(bookmark,?), source_order=MAX(COALESCE(source_order,0),?),
			source_version=COALESCE(source_version,?), source_last_modified_at=COALESCE(source_last_modified_at,?),
			source_fetched_at=COALESCE(source_fetched_at,?), memo=COALESCE(memo,?),
			uploaded_at=CASE WHEN ? IS NULL THEN uploaded_at WHEN uploaded_at IS NULL THEN ? WHEN ?<uploaded_at THEN ? ELSE uploaded_at END
			WHERE id=?`,
				bookmark, chapter.SourceOrder, chapter.Version, derefSeconds(chapter.LastModifiedAt),
				derefSeconds(chapter.DateFetch), memoText(chapter.Memo),
				uploadedAt, uploadedAt, uploadedAt, uploadedAt, chapterID); err != nil {
				return nil, fmt.Errorf("merge chapter: %w", err)
			}
		}
		chapterIDs[url] = chapterID

		if !chapter.Read {
			continue
		}
		var readAt *int64
		if stamp := historyStamp(manga, url); stamp > 0 {
			readAt = &stamp
		}
		if _, err := tx.Exec(`INSERT INTO chapter_read_state(chapter_id,manga_id,read,read_at)
			VALUES(?,?,1,?) ON CONFLICT(chapter_id) DO UPDATE SET read=1, read_at=COALESCE(excluded.read_at, chapter_read_state.read_at)`,
			chapterID, mangaID, readAt); err != nil {
			return nil, fmt.Errorf("mark chapter read: %w", err)
		}
		summary.ReadChapters++
	}
	return chapterIDs, nil
}

func importHistory(tx *sqlx.Tx, manga *Manga, mangaID string, chapterIDs map[string]string, summary *Summary) error {
	resumeChapter := ""
	var resumeAt int64
	var resumePage int64
	pages := map[string]int64{}
	for _, chapter := range manga.Chapters {
		pages[chapter.URL] = chapter.LastPageRead
	}
	for _, entry := range manga.History {
		if entry.URL == "" {
			continue
		}
		if _, ok := chapterIDs[entry.URL]; !ok {
			continue
		}
		occurredAt := millisToSeconds(entry.LastRead)
		if occurredAt > 0 {
			inserted, err := insertHistoryEvent(tx, mangaID, chapterIDs[entry.URL], occurredAt)
			if err != nil {
				return err
			}
			if inserted {
				summary.History++
			}
		}
		if seconds := entry.ReadDuration / 1000; seconds > 0 && occurredAt > 0 {
			inserted, err := insertReadingSession(tx, mangaID, seconds, occurredAt)
			if err != nil {
				return err
			}
			if inserted {
				summary.ReadingSessions++
			}
		}
		if entry.LastRead > resumeAt {
			resumeAt = entry.LastRead
			resumeChapter = entry.URL
			resumePage = pages[entry.URL]
		}
	}
	if resumeChapter == "" {
		for _, chapter := range manga.Chapters {
			if chapter.LastPageRead > resumePage || (resumeChapter == "" && chapter.LastPageRead > 0) {
				resumePage = chapter.LastPageRead
				resumeChapter = chapter.URL
			}
		}
	}
	if resumeChapter == "" {
		return nil
	}
	chapterID, ok := chapterIDs[resumeChapter]
	if !ok {
		return nil
	}
	page := resumePage + 1
	if page < 1 {
		page = 1
	}
	lastReadAt := millisToSeconds(resumeAt)
	if lastReadAt == 0 {
		lastReadAt = time.Now().Unix()
	}
	if _, err := tx.Exec(`INSERT INTO reading_progress(manga_id,last_read_chapter_id,last_read_page,total_pages,is_completed,last_read_at)
		VALUES(?,?,?,0,0,?)
		ON CONFLICT(manga_id) DO UPDATE SET last_read_chapter_id=excluded.last_read_chapter_id,
			last_read_page=excluded.last_read_page, last_read_at=MAX(excluded.last_read_at, reading_progress.last_read_at)`,
		mangaID, chapterID, page, lastReadAt); err != nil {
		return fmt.Errorf("restore reading progress: %w", err)
	}
	return nil
}

func insertHistoryEvent(tx *sqlx.Tx, mangaID, chapterID string, occurredAt int64) (bool, error) {
	var existing int
	if err := tx.Get(&existing, `SELECT COUNT(*) FROM history_events WHERE manga_id=? AND chapter_id=? AND occurred_at=?`, mangaID, chapterID, occurredAt); err != nil {
		return false, err
	}
	if existing > 0 {
		return false, nil
	}
	id, err := identity.New()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO history_events(id,manga_id,chapter_id,page,occurred_at) VALUES(?,?,?,NULL,?)`, id, mangaID, chapterID, occurredAt); err != nil {
		return false, err
	}
	return true, nil
}

func insertReadingSession(tx *sqlx.Tx, mangaID string, seconds, occurredAt int64) (bool, error) {
	var existing int
	if err := tx.Get(&existing, `SELECT COUNT(*) FROM reading_sessions WHERE manga_id=? AND occurred_at=?`, mangaID, occurredAt); err != nil {
		return false, err
	}
	if existing > 0 {
		return false, nil
	}
	id, err := identity.New()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO reading_sessions(id,manga_id,seconds,occurred_at) VALUES(?,?,?,?)`, id, mangaID, seconds, occurredAt); err != nil {
		return false, err
	}
	return true, nil
}

func importTracking(tx *sqlx.Tx, manga *Manga, mangaID string, summary *Summary) error {
	for _, tracking := range manga.Tracking {
		trackerType, ok := TrackerTypeName(tracking.SyncID)
		if !ok {
			summary.SkippedTracking++
			continue
		}
		// The writer prefers its legacy 32-bit identifier when it is set.
		remoteID := strconv.FormatInt(int64(tracking.MediaIDInt), 10)
		if tracking.MediaIDInt == 0 {
			remoteID = strconv.FormatInt(tracking.MediaID, 10)
		}
		if remoteID == "0" || remoteID == "" {
			summary.SkippedTracking++
			continue
		}
		score := float64(tracking.Score)
		var scoreValue *float64
		if score > 0 {
			scoreValue = &score
		}
		var total *int
		if tracking.TotalChapters > 0 {
			value := int(tracking.TotalChapters)
			total = &value
		}
		var existing int
		if err := tx.Get(&existing, `SELECT COUNT(*) FROM tracker_bindings WHERE manga_id=? AND tracker_type=?`, mangaID, trackerType); err != nil {
			return err
		}
		var remoteLibraryID *string
		if tracking.LibraryID != 0 {
			value := strconv.FormatInt(tracking.LibraryID, 10)
			remoteLibraryID = &value
		}
		if _, err := tx.Exec(`INSERT INTO tracker_bindings(manga_id,tracker_type,remote_id,remote_title,remote_score,remote_status,last_synced_chapter,total_remote_chapters,started_at,finished_at,remote_url,remote_library_id,is_private)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(manga_id,tracker_type) DO UPDATE SET remote_id=excluded.remote_id, remote_title=excluded.remote_title,
				remote_score=excluded.remote_score, remote_status=excluded.remote_status, last_synced_chapter=excluded.last_synced_chapter,
				total_remote_chapters=excluded.total_remote_chapters, started_at=excluded.started_at, finished_at=excluded.finished_at,
				remote_url=COALESCE(NULLIF(excluded.remote_url,''),remote_url), remote_library_id=COALESCE(excluded.remote_library_id,remote_library_id),
				is_private=MAX(is_private,excluded.is_private)`,
			mangaID, trackerType, remoteID, tracking.Title, scoreValue, trackerStatus(trackerType, tracking.Status),
			float64(tracking.LastChapterRead), total,
			derefSeconds(tracking.StartedReadingDate), derefSeconds(tracking.FinishedReadingDate),
			tracking.TrackingURL, remoteLibraryID, boolFlag(tracking.Private)); err != nil {
			return fmt.Errorf("restore tracker binding: %w", err)
		}
		if existing == 0 {
			summary.Tracking++
		}
	}
	return nil
}

// localSource resolves a backup source identifier to a local source row,
// creating the placeholder row an uninstalled source needs. The second result
// is false when the plan drops that source.
func (p *Plan) localSource(tx *sqlx.Tx, backupSourceID int64) (string, bool, error) {
	resolved := p.sources[backupSourceID]
	if resolved == nil || resolved.skip {
		return "", false, nil
	}
	if resolved.deferred {
		id, err := placeholderSource(tx, backupSourceID, resolved.name)
		if err != nil {
			return "", false, err
		}
		return id, true, nil
	}
	return resolved.ref.ID, true, nil
}

// importFeeds restores the browse feeds and saved searches the backup carries.
// A feed is matched on its source and position, and a saved search on its
// source, name and query, so a re-import updates rather than duplicates them.
func importFeeds(tx *sqlx.Tx, plan *Plan, summary *Summary) error {
	for index, feed := range plan.backup.Feeds {
		sourceID, ok, err := plan.localSource(tx, feed.Source)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		var feedID string
		err = tx.Get(&feedID, `SELECT id FROM source_feeds WHERE source_id=? AND feed_order=?`, sourceID, index)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if feedID, err = identity.New(); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO source_feeds(id,source_id,is_global,feed_order) VALUES(?,?,?,?)`,
				feedID, sourceID, boolFlag(feed.Global), index); err != nil {
				return fmt.Errorf("create feed: %w", err)
			}
			summary.Feeds++
		case err != nil:
			return err
		default:
			if _, err := tx.Exec(`UPDATE source_feeds SET is_global=? WHERE id=?`, boolFlag(feed.Global), feedID); err != nil {
				return fmt.Errorf("update feed: %w", err)
			}
		}
		if feed.SavedSearch == nil {
			continue
		}
		created, err := importSavedSearch(tx, sourceID, &feedID, index, *feed.SavedSearch)
		if err != nil {
			return err
		}
		if created {
			summary.SavedSearches++
		}
	}
	for index, search := range plan.backup.SavedSearches {
		sourceID, ok, err := plan.localSource(tx, search.Source)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		created, err := importSavedSearch(tx, sourceID, nil, index, search)
		if err != nil {
			return err
		}
		if created {
			summary.SavedSearches++
		}
	}
	return nil
}

// importSavedSearch stores one saved search and reports whether it created a
// new row. The source's filter encoding is opaque and is stored verbatim.
func importSavedSearch(tx *sqlx.Tx, sourceID string, feedID *string, order int, search SavedSearch) (bool, error) {
	name := strings.TrimSpace(search.Name)
	if name == "" {
		return false, nil
	}
	filters := strings.TrimSpace(search.FilterList)
	if filters == "" {
		filters = "[]"
	}
	var existing string
	err := tx.Get(&existing, `SELECT id FROM saved_searches WHERE source_id=? AND name=? AND query=?`, sourceID, name, search.Query)
	if err == nil {
		if _, err := tx.Exec(`UPDATE saved_searches SET feed_id=COALESCE(?,feed_id), filters=?, search_order=? WHERE id=?`,
			feedID, filters, order, existing); err != nil {
			return false, fmt.Errorf("update saved search: %w", err)
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	id, err := identity.New()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO saved_searches(id,source_id,feed_id,name,query,filters,search_order) VALUES(?,?,?,?,?,?,?)`,
		id, sourceID, feedID, name, search.Query, filters, order); err != nil {
		return false, fmt.Errorf("create saved search: %w", err)
	}
	return true, nil
}

// importMerges records the additional sources merged into a title and links
// them through manga_sources so the refresh path pulls their chapters. The
// source-side locator is the merged manga url, falling back to the merge url.
func importMerges(tx *sqlx.Tx, plan *Plan, manga *Manga, mangaID string, now int64, summary *Summary) error {
	for index, reference := range manga.MergedReferences {
		sourceID, ok, err := plan.localSource(tx, reference.MangaSourceID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		sourceMangaID := strings.TrimSpace(reference.MangaURL)
		if sourceMangaID == "" {
			sourceMangaID = strings.TrimSpace(reference.MergeURL)
		}
		if sourceMangaID == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO manga_sources(manga_id,source_id,source_manga_id,url,is_primary,first_seen_at,last_seen_at)
			VALUES(?,?,?,?,0,?,?)
			ON CONFLICT(source_id,source_manga_id) DO UPDATE SET last_seen_at=excluded.last_seen_at, url=COALESCE(NULLIF(excluded.url,''),url)`,
			mangaID, sourceID, sourceMangaID, optional(reference.MergeURL), now, now); err != nil {
			return fmt.Errorf("link merged source: %w", err)
		}
		var existing string
		err = tx.Get(&existing, `SELECT id FROM manga_merges WHERE manga_id=? AND source_id=? AND source_manga_id=?`, mangaID, sourceID, sourceMangaID)
		if err == nil {
			if _, err := tx.Exec(`UPDATE manga_merges SET url=?, is_info_manga=?, get_chapter_updates=?, chapter_sort_mode=?,
				chapter_priority=?, download_chapters=?, merge_order=? WHERE id=?`,
				optional(reference.MergeURL), boolFlag(reference.IsInfoManga), boolFlag(reference.GetChapterUpdates),
				reference.ChapterSortMode, reference.ChapterPriority, boolFlag(reference.DownloadChapters), index, existing); err != nil {
				return fmt.Errorf("update merge: %w", err)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, err := identity.New()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manga_merges(id,manga_id,source_id,source_manga_id,url,is_info_manga,get_chapter_updates,
			chapter_sort_mode,chapter_priority,download_chapters,merge_order) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			id, mangaID, sourceID, sourceMangaID, optional(reference.MergeURL), boolFlag(reference.IsInfoManga),
			boolFlag(reference.GetChapterUpdates), reference.ChapterSortMode, reference.ChapterPriority,
			boolFlag(reference.DownloadChapters), index); err != nil {
			return fmt.Errorf("record merge: %w", err)
		}
		summary.Merges++
	}
	return nil
}

// importFlatMetadata restores the source-attached metadata record together
// with its alternative titles and tags.
func importFlatMetadata(tx *sqlx.Tx, manga *Manga, mangaID string, summary *Summary) error {
	metadata := manga.FlatMetadata
	if metadata == nil {
		return nil
	}
	extra := metadata.Extra
	if strings.TrimSpace(extra) == "" {
		extra = "{}"
	}
	if _, err := tx.Exec(`INSERT INTO manga_metadata(manga_id,uploader,extra,indexed_extra,extra_version)
		VALUES(?,?,?,?,?)
		ON CONFLICT(manga_id) DO UPDATE SET uploader=excluded.uploader, extra=excluded.extra,
			indexed_extra=excluded.indexed_extra, extra_version=excluded.extra_version`,
		mangaID, optional(metadata.Uploader), extra, optional(metadata.IndexedExtra), metadata.ExtraVersion); err != nil {
		return fmt.Errorf("store metadata: %w", err)
	}
	summary.Metadata++
	for _, title := range metadata.Titles {
		name := strings.TrimSpace(title.Title)
		if name == "" {
			continue
		}
		id, err := identity.New()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manga_titles(id,manga_id,title,title_type) VALUES(?,?,?,?)
			ON CONFLICT(manga_id,title) DO UPDATE SET title_type=excluded.title_type`,
			id, mangaID, name, title.Type); err != nil {
			return fmt.Errorf("store title: %w", err)
		}
	}
	for _, tag := range metadata.Tags {
		name := strings.TrimSpace(tag.Name)
		if name == "" {
			continue
		}
		id, err := identity.New()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manga_tags(id,manga_id,namespace,name,tag_type) VALUES(?,?,?,?,?)
			ON CONFLICT(manga_id,namespace,name) DO UPDATE SET tag_type=excluded.tag_type`,
			id, mangaID, optional(tag.Namespace), name, tag.Type); err != nil {
			return fmt.Errorf("store tag: %w", err)
		}
	}
	return nil
}

// boolFlag converts a boolean to the integer a SQLite flag column stores.
func boolFlag(value bool) int {
	if value {
		return 1
	}
	return 0
}

// int64Value returns a pointer to value, or nil when value is zero so the
// column stays unset.
func int64Value(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

// memoText stores a memo only when it carries data; an empty object is the
// writer's default and is not worth persisting.
func memoText(memo string) *string {
	trimmed := strings.TrimSpace(memo)
	if trimmed == "" || trimmed == "{}" || trimmed == "[]" {
		return nil
	}
	return &trimmed
}

// pointerSeconds converts an optional millisecond stamp to optional seconds.
func pointerSeconds(millis *int64) *int64 {
	if millis == nil {
		return nil
	}
	return derefSeconds(*millis)
}

// updateStrategyText maps the numeric update strategy onto the label the
// updater reads. Always-update is the default and stays unset.
func updateStrategyText(strategy int32) *string {
	if strategy == 1 {
		label := "only_fetch_once"
		return &label
	}
	return nil
}

// readerOverrides translates the viewer flag bits into the per-title reader
// settings. An absent or default flag leaves the global setting in place.
func readerOverrides(manga *Manga) (mode, direction, fit *string) {
	flags := manga.Viewer
	if manga.ViewerFlags != nil {
		flags = *manga.ViewerFlags
	}
	switch flags & 0x07 {
	case 1:
		mode, direction = stringPointer("single"), stringPointer("ltr")
	case 2:
		mode, direction = stringPointer("single"), stringPointer("rtl")
	case 3:
		mode = stringPointer("single")
	case 4, 5:
		mode = stringPointer("webtoon")
	}
	return mode, direction, fit
}

func historyStamp(manga *Manga, url string) int64 {
	for _, entry := range manga.History {
		if entry.URL == url {
			return millisToSeconds(entry.LastRead)
		}
	}
	return 0
}

func derefSeconds(millis int64) *int64 {
	if stamp := millisToSeconds(millis); stamp > 0 {
		return &stamp
	}
	return nil
}

func millisToSeconds(millis int64) int64 {
	if millis <= 0 {
		return 0
	}
	return millis / 1000
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPointer(value string) *string { return &value }

// jsonArray encodes a string list for a column the API returns as an array. An
// empty list becomes NULL so a merge does not clear stored metadata.
func jsonArray(values []string) *string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil
	}
	text := string(encoded)
	return &text
}
