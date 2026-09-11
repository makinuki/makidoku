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
	Chapters        int `json:"chapters"`
	ReadChapters    int `json:"readChapters"`
	History         int `json:"history"`
	ReadingSessions int `json:"readingSessions"`
	Tracking        int `json:"tracking"`
	SkippedTracking int `json:"skippedTracking"`
}

// Import applies a plan in one transaction. Re-importing the same file merges
// onto the existing library rather than duplicating it.
func Import(db *sqlx.DB, plan *Plan) (Summary, error) {
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

	for index := range plan.backup.Manga {
		manga := &plan.backup.Manga[index]
		resolved := plan.sources[manga.Source]
		if !manga.Favorite || resolved == nil || resolved.skip {
			summary.SkippedManga++
			continue
		}
		sourceID := resolved.ref.ID
		if resolved.deferred {
			sourceID, err = placeholderSource(tx, manga.Source, resolved.name)
			if err != nil {
				return summary, err
			}
			summary.DeferredManga++
		}
		if err := importManga(tx, manga, sourceID, categoryIDs, &summary); err != nil {
			return summary, fmt.Errorf("import %q: %w", manga.Title, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return summary, fmt.Errorf("commit import: %w", err)
	}
	return summary, nil
}

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
			result, insertErr := tx.Exec(`INSERT INTO categories(name,sort_order) VALUES(?,?)`, name, category.Order)
			if insertErr != nil {
				return nil, fmt.Errorf("create category %q: %w", name, insertErr)
			}
			if id, err = result.LastInsertId(); err != nil {
				return nil, fmt.Errorf("create category %q: %w", name, err)
			}
			summary.Categories++
		} else if err != nil {
			return nil, fmt.Errorf("read category %q: %w", name, err)
		} else if _, err := tx.Exec(`UPDATE categories SET sort_order=? WHERE id=?`, category.Order, id); err != nil {
			return nil, fmt.Errorf("update category %q: %w", name, err)
		}
		out[category.ID] = id
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

func importManga(tx *sqlx.Tx, manga *Manga, sourceID string, categoryIDs map[int64]int64, summary *Summary) error {
	title := strings.TrimSpace(manga.Title)
	if title == "" {
		title = manga.URL
	}
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

	var mangaID string
	err := tx.Get(&mangaID, `SELECT manga_id FROM manga_sources WHERE source_id=? AND source_manga_id=?`, sourceID, manga.URL)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if mangaID, err = identity.New(); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manga(id,source_id,title,alt_titles,description,authors,artists,genres,status,cover_url,in_library,download_format,download_new_chapters,reader_mode,reader_direction,reader_fit,created_at,updated_at)
			VALUES(?,?,?,NULL,?,?,?,?,?,?,1,'cbz',0,?,?,?,?,?)`,
			mangaID, sourceID, title, optional(manga.Description), jsonArray([]string{manga.Author}), jsonArray([]string{manga.Artist}), genres, status, manga.ThumbnailURL,
			mode, direction, fit, createdAt, updatedAt); err != nil {
			return fmt.Errorf("create manga: %w", err)
		}
	default:
		if err != nil {
			return err
		}
		summary.MergedManga++
		if _, err := tx.Exec(`UPDATE manga SET title=?, description=COALESCE(?,description), authors=COALESCE(?,authors),
			artists=COALESCE(?,artists), genres=COALESCE(?,genres), status=?, cover_url=CASE WHEN ?<>'' THEN ? ELSE cover_url END,
			in_library=1, reader_mode=COALESCE(?,reader_mode), reader_direction=COALESCE(?,reader_direction),
			reader_fit=COALESCE(?,reader_fit), updated_at=? WHERE id=?`,
			title, optional(manga.Description), jsonArray([]string{manga.Author}), jsonArray([]string{manga.Artist}), genres, status,
			manga.ThumbnailURL, manga.ThumbnailURL, mode, direction, fit, updatedAt, mangaID); err != nil {
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
		VALUES(?,?,?,NULL,?,?,?)
		ON CONFLICT(source_id,source_manga_id) DO UPDATE SET last_seen_at=excluded.last_seen_at`,
		mangaID, sourceID, manga.URL, primary, now, now); err != nil {
		return fmt.Errorf("link manga source: %w", err)
	}
	for _, categoryID := range manga.Categories {
		localID, ok := categoryIDs[categoryID]
		if !ok {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_categories(manga_id,category_id) VALUES(?,?)`, mangaID, localID); err != nil {
			return fmt.Errorf("link category: %w", err)
		}
	}

	chapterIDs, err := importChapters(tx, manga, mangaID, sourceID, now, summary)
	if err != nil {
		return err
	}
	if err := importHistory(tx, manga, mangaID, chapterIDs, summary); err != nil {
		return err
	}
	if err := importTracking(tx, manga, mangaID, summary); err != nil {
		return err
	}
	return nil
}

func importChapters(tx *sqlx.Tx, manga *Manga, mangaID, sourceID string, now int64, summary *Summary) (map[string]string, error) {
	chapterIDs := map[string]string{}
	for _, chapter := range manga.Chapters {
		url := strings.TrimSpace(chapter.URL)
		if url == "" {
			continue
		}
		var chapterID string
		err := tx.Get(&chapterID, `SELECT chapter_id FROM chapter_sources WHERE source_id=? AND source_chapter_id=?`, sourceID, url)
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
			if _, err := tx.Exec(`INSERT INTO chapters(id,manga_id,source_id,chapter_number,volume,title,language,uploaded_at,scanlator,downloaded,download_path)
				VALUES(?,?,?,?,NULL,?,NULL,?,?,0,NULL)`,
				chapterID, mangaID, sourceID, number, optional(chapter.Name), uploadedAt, optional(chapter.Scanlator)); err != nil {
				return nil, fmt.Errorf("create chapter: %w", err)
			}
			if _, err := tx.Exec(`INSERT INTO chapter_sources(chapter_id,source_id,source_chapter_id,first_seen_at,last_seen_at)
				VALUES(?,?,?,?,?)`, chapterID, sourceID, url, now, now); err != nil {
				return nil, fmt.Errorf("link chapter source: %w", err)
			}
			summary.Chapters++
		} else if err != nil {
			return nil, err
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
		remoteID := strconv.FormatInt(tracking.MediaID, 10)
		if tracking.MediaID == 0 {
			remoteID = strconv.FormatInt(int64(tracking.MediaIDInt), 10)
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
		if _, err := tx.Exec(`INSERT INTO tracker_bindings(manga_id,tracker_type,remote_id,remote_title,remote_score,remote_status,last_synced_chapter,total_remote_chapters,started_at,finished_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(manga_id,tracker_type) DO UPDATE SET remote_id=excluded.remote_id, remote_title=excluded.remote_title,
				remote_score=excluded.remote_score, remote_status=excluded.remote_status, last_synced_chapter=excluded.last_synced_chapter,
				total_remote_chapters=excluded.total_remote_chapters, started_at=excluded.started_at, finished_at=excluded.finished_at`,
			mangaID, trackerType, remoteID, tracking.Title, scoreValue, trackerStatus(tracking.Status),
			float64(tracking.LastChapterRead), total,
			derefSeconds(tracking.StartedReadingDate), derefSeconds(tracking.FinishedReadingDate)); err != nil {
			return fmt.Errorf("restore tracker binding: %w", err)
		}
		if existing == 0 {
			summary.Tracking++
		}
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
