package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/makinuki/makidoku/internal/identity"
)

// This file holds the read and write paths for the library metadata a restore
// can carry beyond the core rows: browse feeds, saved searches, merged
// sources, alternative titles and tags, and the free-form metadata record.

// ListImportedSources returns the placeholder sources a restore created for
// titles whose original source is not installed. Their titles can be moved
// onto an installed source once one is available.
func (r *Repository) ListImportedSources() ([]Source, error) {
	sources := []Source{}
	if err := r.db.Select(&sources, `SELECT id,COALESCE(plugin_key,'') AS plugin_key,name,version,abi_version,lang,base_url,COALESCE(wasm_path,'') AS wasm_path,installed_at
		FROM sources WHERE installed=0 AND id LIKE 'imported-%' ORDER BY name`); err != nil {
		return nil, err
	}
	return sources, nil
}

// ListFeeds returns every browse feed ordered by source and feed order.
func (r *Repository) ListFeeds() ([]Feed, error) {
	feeds := []Feed{}
	if err := r.db.Select(&feeds, `SELECT id,source_id,is_global,feed_order FROM source_feeds ORDER BY source_id, feed_order, id`); err != nil {
		return nil, err
	}
	return feeds, nil
}

// ListSavedSearches returns the saved searches of one source ordered by their
// recorded position. An empty source id returns every saved search.
func (r *Repository) ListSavedSearches(sourceID string) ([]SavedSearch, error) {
	searches := []SavedSearch{}
	query := `SELECT id,source_id,feed_id,name,query,filters,search_order FROM saved_searches`
	args := []any{}
	if strings.TrimSpace(sourceID) != "" {
		query += ` WHERE source_id=?`
		args = append(args, sourceID)
	}
	query += ` ORDER BY source_id, search_order, id`
	if err := r.db.Select(&searches, query, args...); err != nil {
		return nil, err
	}
	return searches, nil
}

// CreateSavedSearch stores one saved search. An empty id is generated.
func (r *Repository) CreateSavedSearch(search SavedSearch) (SavedSearch, error) {
	search.SourceID = strings.TrimSpace(search.SourceID)
	search.Name = strings.TrimSpace(search.Name)
	if search.SourceID == "" || search.Name == "" {
		return SavedSearch{}, errors.New("saved search needs a source and a name")
	}
	if search.ID == "" {
		id, err := identity.New()
		if err != nil {
			return SavedSearch{}, err
		}
		search.ID = id
	}
	if strings.TrimSpace(search.Filters) == "" {
		search.Filters = "[]"
	}
	if _, err := r.db.Exec(`INSERT INTO saved_searches(id,source_id,feed_id,name,query,filters,search_order)
		VALUES(?,?,?,?,?,?,?)`,
		search.ID, search.SourceID, search.FeedID, search.Name, search.Query, search.Filters, search.SearchOrder); err != nil {
		return SavedSearch{}, fmt.Errorf("create saved search: %w", err)
	}
	return search, nil
}

// DeleteSavedSearch removes one saved search.
func (r *Repository) DeleteSavedSearch(id string) error {
	_, err := r.db.Exec(`DELETE FROM saved_searches WHERE id=?`, id)
	return err
}

// ListMangaMerges returns the merged sources of one title.
func (r *Repository) ListMangaMerges(mangaID string) ([]MangaMerge, error) {
	merges := []MangaMerge{}
	if err := r.db.Select(&merges, `SELECT id,manga_id,source_id,source_manga_id,url,is_info_manga,
		get_chapter_updates,chapter_sort_mode,chapter_priority,download_chapters,merge_order
		FROM manga_merges WHERE manga_id=? ORDER BY merge_order, id`, mangaID); err != nil {
		return nil, err
	}
	return merges, nil
}

