package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/makinuki/makidoku/internal/identity"
)

func placeholders(n int) string {
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

// Repository groups typed queries. Currently it provides only health helpers;
// domain queries are added as the matching subsystems land.

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

// DB exposes the underlying handle for callers that need raw sqlx.
func (r *Repository) DB() *sqlx.DB { return r.db }

// Ping verifies the connection.
func (r *Repository) Ping() error { return r.db.Ping() }

// resolveSource accepts either the backend-owned source ID or the private
// plugin key used by engine callers and returns the canonical source ID.
func (r *Repository) resolveSource(reference string) (string, error) {
	var id string
	err := r.db.Get(&id, `SELECT id FROM sources WHERE id=? OR plugin_key=?`, strings.TrimSpace(reference), strings.TrimSpace(reference))
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetMangaSource resolves a canonical manga ID to the source adapter record
// needed for a plugin call. The external manga ID never crosses the API
// boundary.
func (r *Repository) GetMangaSource(mangaID string) (MangaSource, error) {
	var source MangaSource
	err := r.db.Get(&source, `SELECT ms.manga_id, ms.source_id, ms.source_manga_id, COALESCE(s.plugin_key, '') AS plugin_key, COALESCE(ms.url, '') AS url
		FROM manga_sources ms JOIN sources s ON s.id=ms.source_id WHERE ms.manga_id=? ORDER BY ms.is_primary DESC LIMIT 1`, mangaID)
	return source, err
}

// SetSourceURL records the series page declared for one source link. The
// locator belongs to the link rather than the manga row because one title can
// be linked to several sources.
func (r *Repository) SetSourceURL(mangaID, sourceID, url string) error {
	_, err := r.db.Exec(`UPDATE manga_sources SET url=? WHERE manga_id=? AND source_id=?`, strings.TrimSpace(url), mangaID, sourceID)
	return err
}

// SetSourceLocator rewrites the identifier a source link is read with, and the
// page that resolved it. Both belong to the link, because one title can be
// linked to several sources. An empty page keeps the stored one, so a rung
// that resolved without a page cannot blank a page recorded earlier.
func (r *Repository) SetSourceLocator(mangaID, sourceID, sourceMangaID, url string) error {
	_, err := r.db.Exec(`UPDATE manga_sources SET source_manga_id=?, url=COALESCE(NULLIF(?,''),url)
		WHERE manga_id=? AND source_id=?`,
		strings.TrimSpace(sourceMangaID), strings.TrimSpace(url), mangaID, sourceID)
	return err
}

// MigrateMangaSource atomically retires every chapter of a manga that does
// not belong to the kept source and moves the discovered replacement onto
// the canonical manga. Running both steps in one transaction means a failed
// attach cannot leave the title without chapters. The call returns the
// retired chapters (for progress remapping) and the artifact paths of
// downloaded ones (for file cleanup).
func (r *Repository) MigrateMangaSource(mangaID, keepSourceID, discoveredMangaID string) ([]Chapter, []string, error) {
	mangaID = strings.TrimSpace(mangaID)
	keepSourceID = strings.TrimSpace(keepSourceID)
	discoveredMangaID = strings.TrimSpace(discoveredMangaID)
	if mangaID == "" || discoveredMangaID == "" || mangaID == discoveredMangaID || keepSourceID == "" {
		return nil, nil, errors.New("manga id, kept source id and a distinct replacement manga id are required")
	}
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	retired, artifacts, err := r.retireChaptersInTx(tx, mangaID, keepSourceID)
	if err != nil {
		return nil, nil, err
	}
	if err := r.attachSourceInTx(tx, mangaID, discoveredMangaID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return retired, artifacts, nil
}

// attachSourceInTx moves a discovered source representation onto the target
// canonical manga inside an open transaction. The target Makidoku ID remains
// unchanged and the attached source becomes the only primary so plugin calls
// resolve to the replacement.
func (r *Repository) attachSourceInTx(tx *sqlx.Tx, targetID, discoveredID string) error {
	var source struct {
		SourceID      string `db:"source_id"`
		SourceMangaID string `db:"source_manga_id"`
	}
	if err := tx.Get(&source, `SELECT source_id,source_manga_id FROM manga_sources WHERE manga_id=?`, discoveredID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE manga_sources SET manga_id=?,is_primary=1,last_seen_at=? WHERE manga_id=?`, targetID, time.Now().Unix(), discoveredID); err != nil {
		return err
	}
	// The attached source becomes the only primary so plugin calls resolve to
	// the replacement.
	if _, err := tx.Exec(`UPDATE manga_sources SET is_primary=0 WHERE manga_id=? AND source_id<>?`, targetID, source.SourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE manga SET source_id=?, updated_at=? WHERE id=?`, source.SourceID, time.Now().Unix(), targetID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE chapters SET manga_id=? WHERE manga_id=?`, targetID, discoveredID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM manga WHERE id=? AND in_library=0`, discoveredID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE manga_sources SET is_primary=CASE WHEN source_id=? THEN 1 ELSE is_primary END WHERE manga_id=?`, source.SourceID, targetID); err != nil {
		return err
	}
	return nil
}

// retireChaptersInTx removes every chapter of a manga that does not belong to
// the kept source, together with dependent rows (pages, caches and source
// mappings cascade). Reading progress referencing retired chapters is dropped
// because its chapter link has no cascade. The call returns the retired
// chapters and the artifact paths of downloaded ones so callers can remap
// progress and clean files.
func (r *Repository) retireChaptersInTx(tx *sqlx.Tx, mangaID, keepSourceID string) ([]Chapter, []string, error) {
	var retired []Chapter
	if err := tx.Select(&retired, chapterSelect+`
		WHERE c.manga_id=? AND c.source_id<>?`, mangaID, keepSourceID); err != nil {
		return nil, nil, err
	}
	if len(retired) == 0 {
		return nil, []string{}, nil
	}
	var artifacts []string
	if err := tx.Select(&artifacts, `SELECT download_path FROM chapters
		WHERE manga_id=? AND source_id<>? AND downloaded=1 AND download_path IS NOT NULL`, mangaID, keepSourceID); err != nil {
		return nil, nil, err
	}
	if artifacts == nil {
		artifacts = []string{}
	}
	if _, err := tx.Exec(`DELETE FROM reading_progress WHERE manga_id=? AND last_read_chapter_id IN
		(SELECT id FROM chapters WHERE manga_id=? AND source_id<>?)`, mangaID, mangaID, keepSourceID); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(`DELETE FROM chapters WHERE manga_id=? AND source_id<>?`, mangaID, keepSourceID); err != nil {
		return nil, nil, err
	}
	return retired, artifacts, nil
}

// UpsertPages replaces the materialized page list for a chapter. Page IDs are
// generated by MakiDoku and remain stable while the source page order is
// unchanged.
func (r *Repository) UpsertPages(chapterID string, pages []Page) ([]Page, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	seen := make([]int, 0, len(pages))
	for index := range pages {
		page := pages[index]
		seen = append(seen, page.PageIndex)
		if page.ID == "" {
			_ = tx.Get(&page.ID, `SELECT id FROM pages WHERE chapter_id=? AND page_index=?`, chapterID, page.PageIndex)
			if page.ID == "" {
				page.ID, err = identity.New()
				if err != nil {
					return nil, err
				}
			}
		}
		if _, err := tx.Exec(`INSERT INTO pages(id,chapter_id,page_index,remote_url,request_headers,is_scrambled)
			VALUES(?,?,?,?,?,?) ON CONFLICT(chapter_id,page_index) DO UPDATE SET
			remote_url=excluded.remote_url,request_headers=excluded.request_headers,is_scrambled=excluded.is_scrambled`, page.ID, chapterID, page.PageIndex, page.RemoteURL, page.HeadersJSON, page.IsScrambled); err != nil {
			return nil, err
		}
		pages[index] = page
	}
	if len(seen) == 0 {
		if _, err := tx.Exec(`DELETE FROM pages WHERE chapter_id=?`, chapterID); err != nil {
			return nil, err
		}
	} else {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(seen)), ",")
		args := make([]any, 0, len(seen)+1)
		args = append(args, chapterID)
		for _, value := range seen {
			args = append(args, value)
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE chapter_id=? AND page_index NOT IN (`+placeholders+`)`, args...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.ListPages(chapterID)
}

func (r *Repository) ListPages(chapterID string) ([]Page, error) {
	var pages []Page
	err := r.db.Select(&pages, `SELECT id,chapter_id,page_index,remote_url,request_headers,is_scrambled FROM pages WHERE chapter_id=? ORDER BY page_index`, chapterID)
	return pages, err
}

func (r *Repository) GetPage(id string) (Page, error) {
	var page Page
	err := r.db.Get(&page, `SELECT id,chapter_id,page_index,remote_url,request_headers,is_scrambled FROM pages WHERE id=?`, id)
	return page, err
}

// SetPageCache records the disk location of a processed page image.
func (r *Repository) SetPageCache(pageID, key, path, contentType string, byteSize int64, fetchedAt int64) error {
	_, err := r.db.Exec(`INSERT INTO page_cache(page_id,cache_key,byte_path,content_type,byte_size,fetched_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(page_id) DO UPDATE SET
		cache_key=excluded.cache_key,byte_path=excluded.byte_path,
		content_type=excluded.content_type,byte_size=excluded.byte_size,
		fetched_at=excluded.fetched_at`,
		pageID, key, path, contentType, byteSize, fetchedAt)
	return err
}

// GetPageCache returns the cache record of a page. sql.ErrNoRows reports that
// the bytes were never cached or were evicted.
func (r *Repository) GetPageCache(pageID string) (PageCache, error) {
	var cache PageCache
	err := r.db.Get(&cache, `SELECT page_id,cache_key,byte_path,content_type,byte_size,fetched_at FROM page_cache WHERE page_id=?`, pageID)
	return cache, err
}

// ListCachedPaths returns every cache byte path currently referenced by the
// database. Cache files missing from this set are orphans.
func (r *Repository) ListCachedPaths() ([]string, error) {
	var paths []string
	err := r.db.Select(&paths, `SELECT byte_path FROM page_cache`)
	return paths, err
}

// Category helpers (used by the library API).

func (r *Repository) ListCategories() ([]Category, error) {
	var out []Category
	err := r.db.Select(&out, `SELECT id, name, sort_order FROM categories ORDER BY sort_order, name`)
	return out, err
}

func (r *Repository) CreateCategory(name string, sortOrder int) (Category, error) {
	res, err := r.db.Exec(`INSERT INTO categories(name, sort_order) VALUES(?, ?)`, name, sortOrder)
	if err != nil {
		return Category{}, err
	}
	id, _ := res.LastInsertId()
	return Category{ID: id, Name: name, SortOrder: sortOrder}, nil
}

func (r *Repository) UpdateCategory(id int64, name string, sortOrder int) (Category, error) {
	name = strings.TrimSpace(name)
	if id < 1 || name == "" {
		return Category{}, errors.New("category id and name are required")
	}
	result, err := r.db.Exec(`UPDATE categories SET name=?, sort_order=? WHERE id=?`, name, sortOrder, id)
	if err != nil {
		return Category{}, err
	}
	if err := requireChange(result, "update category"); err != nil {
		return Category{}, err
	}
	var category Category
	err = r.db.Get(&category, `SELECT id,name,sort_order FROM categories WHERE id=?`, id)
	return category, err
}

func (r *Repository) DeleteCategory(id int64) error {
	if id < 1 {
		return errors.New("category id must be positive")
	}
	result, err := r.db.Exec(`DELETE FROM categories WHERE id=?`, id)
	if err != nil {
		return err
	}
	return requireChange(result, "delete category")
}

func (r *Repository) SetMangaLibrary(id string, inLibrary bool) (Manga, error) {
	result, err := r.db.Exec(`UPDATE manga SET in_library=?, updated_at=? WHERE id=?`, inLibrary, time.Now().Unix(), strings.TrimSpace(id))
	if err != nil {
		return Manga{}, err
	}
	if err := requireChange(result, "update library membership"); err != nil {
		return Manga{}, err
	}
	return r.GetManga(id)
}

func (r *Repository) SetMangaDownloadNewChapters(id string, enabled bool) (Manga, error) {
	result, err := r.db.Exec(`UPDATE manga SET download_new_chapters=?, updated_at=? WHERE id=?`, enabled, time.Now().Unix(), strings.TrimSpace(id))
	if err != nil {
		return Manga{}, err
	}
	if err := requireChange(result, "update automatic downloads"); err != nil {
		return Manga{}, err
	}
	return r.GetManga(id)
}

func (r *Repository) SetMangaCategory(mangaID string, categoryID int64, enabled bool) error {
	if strings.TrimSpace(mangaID) == "" || categoryID < 1 {
		return errors.New("manga id and category id are required")
	}
	if enabled {
		_, err := r.db.Exec(`INSERT OR IGNORE INTO manga_categories(manga_id,category_id) VALUES(?,?)`, mangaID, categoryID)
		return err
	}
	_, err := r.db.Exec(`DELETE FROM manga_categories WHERE manga_id=? AND category_id=?`, mangaID, categoryID)
	return err
}

func (r *Repository) ListMangaCategories(mangaID string) ([]Category, error) {
	var out []Category
	err := r.db.Select(&out, `SELECT c.id,c.name,c.sort_order FROM categories c JOIN manga_categories mc ON mc.category_id=c.id WHERE mc.manga_id=? ORDER BY c.sort_order,c.name`, mangaID)
	return out, err
}

func (r *Repository) ListLibrary(query string, categoryID int64) ([]LibraryManga, error) {
	query = strings.TrimSpace(query)
	args := []any{}
	where := `WHERE m.in_library=1`
	if query != "" {
		where += ` AND (m.title LIKE ? OR m.alt_titles LIKE ?)`
		like := "%" + query + "%"
		args = append(args, like, like)
	}
	if categoryID > 0 {
		where += ` AND EXISTS (SELECT 1 FROM manga_categories mc WHERE mc.manga_id=m.id AND mc.category_id=?)`
		args = append(args, categoryID)
	}
	var manga []Manga
	err := r.db.Select(&manga, `SELECT m.id,m.source_id,m.title,m.alt_titles,m.description,m.authors,m.artists,m.genres,m.tags,m.status,m.cover_url,m.cover_cache_path,m.cover_content_type,m.cover_fetched_at,m.in_library,m.download_format,m.download_new_chapters,m.reader_mode,m.reader_direction,m.reader_fit,m.chapter_sort,m.chapter_filter,m.chapter_language,m.created_at,m.updated_at,m.details_fetched_at,
		m.custom_title,m.custom_artist,m.custom_author,m.custom_description,m.custom_genres,m.custom_status,m.custom_cover_url,
		m.notes,m.memo,m.source_version,m.update_strategy,m.favorite_modified_at,m.initialized,m.excluded_scanlators,m.chapter_flags
		FROM manga m `+where+` ORDER BY m.updated_at DESC,m.title`, args...)
	if err != nil {
		return nil, err
	}

	// Categories, progress and unread counts are fetched in three batched
	// queries instead of per-title round-trips.
	ids := make([]any, 0, len(manga))
	for _, item := range manga {
		ids = append(ids, item.ID)
	}
	in := placeholders(len(ids))

	categoryMap := map[string][]Category{}
	if len(ids) > 0 {
		var catRows []struct {
			MangaID   string `db:"manga_id"`
			ID        int64  `db:"id"`
			Name      string `db:"name"`
			SortOrder int    `db:"sort_order"`
		}
		if err := r.db.Select(&catRows, `SELECT mc.manga_id,c.id,c.name,c.sort_order FROM manga_categories mc JOIN categories c ON c.id=mc.category_id WHERE mc.manga_id IN (`+in+`) ORDER BY c.sort_order,c.name`, ids...); err != nil {
			return nil, err
		}
		for _, row := range catRows {
			categoryMap[row.MangaID] = append(categoryMap[row.MangaID], Category{ID: row.ID, Name: row.Name, SortOrder: row.SortOrder})
		}
	}

	progressMap := map[string]ReadingProgress{}
	if len(ids) > 0 {
		var progressRows []ReadingProgress
		if err := r.db.Select(&progressRows, `SELECT manga_id,last_read_chapter_id,last_read_page,total_pages,is_completed,last_read_at FROM reading_progress WHERE manga_id IN (`+in+`)`, ids...); err != nil {
			return nil, err
		}
		for _, row := range progressRows {
			progressMap[row.MangaID] = row
		}
	}

	unreadMap := map[string]int{}
	if len(ids) > 0 {
		var unreadRows []struct {
			MangaID string `db:"manga_id"`
			Unread  int    `db:"unread"`
		}
		unreadQuery := `SELECT c.manga_id, SUM(CASE WHEN COALESCE(rs.read, 0)=0 THEN 1 ELSE 0 END) AS unread
			FROM chapters c LEFT JOIN chapter_read_state rs ON rs.chapter_id=c.id
			WHERE c.manga_id IN (` + in + `) GROUP BY c.manga_id`
		if err := r.db.Select(&unreadRows, unreadQuery, ids...); err != nil {
			return nil, err
		}
		for _, row := range unreadRows {
			unreadMap[row.MangaID] = row.Unread
		}
	}

	// Chapter totals, downloaded and bookmarked counts, and content languages
	// feed the library badges, filters, and sorts in one batched round-trip.
	type chapterTotals struct {
		Total      int
		Downloaded int
		Bookmarked int
	}
	totalsMap := map[string]chapterTotals{}
	langMap := map[string][]string{}
	if len(ids) > 0 {
		var totalRows []struct {
			MangaID    string `db:"manga_id"`
			Total      int    `db:"total"`
			Downloaded int    `db:"downloaded"`
			Bookmarked int    `db:"bookmarked"`
		}
		totalQuery := `SELECT c.manga_id,
			COUNT(*) AS total,
			SUM(CASE WHEN c.downloaded=1 THEN 1 ELSE 0 END) AS downloaded,
			SUM(CASE WHEN c.bookmark=1 THEN 1 ELSE 0 END) AS bookmarked
			FROM chapters c WHERE c.manga_id IN (` + in + `) GROUP BY c.manga_id`
		if err := r.db.Select(&totalRows, totalQuery, ids...); err != nil {
			return nil, err
		}
		for _, row := range totalRows {
			totalsMap[row.MangaID] = chapterTotals{Total: row.Total, Downloaded: row.Downloaded, Bookmarked: row.Bookmarked}
		}
		var langRows []struct {
			MangaID  string `db:"manga_id"`
			Language string `db:"language"`
		}
		langQuery := `SELECT DISTINCT c.manga_id, c.language FROM chapters c
			WHERE c.manga_id IN (` + in + `) AND c.language IS NOT NULL AND c.language != ''
			ORDER BY c.manga_id, c.language`
		if err := r.db.Select(&langRows, langQuery, ids...); err != nil {
			return nil, err
		}
		for _, row := range langRows {
			langMap[row.MangaID] = append(langMap[row.MangaID], row.Language)
		}
	}

	out := make([]LibraryManga, 0, len(manga))
	for _, item := range manga {
		var progress *ReadingProgress
		if p, ok := progressMap[item.ID]; ok {
			progress = &p
		}
		languages := langMap[item.ID]
		if languages == nil {
			languages = []string{}
		}
		out = append(out, LibraryManga{
			Manga:              item,
			Categories:         categoryMap[item.ID],
			Progress:           progress,
			UnreadChapters:     unreadMap[item.ID],
			DownloadedChapters: totalsMap[item.ID].Downloaded,
			TotalChapters:      totalsMap[item.ID].Total,
			BookmarkedChapters: totalsMap[item.ID].Bookmarked,
			Languages:          languages,
		})
	}
	return out, nil
}

func (r *Repository) ListLibraryBySource(sourceID string) ([]Manga, error) {
	var manga []Manga
	err := r.db.Select(&manga, `SELECT m.id,m.source_id,m.title,m.alt_titles,m.description,m.authors,m.artists,m.genres,m.tags,m.status,m.cover_url,m.cover_cache_path,m.cover_content_type,m.cover_fetched_at,m.in_library,m.download_format,m.download_new_chapters,m.reader_mode,m.reader_direction,m.reader_fit,m.chapter_sort,m.chapter_filter,m.chapter_language,m.created_at,m.updated_at,m.details_fetched_at,
		m.custom_title,m.custom_artist,m.custom_author,m.custom_description,m.custom_genres,m.custom_status,m.custom_cover_url,
		m.notes,m.memo,m.source_version,m.update_strategy,m.favorite_modified_at,m.initialized,m.excluded_scanlators,m.chapter_flags
		FROM manga m WHERE m.in_library=1 AND m.source_id=? ORDER BY m.title`, strings.TrimSpace(sourceID))
	if manga == nil {
		manga = []Manga{}
	}
	return manga, err
}

func (r *Repository) LibrarySourceCounts() (map[string]int, error) {
	var rows []struct {
		SourceID string `db:"source_id"`
		Count    int    `db:"count"`
	}
	if err := r.db.Select(&rows, `SELECT source_id,COUNT(*) AS count FROM manga WHERE in_library=1 GROUP BY source_id`); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.SourceID] = row.Count
	}
	return out, nil
}

func (r *Repository) GetMangaAggregate(id string) (MangaAggregate, error) {
	manga, err := r.GetManga(id)
	if err != nil {
		return MangaAggregate{}, err
	}
	chapters, err := r.ListChapters(id)
	if err != nil {
		return MangaAggregate{}, err
	}
	readStates, err := r.ListChapterRead(id)
	if err != nil {
		return MangaAggregate{}, err
	}
	readMap := make(map[string]bool, len(readStates))
	for _, state := range readStates {
		readMap[state.ChapterID] = state.Read
	}
	for index := range chapters {
		chapters[index].Read = readMap[chapters[index].ID]
	}
	categories, err := r.ListMangaCategories(id)
	if err != nil {
		return MangaAggregate{}, err
	}
	trackers, err := r.ListTrackerBindings(id)
	if err != nil {
		return MangaAggregate{}, err
	}
	var progress *ReadingProgress
	if p, err := r.GetReadingProgress(id); err == nil {
		progress = &p
	} else if !errors.Is(err, sql.ErrNoRows) {
		return MangaAggregate{}, err
	}
	readingSeconds, err := r.ReadingSeconds(id)
	if err != nil {
		return MangaAggregate{}, err
	}
	// Nil slices marshal as JSON null, which the web client renders as absent
	// collections and crashes on; the aggregate contract is always an array.
	if categories == nil {
		categories = []Category{}
	}
	if chapters == nil {
		chapters = []Chapter{}
	}
	if trackers == nil {
		trackers = []TrackerBinding{}
	}
	return MangaAggregate{Manga: manga, Categories: categories, Chapters: chapters, Progress: progress, Trackers: trackers, ReadingSeconds: readingSeconds}, nil
}

func (r *Repository) ListHistory() ([]HistoryItem, error) {
	var rows []ReadingProgress
	err := r.db.Select(&rows, `SELECT manga_id,last_read_chapter_id,last_read_page,total_pages,is_completed,last_read_at FROM reading_progress ORDER BY last_read_at DESC`)
	if err != nil {
		return nil, err
	}

	// Manga and chapter records are fetched in batched queries. A progress
	// row whose references vanished (retired chapters, removed titles) is
	// skipped rather than failing the whole page.
	mangaIDs := make([]any, 0, len(rows))
	chapterIDs := make([]any, 0, len(rows))
	seenManga := map[string]struct{}{}
	seenChapter := map[string]struct{}{}
	for _, row := range rows {
		if _, ok := seenManga[row.MangaID]; !ok {
			seenManga[row.MangaID] = struct{}{}
			mangaIDs = append(mangaIDs, row.MangaID)
		}
		if _, ok := seenChapter[row.LastReadChapterID]; !ok {
			seenChapter[row.LastReadChapterID] = struct{}{}
			chapterIDs = append(chapterIDs, row.LastReadChapterID)
		}
	}

	mangaMap := map[string]Manga{}
	if len(mangaIDs) > 0 {
		var mangaRows []Manga
		if err := r.db.Select(&mangaRows, `SELECT * FROM manga WHERE id IN (`+placeholders(len(mangaIDs))+`)`, mangaIDs...); err != nil {
			return nil, err
		}
		for _, m := range mangaRows {
			mangaMap[m.ID] = m
		}
	}
	chapterMap := map[string]Chapter{}
	if len(chapterIDs) > 0 {
		var chapterRows []Chapter
		if err := r.db.Select(&chapterRows, `SELECT * FROM chapters WHERE id IN (`+placeholders(len(chapterIDs))+`)`, chapterIDs...); err != nil {
			return nil, err
		}
		for _, c := range chapterRows {
			chapterMap[c.ID] = c
		}
	}

	out := make([]HistoryItem, 0, len(rows))
	for _, row := range rows {
		manga, ok := mangaMap[row.MangaID]
		if !ok {
			slog.Warn("history skipping progress for missing manga", "manga", row.MangaID)
			continue
		}
		chapter, ok := chapterMap[row.LastReadChapterID]
		if !ok {
			slog.Warn("history skipping progress for missing chapter", "chapter", row.LastReadChapterID)
			continue
		}
		out = append(out, HistoryItem{Manga: manga, Chapter: chapter, Progress: row})
	}
	return out, nil
}

func (r *Repository) UpsertManga(manga Manga) (Manga, error) {
	manga.SourceID = strings.TrimSpace(manga.SourceID)
	manga.SourceMangaID = strings.TrimSpace(manga.SourceMangaID)
	if manga.SourceID == "" || manga.SourceMangaID == "" {
		return Manga{}, errors.New("source id and source manga id are required")
	}
	sourceID, err := r.resolveSource(manga.SourceID)
	if err != nil {
		return Manga{}, fmt.Errorf("resolve source %s: %w", manga.SourceID, err)
	}
	manga.SourceID = sourceID
	var existingID string
	var previousCoverURL string
	if err := r.db.Get(&existingID, `SELECT manga_id FROM manga_sources WHERE source_id=? AND source_manga_id=?`, sourceID, manga.SourceMangaID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Manga{}, err
	} else if err == nil {
		manga.ID = existingID
		if err := r.db.Get(&previousCoverURL, `SELECT cover_url FROM manga WHERE id=?`, existingID); err != nil {
			return Manga{}, err
		}
	}
	if manga.ID == "" {
		manga.ID, err = identity.New()
		if err != nil {
			return Manga{}, err
		}
	}
	if manga.DownloadFormat == "" {
		manga.DownloadFormat = "cbz"
	}
	if manga.DownloadFormat != "cbz" && manga.DownloadFormat != "folder" {
		return Manga{}, fmt.Errorf("unsupported download format %q", manga.DownloadFormat)
	}
	now := time.Now().Unix()
	if manga.CreatedAt == 0 {
		manga.CreatedAt = now
	}
	if manga.UpdatedAt == 0 {
		manga.UpdatedAt = now
	}

	_, err = r.db.Exec(`INSERT INTO manga(
		id, source_id, title, alt_titles, description,
		authors, artists, genres, tags, status, cover_url, in_library,
		download_format, download_new_chapters, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title = excluded.title,
		alt_titles = excluded.alt_titles,
		description = excluded.description,
		authors = excluded.authors,
		artists = excluded.artists,
		genres = excluded.genres,
		tags = excluded.tags,
		status = excluded.status,
		cover_url = excluded.cover_url,
		download_format = excluded.download_format,
		updated_at = excluded.updated_at`,
		manga.ID, manga.SourceID, manga.Title,
		manga.AltTitles, manga.Description, manga.Authors, manga.Artists,
		manga.Genres, manga.Tags, manga.Status, manga.CoverURL, manga.InLibrary,
		manga.DownloadFormat, manga.DownloadNewChapters, manga.CreatedAt, manga.UpdatedAt)
	if err != nil {
		return Manga{}, fmt.Errorf("upsert manga %s: %w", manga.ID, err)
	}
	if previousCoverURL != "" && previousCoverURL != manga.CoverURL {
		// A changed locator invalidates the cached rendition so the next
		// cover request refetches from the new URL.
		if _, err := r.db.Exec(`UPDATE manga SET
			cover_cache_path=NULL, cover_content_type=NULL, cover_fetched_at=NULL
			WHERE id=?`, manga.ID); err != nil {
			return Manga{}, fmt.Errorf("invalidate cover cache %s: %w", manga.ID, err)
		}
	}
	_, err = r.db.Exec(`INSERT INTO manga_sources(manga_id,source_id,source_manga_id,url,is_primary,first_seen_at,last_seen_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(source_id,source_manga_id) DO UPDATE SET last_seen_at=excluded.last_seen_at,
			url=COALESCE(NULLIF(excluded.url,''),url)`,
		manga.ID, sourceID, manga.SourceMangaID, manga.SourcePageURL, true, now, now)
	if err != nil {
		return Manga{}, fmt.Errorf("link manga source %s: %w", manga.ID, err)
	}
	return r.GetManga(manga.ID)
}

// UpsertMangaStub records a bare listing entry for a search result without
// overwriting any stored detail of an existing entry. Listing flows must use
// it instead of UpsertManga so a search pass cannot degrade previously
// fetched metadata; a listing that carries a series URL still refreshes the
// stored locator.
func (r *Repository) UpsertMangaStub(manga Manga) (Manga, error) {
	manga.SourceID = strings.TrimSpace(manga.SourceID)
	manga.SourceMangaID = strings.TrimSpace(manga.SourceMangaID)
	if manga.SourceID == "" || manga.SourceMangaID == "" {
		return Manga{}, errors.New("source id and source manga id are required")
	}
	sourceID, err := r.resolveSource(manga.SourceID)
	if err != nil {
		return Manga{}, fmt.Errorf("resolve source %s: %w", manga.SourceID, err)
	}
	var existingID string
	err = r.db.Get(&existingID, `SELECT manga_id FROM manga_sources WHERE source_id=? AND source_manga_id=?`, sourceID, manga.SourceMangaID)
	if errors.Is(err, sql.ErrNoRows) {
		return r.UpsertManga(manga)
	}
	if err != nil {
		return Manga{}, err
	}
	if manga.SourcePageURL != "" {
		if err := r.SetSourceURL(existingID, sourceID, manga.SourcePageURL); err != nil {
			return Manga{}, err
		}
	}
	return r.GetManga(existingID)
}

// SetMangaCover records where the processed cover bytes of a manga live on
// disk. The byte path is a backend-owned detail.
func (r *Repository) SetMangaCover(id, path, contentType string, fetchedAt int64) error {
	_, err := r.db.Exec(`UPDATE manga SET cover_cache_path=?, cover_content_type=?, cover_fetched_at=? WHERE id=?`, path, contentType, fetchedAt, id)
	return err
}

func (r *Repository) GetManga(id string) (Manga, error) {
	var manga Manga
	err := r.db.Get(&manga, `SELECT id, source_id, title,
		alt_titles, description, authors, artists, genres, tags, status, cover_url,
		cover_cache_path, cover_content_type, cover_fetched_at, in_library, download_format, download_new_chapters, reader_mode, reader_direction, reader_fit, chapter_sort, chapter_filter, chapter_language, created_at, updated_at,
		details_fetched_at,
		custom_title, custom_artist, custom_author, custom_description, custom_genres, custom_status, custom_cover_url,
		notes, memo, source_version, update_strategy, favorite_modified_at, initialized, excluded_scanlators, chapter_flags
		FROM manga WHERE id = ?`, id)
	return manga, err
}

// SetMangaReaderOverrides records the per-title reader preferences. A nil
// value clears the override so the reader falls back to the global setting.
func (r *Repository) SetMangaReaderOverrides(id string, mode, direction, fit *string) (Manga, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Manga{}, errors.New("manga id is required")
	}
	if err := validateReaderValue(mode, "single", "double", "webtoon"); err != nil {
		return Manga{}, err
	}
	if err := validateReaderValue(direction, "ltr", "rtl"); err != nil {
		return Manga{}, err
	}
	if err := validateReaderValue(fit, "width", "height", "screen", "original"); err != nil {
		return Manga{}, err
	}
	result, err := r.db.Exec(`UPDATE manga SET reader_mode=?, reader_direction=?, reader_fit=?, updated_at=? WHERE id=?`,
		mode, direction, fit, time.Now().Unix(), id)
	if err != nil {
		return Manga{}, err
	}
	if err := requireChange(result, "update reader overrides"); err != nil {
		return Manga{}, err
	}
	return r.GetManga(id)
}

// validateReaderValue reports whether a nullable override is one of the
// permitted values. A nil value is always permitted and clears the override.
func validateReaderValue(value *string, allowed ...string) error {
	if value == nil {
		return nil
	}
	for _, item := range allowed {
		if *value == item {
			return nil
		}
	}
	return fmt.Errorf("unsupported reader value %q", *value)
}

// UpdateMangaChapterView records the per-title chapter list presentation. An
// empty value clears the field so the client falls back to its defaults.
func (r *Repository) UpdateMangaChapterView(id, sort, filter, language string) (Manga, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Manga{}, errors.New("manga id is required")
	}
	if err := validateChapterViewValue(sort, "", "number-desc", "number-asc", "source"); err != nil {
		return Manga{}, err
	}
	if err := validateChapterViewValue(filter, "", "all", "unread", "downloaded", "bookmarked"); err != nil {
		return Manga{}, err
	}
	if len(language) > 32 {
		return Manga{}, fmt.Errorf("unsupported chapter language %q", language)
	}
	result, err := r.db.Exec(`UPDATE manga SET chapter_sort=?, chapter_filter=?, chapter_language=?, updated_at=? WHERE id=?`,
		strings.TrimSpace(sort), strings.TrimSpace(filter), strings.TrimSpace(language), time.Now().Unix(), id)
	if err != nil {
		return Manga{}, err
	}
	if err := requireChange(result, "update chapter view"); err != nil {
		return Manga{}, err
	}
	return r.GetManga(id)
}

// validateChapterViewValue reports whether a chapter list presentation value
// is permitted. An empty value is always permitted and clears the field.
func validateChapterViewValue(value string, allowed ...string) error {
	for _, item := range allowed {
		if value == item {
			return nil
		}
	}
	return fmt.Errorf("unsupported chapter view value %q", value)
}

// SetMangaDetailsFetched stamps the last successful full-details fetch.
func (r *Repository) SetMangaDetailsFetched(id string, fetchedAt int64) error {
	if _, err := r.db.Exec(`UPDATE manga SET details_fetched_at=? WHERE id=?`, fetchedAt, id); err != nil {
		return fmt.Errorf("stamp details fetch %s: %w", id, err)
	}
	return nil
}

func (r *Repository) UpsertChapter(chapter Chapter) (Chapter, error) {
	chapter.MangaID = strings.TrimSpace(chapter.MangaID)
	chapter.SourceChapterID = strings.TrimSpace(chapter.SourceChapterID)
	if chapter.MangaID == "" || chapter.SourceChapterID == "" {
		return Chapter{}, errors.New("manga id and source chapter id are required")
	}
	if chapter.SourceID == "" {
		if err := r.db.Get(&chapter.SourceID, `SELECT source_id FROM manga WHERE id=?`, chapter.MangaID); err != nil {
			return Chapter{}, err
		}
	}
	sourceID, err := r.resolveSource(chapter.SourceID)
	if err != nil {
		return Chapter{}, fmt.Errorf("resolve source %s: %w", chapter.SourceID, err)
	}
	chapter.SourceID = sourceID
	var existingID string
	if err := r.db.Get(&existingID, `SELECT chapter_id FROM chapter_sources WHERE source_id=? AND source_chapter_id=?`, sourceID, chapter.SourceChapterID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Chapter{}, err
	} else if err == nil {
		chapter.ID = existingID
	}
	now := time.Now().Unix()
	if chapter.ID == "" {
		chapter.ID, err = identity.New()
		if err != nil {
			return Chapter{}, err
		}
		_, err = r.db.Exec(`INSERT INTO chapters(
			id, manga_id, source_id, chapter_number, volume, title, language,
			uploaded_at, scanlator, locked, downloaded, download_path
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chapter.ID, chapter.MangaID, chapter.SourceID,
			chapter.ChapterNumber, chapter.Volume, chapter.Title, chapter.Language,
			chapter.UploadedAt, chapter.Scanlator, chapter.Locked, chapter.Downloaded,
			chapter.DownloadPath)
		if err != nil {
			return Chapter{}, fmt.Errorf("create chapter %s: %w", chapter.ID, err)
		}
		_, err = r.db.Exec(`INSERT INTO chapter_sources(chapter_id,source_id,source_chapter_id,first_seen_at,last_seen_at)
			VALUES(?,?,?,?,?)`, chapter.ID, sourceID, chapter.SourceChapterID, now, now)
		if err != nil {
			return Chapter{}, fmt.Errorf("link chapter source %s: %w", chapter.ID, err)
		}
	} else {
		// manga_id is part of the update: a source may re-parent an external
		// chapter to another series, and the canonical record must follow.
		_, err = r.db.Exec(`UPDATE chapters SET
			manga_id=?, chapter_number=?, volume=?, title=?, language=?, uploaded_at=?, scanlator=?, locked=?
			WHERE id=?`,
			chapter.MangaID, chapter.ChapterNumber, chapter.Volume, chapter.Title,
			chapter.Language, chapter.UploadedAt, chapter.Scanlator, chapter.Locked, chapter.ID)
		if err != nil {
			return Chapter{}, fmt.Errorf("update chapter %s: %w", chapter.ID, err)
		}
		if err != nil {
			return Chapter{}, fmt.Errorf("update chapter %s: %w", chapter.ID, err)
		}
		_, err = r.db.Exec(`INSERT INTO chapter_sources(chapter_id,source_id,source_chapter_id,first_seen_at,last_seen_at)
			VALUES(?,?,?,?,?) ON CONFLICT(chapter_id,source_id) DO UPDATE SET
			source_chapter_id=excluded.source_chapter_id,last_seen_at=excluded.last_seen_at`,
			chapter.ID, sourceID, chapter.SourceChapterID, now, now)
		if err != nil {
			return Chapter{}, fmt.Errorf("link chapter source %s: %w", chapter.ID, err)
		}
	}
	return r.GetChapter(chapter.ID)
}

// chapterSelect resolves the current external chapter id of the owning source
// representation alongside the canonical record. The owning representation is
// authoritative; a missing row leaves the external id empty.
const chapterSelect = `SELECT c.id, c.manga_id, c.source_id,
	cs.source_chapter_id, c.chapter_number, c.volume, c.title, c.language,
		c.uploaded_at, c.scanlator, c.locked, c.downloaded, c.download_path,
		COALESCE((SELECT q.status FROM download_queue q WHERE q.chapter_id=c.id), '') AS download_status
	FROM chapters c
	LEFT JOIN chapter_sources cs ON cs.chapter_id = c.id AND cs.source_id = c.source_id`

func (r *Repository) GetChapter(id string) (Chapter, error) {
	var chapter Chapter
	err := r.db.Get(&chapter, chapterSelect+` WHERE c.id = ?`, id)
	return chapter, err
}

func (r *Repository) ListChapters(mangaID string) ([]Chapter, error) {
	var chapters []Chapter
	err := r.db.Select(&chapters, chapterSelect+` WHERE c.manga_id = ?
		ORDER BY c.chapter_number IS NULL, c.chapter_number, cs.source_chapter_id`, mangaID)
	return chapters, err
}

// BulkChapterIDs returns the chapter IDs of a title in reading order. With
// unreadOnly set, chapters already marked read are omitted; a missing read
// state counts as unread. A language selection restricts the result to those
// codes; chapters with no language stay eligible because their language is
// unknown.
func (r *Repository) BulkChapterIDs(mangaID string, unreadOnly bool, selection []string) ([]string, error) {
	args := []any{mangaID}
	languageClause := ""
	if len(selection) > 0 {
		languageClause = ` AND (c.language IS NULL OR c.language='' OR LOWER(c.language) IN (` + placeholders(len(selection)) + `))`
		for _, code := range selection {
			args = append(args, code)
		}
	}
	query := `SELECT c.id FROM chapters c WHERE c.manga_id=?` + languageClause + ` ORDER BY c.chapter_number IS NULL, c.chapter_number, c.id`
	if unreadOnly {
		query = `SELECT c.id FROM chapters c
			LEFT JOIN chapter_read_state s ON s.chapter_id=c.id
			WHERE c.manga_id=? AND COALESCE(s.read,0)=0` + languageClause + `
			ORDER BY c.chapter_number IS NULL, c.chapter_number, c.id`
	}
	var ids []string
	err := r.db.Select(&ids, query, args...)
	if ids == nil {
		ids = []string{}
	}
	return ids, err
}

// NextChapterIDs returns the next chapters to download after chapterID, in
// reading order. A language selection restricts the result to those codes;
// chapters with no language stay eligible because their language is unknown.
func (r *Repository) NextChapterIDs(mangaID, chapterID string, limit int, selection []string) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	var number *float64
	if err := r.db.Get(&number, `SELECT chapter_number FROM chapters WHERE id=? AND manga_id=?`, chapterID, mangaID); err != nil {
		return nil, err
	}
	if number == nil {
		return []string{}, nil
	}
	query := `SELECT c.id FROM chapters c LEFT JOIN download_queue q ON q.chapter_id=c.id
		WHERE c.manga_id=? AND c.chapter_number>? AND c.downloaded=0 AND (q.status IS NULL OR q.status = ?)`
	args := []any{mangaID, *number, QueueFailed}
	if len(selection) > 0 {
		query += ` AND (c.language IS NULL OR c.language='' OR LOWER(c.language) IN (` + placeholders(len(selection)) + `))`
		for _, code := range selection {
			args = append(args, code)
		}
	}
	query += ` ORDER BY c.chapter_number LIMIT ?`
	args = append(args, limit)
	var ids []string
	err := r.db.Select(&ids, query, args...)
	if ids == nil {
		ids = []string{}
	}
	return ids, err
}

// SourceChapterLanguages returns the distinct chapter language codes observed
// for each source. A source whose chapters carry no language is absent from
// the map. The API uses this to decide which sources have a real language
// choice to offer.
func (r *Repository) SourceChapterLanguages() (map[string][]string, error) {
	var rows []struct {
		SourceID string `db:"source_id"`
		Language string `db:"language"`
	}
	if err := r.db.Select(&rows, `SELECT DISTINCT source_id, language FROM chapters
		WHERE language IS NOT NULL AND language != ''
		ORDER BY source_id, language`); err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(rows))
	for _, row := range rows {
		out[row.SourceID] = append(out[row.SourceID], row.Language)
	}
	return out, nil
}

func (r *Repository) UpsertReadingProgress(progress ReadingProgress) (ReadingProgress, error) {
	if strings.TrimSpace(progress.MangaID) == "" || strings.TrimSpace(progress.LastReadChapterID) == "" {
		return ReadingProgress{}, errors.New("manga id and chapter id are required")
	}
	if progress.LastReadPage < 1 || progress.TotalPages < 1 || progress.LastReadPage > progress.TotalPages {
		return ReadingProgress{}, errors.New("invalid reading progress")
	}
	var chapterManga string
	if err := r.db.Get(&chapterManga, `SELECT manga_id FROM chapters WHERE id=?`, progress.LastReadChapterID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReadingProgress{}, errors.New("last read chapter does not exist")
		}
		return ReadingProgress{}, err
	}
	if chapterManga != progress.MangaID {
		return ReadingProgress{}, errors.New("last read chapter does not belong to manga")
	}
	progress.LastReadAt = time.Now().Unix()
	// Completion is monotonic: a later write that reports the chapter as
	// unfinished (navigating back) must not clear an existing flag, which
	// would disagree with remote tracker state.
	tx, err := r.db.Beginx()
	if err != nil {
		return ReadingProgress{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`INSERT INTO reading_progress(
		manga_id, last_read_chapter_id, last_read_page, total_pages, is_completed, last_read_at
	) VALUES(?, ?, ?, ?, ?, ?)
	ON CONFLICT(manga_id) DO UPDATE SET last_read_chapter_id=excluded.last_read_chapter_id,
	last_read_page=excluded.last_read_page, total_pages=excluded.total_pages,
	is_completed=reading_progress.is_completed OR excluded.is_completed,
	last_read_at=excluded.last_read_at`,
		progress.MangaID, progress.LastReadChapterID, progress.LastReadPage,
		progress.TotalPages, progress.IsCompleted, progress.LastReadAt)
	if err != nil {
		return ReadingProgress{}, err
	}
	eventID, err := identity.New()
	if err != nil {
		return ReadingProgress{}, err
	}
	// History keeps one row per chapter with the latest page: repeated
	// progress writes refresh the row instead of appending a row per page.
	changed, err := tx.Exec(`UPDATE history_events SET page=?, occurred_at=? WHERE manga_id=? AND chapter_id=?`, progress.LastReadPage, progress.LastReadAt, progress.MangaID, progress.LastReadChapterID)
	if err != nil {
		return ReadingProgress{}, err
	}
	affected, err := changed.RowsAffected()
	if err != nil {
		return ReadingProgress{}, err
	}
	if affected == 0 {
		if _, err := tx.Exec(`INSERT INTO history_events(id,manga_id,chapter_id,page,occurred_at) VALUES(?,?,?,?,?)`, eventID, progress.MangaID, progress.LastReadChapterID, progress.LastReadPage, progress.LastReadAt); err != nil {
			return ReadingProgress{}, err
		}
	}
	if progress.IsCompleted {
		if _, err := tx.Exec(`INSERT INTO chapter_read_state(chapter_id,manga_id,read,read_at) VALUES(?,?,1,?)
			ON CONFLICT(chapter_id) DO UPDATE SET read=1,read_at=excluded.read_at`, progress.LastReadChapterID, progress.MangaID, progress.LastReadAt); err != nil {
			return ReadingProgress{}, err
		}
	}
	if progress.SessionSeconds > 0 {
		sessionID, err := identity.New()
		if err != nil {
			return ReadingProgress{}, err
		}
		if _, err := tx.Exec(`INSERT INTO reading_sessions(id,manga_id,seconds,occurred_at) VALUES(?,?,?,?)`, sessionID, progress.MangaID, progress.SessionSeconds, progress.LastReadAt); err != nil {
			return ReadingProgress{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ReadingProgress{}, err
	}
	return r.GetReadingProgress(progress.MangaID)
}

func (r *Repository) ReadingSeconds(mangaID string) (int64, error) {
	var seconds int64
	err := r.db.Get(&seconds, `SELECT COALESCE(SUM(seconds), 0) FROM reading_sessions WHERE manga_id=?`, mangaID)
	return seconds, err
}

func (r *Repository) TotalReadingSeconds() (int64, error) {
	var seconds int64
	err := r.db.Get(&seconds, `SELECT COALESCE(SUM(seconds), 0) FROM reading_sessions`)
	return seconds, err
}

func (r *Repository) ReadingStats() (ReadingStats, error) {
	var stats ReadingStats
	if err := r.db.Get(&stats.ReadingSeconds, `SELECT COALESCE(SUM(seconds), 0) FROM reading_sessions`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.TitleCount, `SELECT COUNT(*) FROM manga WHERE in_library=1`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.ChapterCount, `SELECT COUNT(*) FROM chapters c JOIN manga m ON m.id=c.manga_id WHERE m.in_library=1`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Select(&stats.Daily, `SELECT date(occurred_at, 'unixepoch') AS date, SUM(seconds) AS seconds FROM reading_sessions GROUP BY date(occurred_at, 'unixepoch') ORDER BY date`); err != nil {
		return ReadingStats{}, err
	}
	if stats.Daily == nil {
		stats.Daily = []ReadingDay{}
	}
	stats.Overview = StatsOverview{
		LibraryMangaCount: stats.TitleCount,
		TotalReadDuration: stats.ReadingSeconds,
	}
	if err := r.db.Get(&stats.Overview.CompletedMangaCount, `SELECT COUNT(*) FROM reading_progress p JOIN manga m ON m.id=p.manga_id WHERE m.in_library=1 AND p.is_completed=1`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.Titles.UpdateEnabledCount, `SELECT COUNT(*) FROM manga WHERE in_library=1 AND download_new_chapters=1`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.Titles.StartedMangaCount, `SELECT COUNT(*) FROM (SELECT manga_id FROM reading_progress UNION SELECT manga_id FROM history_events)`); err != nil {
		return ReadingStats{}, err
	}
	stats.Chapters.TotalChapterCount = stats.ChapterCount
	if err := r.db.Get(&stats.Chapters.ReadChapterCount, `SELECT COUNT(*) FROM chapter_read_state s JOIN chapters c ON c.id=s.chapter_id JOIN manga m ON m.id=c.manga_id WHERE m.in_library=1 AND s.read=1`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.Chapters.DownloadCount, `SELECT COUNT(*) FROM chapters c JOIN manga m ON m.id=c.manga_id WHERE m.in_library=1 AND c.downloaded=1`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.Trackers.TrackedTitleCount, `SELECT COUNT(DISTINCT manga_id) FROM tracker_bindings`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.Trackers.MeanScore, `SELECT COALESCE(AVG(remote_score), 0) FROM tracker_bindings WHERE remote_score IS NOT NULL`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Get(&stats.Trackers.TrackerCount, `SELECT COUNT(DISTINCT tracker_type) FROM tracker_bindings`); err != nil {
		return ReadingStats{}, err
	}
	if err := r.db.Select(&stats.TopTitles, `SELECT s.manga_id AS manga_id, m.title AS title, SUM(s.seconds) AS seconds,
		(SELECT COUNT(*) FROM chapter_read_state cs JOIN chapters c ON c.id=cs.chapter_id WHERE c.manga_id=s.manga_id AND cs.read=1) AS chapters_read
		FROM reading_sessions s JOIN manga m ON m.id=s.manga_id
		GROUP BY s.manga_id, m.title ORDER BY seconds DESC LIMIT 10`); err != nil {
		return ReadingStats{}, err
	}
	if stats.TopTitles == nil {
		stats.TopTitles = []TopTitle{}
	}
	return stats, nil
}

func (r *Repository) GetReadingProgress(mangaID string) (ReadingProgress, error) {
	var p ReadingProgress
	err := r.db.Get(&p, `SELECT manga_id, last_read_chapter_id, last_read_page, total_pages,
		is_completed, last_read_at FROM reading_progress WHERE manga_id = ?`, mangaID)
	return p, err
}

func (r *Repository) SetSetting(key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" || value == "" {
		return errors.New("setting key and value are required")
	}
	_, err := r.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (r *Repository) GetSetting(key string) (Setting, error) {
	var setting Setting
	err := r.db.Get(&setting, `SELECT key,value FROM settings WHERE key=?`, strings.TrimSpace(key))
	return setting, err
}

func (r *Repository) ListSettings() ([]Setting, error) {
	var settings []Setting
	err := r.db.Select(&settings, `SELECT key,value FROM settings ORDER BY key`)
	return settings, err
}

func (r *Repository) SetChapterRead(chapterID, mangaID string, read bool) error {
	chapterID = strings.TrimSpace(chapterID)
	mangaID = strings.TrimSpace(mangaID)
	if chapterID == "" || mangaID == "" {
		return errors.New("chapter id and manga id are required")
	}
	var owner string
	if err := r.db.Get(&owner, `SELECT manga_id FROM chapters WHERE id=?`, chapterID); err != nil {
		return err
	}
	if owner != mangaID {
		return errors.New("chapter does not belong to manga")
	}
	var readAt any
	if read {
		readAt = time.Now().Unix()
	}
	_, err := r.db.Exec(`INSERT INTO chapter_read_state(chapter_id,manga_id,read,read_at) VALUES(?,?,?,?)
		ON CONFLICT(chapter_id) DO UPDATE SET read=excluded.read,read_at=excluded.read_at`, chapterID, mangaID, read, readAt)
	return err
}

func (r *Repository) GetChapterRead(chapterID string) (ChapterReadState, error) {
	var state ChapterReadState
	err := r.db.Get(&state, `SELECT chapter_id,manga_id,read,read_at FROM chapter_read_state WHERE chapter_id=?`, strings.TrimSpace(chapterID))
	return state, err
}

func (r *Repository) ListChapterRead(mangaID string) ([]ChapterReadState, error) {
	var states []ChapterReadState
	err := r.db.Select(&states, `SELECT chapter_id,manga_id,read,read_at FROM chapter_read_state WHERE manga_id=? ORDER BY chapter_id`, mangaID)
	return states, err
}

func (r *Repository) ListHistoryEvents(limit int) ([]HistoryEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	var events []HistoryEvent
	err := r.db.Select(&events, `SELECT id,manga_id,chapter_id,page,occurred_at FROM history_events ORDER BY occurred_at DESC,id DESC LIMIT ?`, limit)
	return events, err
}

func (r *Repository) DeleteHistoryEvent(id string) error {
	result, err := r.db.Exec(`DELETE FROM history_events WHERE id=?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	return requireChange(result, "delete history event")
}

func (r *Repository) RecordUpdate(mangaID, chapterID string, seenAt int64) (UpdateLog, error) {
	id, err := identity.New()
	if err != nil {
		return UpdateLog{}, err
	}
	_, err = r.db.Exec(`INSERT INTO update_log(id,manga_id,chapter_id,seen_at) VALUES(?,?,?,?) ON CONFLICT(manga_id,chapter_id) DO NOTHING`, id, mangaID, chapterID, seenAt)
	if err != nil {
		return UpdateLog{}, err
	}
	var logEntry UpdateLog
	err = r.db.Get(&logEntry, `SELECT id,manga_id,chapter_id,seen_at,acknowledged FROM update_log WHERE manga_id=? AND chapter_id=?`, mangaID, chapterID)
	return logEntry, err
}

func (r *Repository) ListUpdateLogs(all bool) ([]UpdateLog, error) {
	query := `SELECT id,manga_id,chapter_id,seen_at,acknowledged FROM update_log`
	if !all {
		query += ` WHERE acknowledged=0`
	}
	query += ` ORDER BY seen_at DESC,id DESC`
	var logs []UpdateLog
	err := r.db.Select(&logs, query)
	return logs, err
}

func (r *Repository) AcknowledgeUpdates(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	marks := placeholders(len(ids))
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var rows []struct {
		ID        string `db:"id"`
		ChapterID string `db:"chapter_id"`
		MangaID   string `db:"manga_id"`
	}
	if err := tx.Select(&rows, `SELECT id,chapter_id,manga_id FROM update_log WHERE id IN (`+marks+`)`, args...); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE update_log SET acknowledged=1 WHERE id IN (`+marks+`)`, args...); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := tx.Exec(`INSERT INTO chapter_read_state(chapter_id,manga_id,read,read_at) VALUES(?,?,1,?) ON CONFLICT(chapter_id) DO UPDATE SET read=1,read_at=excluded.read_at`, row.ChapterID, row.MangaID, time.Now().Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) SetLibraryUpdateState(status string, at int64) error {
	_, err := r.db.Exec(`INSERT INTO library_update_state(id,last_run_at,last_status) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET last_run_at=excluded.last_run_at,last_status=excluded.last_status`, at, status)
	return err
}

func (r *Repository) GetLibraryUpdateState() (LibraryUpdateState, error) {
	var state LibraryUpdateState
	err := r.db.Get(&state, `SELECT last_run_at,last_status FROM library_update_state WHERE id=1`)
	return state, err
}

func (r *Repository) UpsertTrackerBinding(binding TrackerBinding) (TrackerBinding, error) {
	if strings.TrimSpace(binding.MangaID) == "" || strings.TrimSpace(binding.TrackerType) == "" || strings.TrimSpace(binding.RemoteID) == "" {
		return TrackerBinding{}, errors.New("manga id, tracker type and remote id are required")
	}
	existing, lookupErr := r.GetTrackerBinding(binding.MangaID, binding.TrackerType)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return TrackerBinding{}, lookupErr
	}
	rebound := lookupErr == nil && existing.RemoteID != binding.RemoteID
	_, err := r.db.Exec(`INSERT INTO tracker_bindings(
		manga_id, tracker_type, remote_id, remote_title, remote_score, remote_status,
		last_synced_chapter, total_remote_chapters
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(manga_id, tracker_type) DO UPDATE SET remote_id=excluded.remote_id,
	remote_title=excluded.remote_title,
	remote_score=CASE WHEN tracker_bindings.remote_id=excluded.remote_id
		THEN COALESCE(tracker_bindings.remote_score, excluded.remote_score)
		ELSE excluded.remote_score END,
	remote_status=excluded.remote_status, total_remote_chapters=excluded.total_remote_chapters`,
		binding.MangaID, binding.TrackerType, binding.RemoteID, binding.RemoteTitle,
		binding.RemoteScore, binding.RemoteStatus, binding.LastSyncedChapter, binding.TotalRemoteChapters)
	if err != nil {
		return TrackerBinding{}, err
	}
	if rebound {
		if _, err := r.db.Exec(`DELETE FROM tracker_sync_jobs WHERE binding_id=?`, existing.ID); err != nil {
			return TrackerBinding{}, err
		}
		if _, err := r.db.Exec(`UPDATE tracker_bindings SET last_synced_chapter=0 WHERE id=?`, existing.ID); err != nil {
			return TrackerBinding{}, err
		}
	}
	return r.GetTrackerBinding(binding.MangaID, binding.TrackerType)
}

// UpdateTrackerTracking persists a user's score and date edits on a binding.
// Nil fields keep their stored value.
func (r *Repository) UpdateTrackerTracking(mangaID, trackerType string, score *float64, startedAt, finishedAt *int64) (TrackerBinding, error) {
	binding, err := r.GetTrackerBinding(mangaID, trackerType)
	if err != nil {
		return TrackerBinding{}, err
	}
	if score != nil {
		binding.RemoteScore = score
	}
	if startedAt != nil {
		binding.StartedAt = startedAt
	}
	if finishedAt != nil {
		binding.FinishedAt = finishedAt
	}
	if _, err := r.db.Exec(`UPDATE tracker_bindings SET remote_score=?, started_at=?, finished_at=? WHERE id=?`,
		binding.RemoteScore, binding.StartedAt, binding.FinishedAt, binding.ID); err != nil {
		return TrackerBinding{}, err
	}
	return binding, nil
}

func (r *Repository) GetTrackerBinding(mangaID, trackerType string) (TrackerBinding, error) {
	var b TrackerBinding
	err := r.db.Get(&b, `SELECT id, manga_id, tracker_type, remote_id, remote_title, remote_score,
		remote_status, last_synced_chapter, total_remote_chapters, started_at, finished_at FROM tracker_bindings WHERE manga_id=? AND tracker_type=?`, mangaID, trackerType)
	return b, err
}

func (r *Repository) GetTrackerBindingByID(id int64) (TrackerBinding, error) {
	var b TrackerBinding
	err := r.db.Get(&b, `SELECT id, manga_id, tracker_type, remote_id, remote_title, remote_score, remote_status,
		last_synced_chapter, total_remote_chapters, started_at, finished_at FROM tracker_bindings WHERE id=?`, id)
	return b, err
}

func (r *Repository) ListTrackerBindings(mangaID string) ([]TrackerBinding, error) {
	var out []TrackerBinding
	err := r.db.Select(&out, `SELECT id, manga_id, tracker_type, remote_id, remote_title, remote_score,
		remote_status, last_synced_chapter, total_remote_chapters, started_at, finished_at FROM tracker_bindings WHERE manga_id=? ORDER BY tracker_type`, mangaID)
	return out, err
}
func (r *Repository) DeleteTrackerBinding(mangaID, trackerType string) error {
	result, err := r.db.Exec(`DELETE FROM tracker_bindings WHERE manga_id=? AND tracker_type=?`, mangaID, trackerType)
	if err != nil {
		return err
	}
	return requireChange(result, "delete tracker binding")
}

func (r *Repository) SaveTrackerCredential(trackerType string, accessToken, refreshToken []byte, expiresAt *int64, metadata []byte) error {
	now := time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO tracker_credentials(tracker_type,access_token,refresh_token,expires_at,metadata,created_at,updated_at)
	VALUES(?,?,?,?,?,?,?) ON CONFLICT(tracker_type) DO UPDATE SET access_token=excluded.access_token,refresh_token=excluded.refresh_token,
	expires_at=excluded.expires_at,metadata=excluded.metadata,updated_at=excluded.updated_at`, trackerType, accessToken, refreshToken, expiresAt, metadata, now, now)
	return err
}

func (r *Repository) LoadTrackerCredential(trackerType string) (TrackerCredentialRecord, error) {
	var c TrackerCredentialRecord
	err := r.db.Get(&c, `SELECT tracker_type,access_token,refresh_token,expires_at,metadata FROM tracker_credentials WHERE tracker_type=?`, trackerType)
	return c, err
}

func (r *Repository) DeleteTrackerCredential(trackerType string) error {
	_, err := r.db.Exec(`DELETE FROM tracker_credentials WHERE tracker_type=?`, trackerType)
	return err
}

func (r *Repository) ListTrackerCredentials() ([]TrackerCredential, error) {
	var out []TrackerCredential
	err := r.db.Select(&out, `SELECT tracker_type,expires_at,created_at,updated_at FROM tracker_credentials ORDER BY tracker_type`)
	return out, err
}

func (r *Repository) UpdateTrackerSyncedChapter(bindingID int64, chapter float64) error {
	_, err := r.db.Exec(`UPDATE tracker_bindings SET last_synced_chapter = CASE WHEN last_synced_chapter > ? THEN last_synced_chapter ELSE ? END WHERE id=?`, chapter, chapter, bindingID)
	return err
}

func (r *Repository) EnqueueTrackerSync(mangaID string, bindingID int64, chapter float64) (TrackerSyncJob, error) {
	now := time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO tracker_sync_jobs(manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,created_at)
		VALUES(?,?,?,'PENDING',0,?,?) ON CONFLICT(manga_id,binding_id,chapter_number) DO UPDATE SET
		status=CASE WHEN tracker_sync_jobs.status='COMPLETED' THEN tracker_sync_jobs.status ELSE 'PENDING' END,
		next_attempt_at=CASE WHEN tracker_sync_jobs.status='COMPLETED' THEN tracker_sync_jobs.next_attempt_at ELSE excluded.next_attempt_at END,
		error_message=CASE WHEN tracker_sync_jobs.status='COMPLETED' THEN tracker_sync_jobs.error_message ELSE NULL END`, mangaID, bindingID, chapter, now, now)
	if err != nil {
		return TrackerSyncJob{}, err
	}
	var job TrackerSyncJob
	err = r.db.Get(&job, `SELECT id,manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,error_message,created_at,completed_at FROM tracker_sync_jobs WHERE manga_id=? AND binding_id=? AND chapter_number=?`, mangaID, bindingID, chapter)
	return job, err
}

func (r *Repository) ClaimTrackerSync(now int64) (*TrackerSyncJob, error) {
	return r.claimTrackerSync(now, "")
}

func (r *Repository) ClaimTrackerSyncForTracker(now int64, trackerType string) (*TrackerSyncJob, error) {
	return r.claimTrackerSync(now, trackerType)
}

func (r *Repository) claimTrackerSync(now int64, trackerType string) (*TrackerSyncJob, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var query string
	var args []any
	if trackerType == "" {
		query = `SELECT id FROM tracker_sync_jobs WHERE status='PENDING' AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT 1`
		args = []any{now}
	} else {
		query = `SELECT j.id FROM tracker_sync_jobs j JOIN tracker_bindings b ON b.id=j.binding_id WHERE j.status='PENDING' AND j.next_attempt_at<=? AND b.tracker_type=? ORDER BY j.next_attempt_at,j.id LIMIT 1`
		args = []any{now, trackerType}
	}
	if err := tx.Get(&id, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE tracker_sync_jobs SET status='RUNNING' WHERE id=?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var job TrackerSyncJob
	if err := r.db.Get(&job, `SELECT id,manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,error_message,created_at,completed_at FROM tracker_sync_jobs WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) CompleteTrackerSync(id int64) error {
	now := time.Now().Unix()
	_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='COMPLETED',completed_at=?,error_message=NULL WHERE id=?`, now, id)
	return err
}

func (r *Repository) FailTrackerSync(id int64, retry bool, message string) error {
	if !retry {
		_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='FAILED',attempts=attempts+1,error_message=? WHERE id=?`, message, id)
		return err
	}
	var attempts int
	if err := r.db.Get(&attempts, `SELECT attempts FROM tracker_sync_jobs WHERE id=?`, id); err != nil {
		return err
	}
	attempts++
	delay := int64(1 << min(attempts, 5))
	next := time.Now().Unix() + delay
	_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='PENDING',attempts=?,next_attempt_at=?,error_message=? WHERE id=?`, attempts, next, message, id)
	return err
}

func (r *Repository) ResetInterruptedTrackerSync() error {
	_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='PENDING',next_attempt_at=? WHERE status='RUNNING'`, time.Now().Unix())
	return err
}