// AddMangaMerge records a merged source and links it through manga_sources so
// the refresh path can pull its chapters.
func (r *Repository) AddMangaMerge(merge MangaMerge) (MangaMerge, error) {
	merge.SourceMangaID = strings.TrimSpace(merge.SourceMangaID)
	if merge.MangaID == "" || merge.SourceID == "" || merge.SourceMangaID == "" {
		return MangaMerge{}, errors.New("a merge needs a title, a source and a source manga id")
	}
	if merge.ID == "" {
		id, err := identity.New()
		if err != nil {
			return MangaMerge{}, err
		}
		merge.ID = id
	}
	now := time.Now().Unix()
	tx, err := r.db.Beginx()
	if err != nil {
		return MangaMerge{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO manga_sources(manga_id,source_id,source_manga_id,url,is_primary,first_seen_at,last_seen_at)
		VALUES(?,?,?,?,0,?,?)
		ON CONFLICT(source_id,source_manga_id) DO UPDATE SET last_seen_at=excluded.last_seen_at,
			url=COALESCE(NULLIF(excluded.url,''),url)`,
		merge.MangaID, merge.SourceID, merge.SourceMangaID, merge.URL, now, now); err != nil {
		return MangaMerge{}, fmt.Errorf("link merged source: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO manga_merges(id,manga_id,source_id,source_manga_id,url,is_info_manga,
		get_chapter_updates,chapter_sort_mode,chapter_priority,download_chapters,merge_order)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(manga_id,source_id,source_manga_id) DO UPDATE SET url=excluded.url,
			is_info_manga=excluded.is_info_manga, get_chapter_updates=excluded.get_chapter_updates,
			chapter_sort_mode=excluded.chapter_sort_mode, chapter_priority=excluded.chapter_priority,
			download_chapters=excluded.download_chapters, merge_order=excluded.merge_order`,
		merge.ID, merge.MangaID, merge.SourceID, merge.SourceMangaID, merge.URL, merge.IsInfoManga,
		merge.GetChapterUpdates, merge.ChapterSortMode, merge.ChapterPriority, merge.DownloadChapters, merge.MergeOrder); err != nil {
		return MangaMerge{}, fmt.Errorf("record merge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MangaMerge{}, err
	}
	return merge, nil
}

// DeleteMangaMerge removes the merge record and its link, but keeps any
// chapters the merged source already contributed.
func (r *Repository) DeleteMangaMerge(id string) error {
	var merge MangaMerge
	if err := r.db.Get(&merge, `SELECT manga_id,source_id,source_manga_id FROM manga_merges WHERE id=?`, id); err != nil {
		return err
	}
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM manga_merges WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM manga_sources WHERE manga_id=? AND source_id=? AND source_manga_id=? AND is_primary=0`,
		merge.MangaID, merge.SourceID, merge.SourceMangaID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMangaTitles returns the alternative titles of one title.
func (r *Repository) ListMangaTitles(mangaID string) ([]MangaTitle, error) {
	titles := []MangaTitle{}
	if err := r.db.Select(&titles, `SELECT id,manga_id,title,title_type FROM manga_titles WHERE manga_id=? ORDER BY title_type, title`, mangaID); err != nil {
		return nil, err
	}
	return titles, nil
}

// ListMangaTags returns the tags of one title.
func (r *Repository) ListMangaTags(mangaID string) ([]MangaTag, error) {
	tags := []MangaTag{}
	if err := r.db.Select(&tags, `SELECT id,manga_id,namespace,name,tag_type FROM manga_tags WHERE manga_id=? ORDER BY tag_type, name`, mangaID); err != nil {
		return nil, err
	}
	return tags, nil
}

// GetMangaMetadata returns the free-form metadata record of one title. It
// reports sql.ErrNoRows when the title carries none.
func (r *Repository) GetMangaMetadata(mangaID string) (MangaMetadata, error) {
	var metadata MangaMetadata
	err := r.db.Get(&metadata, `SELECT manga_id,uploader,extra,indexed_extra,extra_version FROM manga_metadata WHERE manga_id=?`, mangaID)
	return metadata, err
}

// MangaCustomUpdate carries the per-field overrides of a title. A nil field is
// left unchanged; an empty string clears the override.
type MangaCustomUpdate struct {
	Title       *string `json:"title"`
	Artist      *string `json:"artist"`
	Author      *string `json:"author"`
	Description *string `json:"description"`
	Genres      *string `json:"genres"`
	Status      *string `json:"status"`
	CoverURL    *string `json:"coverUrl"`
}

// UpdateMangaCustom applies a custom-info update and returns the stored title.
// Clearing a custom cover drops the cached rendition so the source cover is
// fetched again.
func (r *Repository) UpdateMangaCustom(id string, update MangaCustomUpdate) (Manga, error) {
	manga, err := r.GetManga(id)
	if err != nil {
		return Manga{}, err
	}
	manga.CustomTitle = applyCustom(update.Title, manga.CustomTitle)
	manga.CustomArtist = applyCustom(update.Artist, manga.CustomArtist)
	manga.CustomAuthor = applyCustom(update.Author, manga.CustomAuthor)
	manga.CustomDescription = applyCustom(update.Description, manga.CustomDescription)
	manga.CustomGenres = applyCustom(update.Genres, manga.CustomGenres)
	manga.CustomStatus = applyCustom(update.Status, manga.CustomStatus)
	manga.CustomCoverURL = applyCustom(update.CoverURL, manga.CustomCoverURL)
	if _, err := r.db.Exec(`UPDATE manga SET custom_title=?, custom_artist=?, custom_author=?, custom_description=?,
		custom_genres=?, custom_status=?, custom_cover_url=? WHERE id=?`,
		manga.CustomTitle, manga.CustomArtist, manga.CustomAuthor, manga.CustomDescription,
		manga.CustomGenres, manga.CustomStatus, manga.CustomCoverURL, id); err != nil {
		return Manga{}, fmt.Errorf("update custom info: %w", err)
	}
	if update.CoverURL != nil {
		if _, err := r.db.Exec(`UPDATE manga SET cover_cache_path=NULL, cover_content_type=NULL, cover_fetched_at=NULL WHERE id=?`, id); err != nil {
			return Manga{}, fmt.Errorf("invalidate cover cache: %w", err)
		}
	}
	return r.GetManga(id)
}

// applyCustom resolves one field of a custom-info update. A nil update leaves
// the stored value; an empty string clears it.
func applyCustom(update *string, stored *string) *string {
	if update == nil {
		return stored
	}
	value := strings.TrimSpace(*update)
	if value == "" {
		return nil
	}
	return &value
}

// SetChapterBookmark sets the bookmark flag of one chapter.
func (r *Repository) SetChapterBookmark(chapterID string, bookmark bool) error {
	flag := 0
	if bookmark {
		flag = 1
	}
	result, err := r.db.Exec(`UPDATE chapters SET bookmark=? WHERE id=?`, flag, chapterID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}