// PruneTerminalTrackerSyncJobs trims finished sync history to the most
// recent limit entries so the table cannot grow without bound. It reports
// how many rows were removed.
func (r *Repository) PruneTerminalTrackerSyncJobs(limit int) (int64, error) {
	if limit < 0 {
		limit = 0
	}
	result, err := r.db.Exec(`DELETE FROM tracker_sync_jobs WHERE status IN ('COMPLETED','FAILED') AND id NOT IN (
		SELECT id FROM tracker_sync_jobs WHERE status IN ('COMPLETED','FAILED')
		ORDER BY created_at DESC, id DESC LIMIT ?)`, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *Repository) ListTrackerSyncJobs() ([]TrackerSyncJob, error) {
	var jobs []TrackerSyncJob
	err := r.db.Select(&jobs, `SELECT id,manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,error_message,created_at,completed_at FROM tracker_sync_jobs ORDER BY created_at DESC,id DESC`)
	return jobs, err
}

// EnqueueChapter adds a chapter to the queue, or restarts a failed row so the
// retry begins from scratch. Any other existing row is already queued or
// downloading, so it keeps its position and progress.
func (r *Repository) EnqueueChapter(chapterID string) (DownloadQueueItem, error) {
	now := time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO download_queue(
		chapter_id, status, progress, total_pages, downloaded_pages,
		error_message, queued_at, position
	) VALUES(?, ?, 0, 0, 0, NULL, ?,
		(SELECT COALESCE(MAX(position), 0) + 1 FROM download_queue))
	ON CONFLICT(chapter_id) DO UPDATE SET
		status = CASE
			WHEN download_queue.status = ? THEN excluded.status
			ELSE download_queue.status
		END,
		progress = CASE
			WHEN download_queue.status = ? THEN 0
			ELSE download_queue.progress
		END,
		total_pages = CASE
			WHEN download_queue.status = ? THEN 0
			ELSE download_queue.total_pages
		END,
		downloaded_pages = CASE
			WHEN download_queue.status = ? THEN 0
			ELSE download_queue.downloaded_pages
		END,
		error_message = CASE
			WHEN download_queue.status = ? THEN NULL
			ELSE download_queue.error_message
		END,
		queued_at = CASE
			WHEN download_queue.status = ? THEN excluded.queued_at
			ELSE download_queue.queued_at
		END,
		position = CASE
			WHEN download_queue.status = ? THEN excluded.position
			ELSE download_queue.position
		END`,
		chapterID, QueuePending, now,
		QueueFailed, QueueFailed, QueueFailed, QueueFailed,
		QueueFailed, QueueFailed, QueueFailed)
	if err != nil {
		return DownloadQueueItem{}, fmt.Errorf("enqueue chapter %s: %w", chapterID, err)
	}
	return r.GetQueueItemByChapter(chapterID)
}

func (r *Repository) UpdateQueueProgress(id int64, totalPages, downloadedPages int, queueErr error) error {
	if totalPages < 0 || downloadedPages < 0 || downloadedPages > totalPages {
		return errors.New("invalid queue progress")
	}
	progress := 0
	if totalPages > 0 {
		progress = downloadedPages * 100 / totalPages
	}
	var message *string
	if queueErr != nil {
		value := queueErr.Error()
		message = &value
	}
	result, err := r.db.Exec(`UPDATE download_queue SET
		status = CASE WHEN status = ? THEN ? ELSE status END,
		progress = ?, total_pages = ?, downloaded_pages = ?, error_message = ?
		WHERE id = ? AND status IN (?, ?)`, QueuePending, QueueDownloading,
		progress, totalPages, downloadedPages, message, id,
		QueuePending, QueueDownloading)
	if err != nil {
		return err
	}
	return requireChange(result, "update queue progress")
}

// SaveQueuePageProgress persists the set of page indexes already fetched for
// an in-flight download. An interrupted or failed attempt resumes from this
// record instead of refetching every page.
func (r *Repository) SaveQueuePageProgress(id int64, totalPages int, done []int) error {
	if totalPages < 0 || len(done) > totalPages {
		return errors.New("invalid queue page progress")
	}
	encoded, err := json.Marshal(done)
	if err != nil {
		return err
	}
	progress := 0
	if totalPages > 0 {
		progress = len(done) * 100 / totalPages
	}
	result, err := r.db.Exec(`UPDATE download_queue SET
		downloaded_pages = ?, progress = ?, done_pages = ?
		WHERE id = ? AND status IN (?, ?)`,
		len(done), progress, string(encoded), id, QueuePending, QueueDownloading)
	if err != nil {
		return err
	}
	return requireChange(result, "save queue page progress")
}

func (r *Repository) MarkQueueFailed(id int64, queueErr error) error {
	message := "download failed"
	if queueErr != nil {
		message = queueErr.Error()
	}
	result, err := r.db.Exec(`UPDATE download_queue SET status = ?, error_message = ?
		WHERE id = ? AND status = ?`, QueueFailed, message, id, QueueDownloading)
	if err != nil {
		return err
	}
	return requireChange(result, "mark queue item failed")
}

// MarkChapterDownloaded records a chapter artifact. The queue is left alone, so
// this is the entry point for chapters that were never queued (imports, manual
// records); downloads that ran through the queue go through
// CompleteQueueDownload instead.
func (r *Repository) MarkChapterDownloaded(chapterID, path string) error {
	return r.writeDownloadedChapter(chapterID, path, nil)
}

// CompleteQueueDownload records a chapter artifact and drops the queue row it
// was downloaded for, in one transaction. The delete only matches a row that is
// still downloading, so an item canceled while the archive was being written
// fails here instead of being marked downloaded.
func (r *Repository) CompleteQueueDownload(chapterID, path string) error {
	return r.writeDownloadedChapter(chapterID, path, func(tx *sqlx.Tx) error {
		result, err := tx.Exec(`DELETE FROM download_queue WHERE chapter_id = ? AND status = ?`,
			chapterID, QueueDownloading)
		if err != nil {
			return err
		}
		if err := requireChange(result, "complete queue item"); err != nil {
			return fmt.Errorf("download canceled before completion")
		}
		return nil
	})
}

// writeDownloadedChapter marks the chapter downloaded and then runs the
// optional queue bookkeeping in the same transaction, so a failure there rolls
// the chapter state back with it.
func (r *Repository) writeDownloadedChapter(chapterID, path string, queueStep func(*sqlx.Tx) error) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`UPDATE chapters SET downloaded = 1, download_path = ? WHERE id = ?`, path, chapterID)
	if err != nil {
		return err
	}
	if err := requireChange(result, "mark chapter downloaded"); err != nil {
		return err
	}
	if queueStep != nil {
		if err := queueStep(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) PauseQueueItem(id int64) error {
	return r.transitionQueueItem(id, QueuePaused, QueuePending, QueueDownloading)
}

func (r *Repository) ResumeQueueItem(id int64) error {
	return r.transitionQueueItem(id, QueuePending, QueuePaused)
}

// CancelQueueItem drops a row from the queue. Cancelling is terminal: the row
// has no work left to do, so nothing is kept for it to resume from.
func (r *Repository) CancelQueueItem(id int64) error {
	result, err := r.db.Exec(`DELETE FROM download_queue WHERE id = ? AND status IN (?, ?, ?, ?)`,
		id, QueuePending, QueueDownloading, QueuePaused, QueueFailed)
	if err != nil {
		return err
	}
	return requireChange(result, "cancel queue item")
}

// RetryQueueItem returns a failed download to the pending state so the
// worker can pick it up again.
func (r *Repository) RetryQueueItem(id int64) error {
	return r.transitionQueueItem(id, QueuePending, QueueFailed)
}

// ReleaseQueueItem returns a claimed item to the pending state so it can be
// picked up later. Progress columns stay untouched, which lets a released item
// resume from its staged pages. Rows that are no longer downloading (for
// example canceled meanwhile) are left as they are.
func (r *Repository) ReleaseQueueItem(id int64) error {
	_, err := r.db.Exec(`UPDATE download_queue SET status = ? WHERE id = ? AND status = ?`,
		QueuePending, id, QueueDownloading)
	return err
}

// CancelAllQueueItems removes every row that can still make progress and
// returns them in queue order, so callers can announce one removal per row.
func (r *Repository) CancelAllQueueItems() ([]DownloadQueueItem, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var items []DownloadQueueItem
	if err := tx.Select(&items, queueSelect+`
		WHERE q.status IN (?, ?, ?, ?) ORDER BY q.position, q.queued_at, q.id`,
		QueuePending, QueueDownloading, QueuePaused, QueueFailed); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(`DELETE FROM download_queue
		WHERE status IN (?, ?, ?, ?)`,
		QueuePending, QueueDownloading, QueuePaused, QueueFailed); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// SetQueueOrder rewrites the claim order from the given item ids in display
// order. Ids that no longer exist are skipped so a client working from a
// slightly stale snapshot cannot fail the whole reorder.
func (r *Repository) SetQueueOrder(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for position, id := range ids {
		if id < 1 {
			return fmt.Errorf("set queue order: invalid item id %d", id)
		}
		if _, err := tx.Exec(`UPDATE download_queue SET position = ? WHERE id = ?`, position, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) transitionQueueItem(id int64, target string, allowed ...string) error {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(allowed)), ",")
	args := make([]any, 0, len(allowed)+2)
	args = append(args, target, id)
	for _, status := range allowed {
		args = append(args, status)
	}
	result, err := r.db.Exec(`UPDATE download_queue SET status = ? WHERE id = ? AND status IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	return requireChange(result, "change queue item status")
}

func (r *Repository) ResetInterruptedQueue() error {
	_, err := r.db.Exec(`UPDATE download_queue SET status = ? WHERE status = ?`, QueuePending, QueueDownloading)
	return err
}

// RetryFailedQueueItemsBySource moves every failed item downloaded from one
// source back to the pending state and returns the affected ids in queue
// order, so callers can announce one transition per row. Progress and staged
// pages are kept, matching the per-item retry.
func (r *Repository) RetryFailedQueueItemsBySource(sourceID string) ([]int64, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var ids []int64
	if err := tx.Select(&ids, `SELECT q.id FROM download_queue q
		JOIN chapters c ON c.id = q.chapter_id
		WHERE q.status = ? AND c.source_id = ? ORDER BY q.position, q.queued_at, q.id`,
		QueueFailed, sourceID); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(`UPDATE download_queue SET status = ?
		WHERE status = ? AND chapter_id IN (SELECT id FROM chapters WHERE source_id = ?)`,
		QueuePending, QueueFailed, sourceID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

// GetSourceDownloadPrefs returns the pacing override stored for a source, or
// nil when the user has set none.
func (r *Repository) GetSourceDownloadPrefs(sourceID string) (*SourceDownloadPrefs, error) {
	var prefs SourceDownloadPrefs
	err := r.db.Get(&prefs, `SELECT source_id, interval_ms, max_attempts, backoff_ms, burst
		FROM source_download_prefs WHERE source_id = ?`, sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &prefs, nil
}

// SetSourceDownloadPrefs stores the pacing override for a source. A row whose
// fields are all nil is deleted, which returns the source to following its
// own suggestions and the global defaults.
func (r *Repository) SetSourceDownloadPrefs(prefs SourceDownloadPrefs) error {
	if prefs.IntervalMs == nil && prefs.MaxAttempts == nil && prefs.BackoffMs == nil && prefs.Burst == nil {
		_, err := r.db.Exec(`DELETE FROM source_download_prefs WHERE source_id = ?`, prefs.SourceID)
		return err
	}
	_, err := r.db.Exec(`INSERT INTO source_download_prefs (source_id, interval_ms, max_attempts, backoff_ms, burst, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id) DO UPDATE SET
			interval_ms = excluded.interval_ms,
			max_attempts = excluded.max_attempts,
			backoff_ms = excluded.backoff_ms,
			burst = excluded.burst,
			updated_at = excluded.updated_at`,
		prefs.SourceID, prefs.IntervalMs, prefs.MaxAttempts, prefs.BackoffMs, prefs.Burst, time.Now().UnixMilli())
	return err
}

// SourceConcurrency is the resolved chapter cap of one source for a claim.
type SourceConcurrency struct {
	SourceID string
	Max      int
}

// ClaimOptions carries the runtime gates a claim must respect. The zero value
// claims strictly in queue order, which is what a one-shot drain wants: it
// runs to completion under its own worker count. Rows of paused sources are
// never claimed, a source never has more than its resolved cap of chapters in
// flight, and at most MaxActiveSources distinct sources download at once. A
// source absent from Caps uses DefaultCap; each gate is ignored when its
// value is not positive.
type ClaimOptions struct {
	PausedSources    []string
	Caps             []SourceConcurrency
	DefaultCap       int
	MaxActiveSources int
}

// ClaimNextQueueItem atomically moves the next claimable pending row into the
// downloading state. The gated statement runs inside one transaction and the
// pool holds a single connection, so two workers cannot overshoot a cap.
func (r *Repository) ClaimNextQueueItem(options ClaimOptions) (*DownloadQueueItem, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	query := `SELECT q.id FROM download_queue q JOIN chapters c ON c.id = q.chapter_id WHERE q.status = ?`
	args := []any{QueuePending}
	if len(options.PausedSources) > 0 {
		query += ` AND c.source_id NOT IN (` + placeholders(len(options.PausedSources)) + `)`
		for _, sourceID := range options.PausedSources {
			args = append(args, sourceID)
		}
	}
	if options.DefaultCap > 0 {
		query += ` AND (SELECT COUNT(*) FROM download_queue d JOIN chapters dc ON dc.id = d.chapter_id
			WHERE d.status = ? AND dc.source_id = c.source_id)
			< COALESCE((SELECT cap FROM caps WHERE caps.source_id = c.source_id), ?)`
		args = append(args, QueueDownloading, options.DefaultCap)
		// The caps CTE carries the sources whose resolved cap differs from
		// the default; without entries it is empty and COALESCE always wins.
		if len(options.Caps) > 0 {
			values := strings.TrimSuffix(strings.Repeat("(?,?),", len(options.Caps)), ",")
			capArgs := make([]any, 0, len(options.Caps)*2)
			for _, cap := range options.Caps {
				capArgs = append(capArgs, cap.SourceID, cap.Max)
			}
			query = `WITH caps(source_id, cap) AS (VALUES ` + values + `) ` + query
			args = append(capArgs, args...)
		} else {
			query = `WITH caps(source_id, cap) AS (SELECT '', 0 WHERE 0) ` + query
		}
	}
	if options.MaxActiveSources > 0 {
		query += ` AND (EXISTS (SELECT 1 FROM download_queue d JOIN chapters dc ON dc.id = d.chapter_id
				WHERE d.status = ? AND dc.source_id = c.source_id)
			OR (SELECT COUNT(DISTINCT dc.source_id) FROM download_queue d JOIN chapters dc ON dc.id = d.chapter_id
				WHERE d.status = ?) < ?)`
		args = append(args, QueueDownloading, QueueDownloading, options.MaxActiveSources)
	}
	query += ` ORDER BY q.position, q.queued_at, q.id LIMIT 1`

	var id int64
	if err := tx.Get(&id, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	result, err := tx.Exec(`UPDATE download_queue SET status = ?, error_message = NULL
		WHERE id = ? AND status = ?`, QueueDownloading, id, QueuePending)
	if err != nil {
		return nil, err
	}
	if err := requireChange(result, "claim queue item"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	item, err := r.GetQueueItem(id)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// QueueSourceIDs lists the sources that have live rows in the download queue,
// so the downloader can resolve their concurrency caps before claiming.
func (r *Repository) QueueSourceIDs() ([]string, error) {
	ids := []string{}
	err := r.db.Select(&ids, `SELECT DISTINCT c.source_id FROM download_queue q
		JOIN chapters c ON c.id = q.chapter_id
		WHERE q.status IN (?, ?, ?, ?) ORDER BY c.source_id`,
		QueuePending, QueueDownloading, QueuePaused, QueueFailed)
	return ids, err
}

func (r *Repository) GetQueueItem(id int64) (DownloadQueueItem, error) {
	var item DownloadQueueItem
	err := r.db.Get(&item, queueSelect+` WHERE q.id = ?`, id)
	return item, err
}

func (r *Repository) GetQueueItemByChapter(chapterID string) (DownloadQueueItem, error) {
	var item DownloadQueueItem
	err := r.db.Get(&item, queueSelect+` WHERE q.chapter_id = ?`, chapterID)
	return item, err
}

func (r *Repository) ListQueue() ([]DownloadQueueItem, error) {
	var items []DownloadQueueItem
	err := r.db.Select(&items, queueSelect+` ORDER BY q.position, q.queued_at, q.id`)
	return items, err
}

const queueSelect = `SELECT
	q.id, q.chapter_id, q.status, q.progress, q.total_pages,
	q.downloaded_pages, q.done_pages, q.error_message, q.queued_at,
	q.position,
	c.manga_id, cs.source_chapter_id, c.chapter_number, c.volume,
	c.title AS chapter_title, c.language, c.uploaded_at, c.scanlator,
	c.source_id, m.title AS manga_title,
	m.description AS manga_description, m.authors AS manga_authors,
	m.artists AS manga_artists, m.genres AS manga_genres, m.download_format,
	s.name AS source_name
	FROM download_queue q
	JOIN chapters c ON c.id = q.chapter_id
	LEFT JOIN chapter_sources cs ON cs.chapter_id = c.id AND cs.source_id = c.source_id
	JOIN manga m ON m.id = c.manga_id
	JOIN sources s ON s.id = c.source_id`

func requireChange(result sql.Result, action string) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%s: item was not in an allowed state", action)
	}
	return nil
}
