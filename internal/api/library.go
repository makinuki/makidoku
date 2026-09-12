package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/chapterrecog"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// RefreshManga refreshes one library title and returns canonical chapter IDs
// that were not present before the refresh. It is shared by the HTTP refresh
// endpoint and the background updater.
func (s *Server) RefreshManga(ctx context.Context, mangaID string) ([]string, error) {
	source, err := s.repo.GetMangaSource(mangaID)
	if err != nil {
		return nil, err
	}
	before, err := s.repo.ListChapters(mangaID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(before))
	for _, chapter := range before {
		known[chapter.ID] = struct{}{}
	}
	details, err := s.engine.Details(ctx, source.SourceID, source.SourceMangaID)
	if err != nil {
		return nil, err
	}
	// A source that re-issues a chapter under a new identifier, for example
	// after rotating a URL suffix, must update the existing local chapter
	// instead of forking a duplicate or reporting a new release.
	chapters := withDerivedChapterNumbers(details.Title, details.Chapters)
	adopted := adoptedChapterIDs(before, chapters)
	updated, err := s.repo.UpsertManga(db.Manga{ID: source.MangaID, SourceID: source.SourceID, SourceMangaID: source.SourceMangaID, Title: details.Title, AltTitles: jsonString(details.AltTitles), Description: stringPointer(details.Description), Authors: jsonString(details.Authors), Artists: jsonString(details.Artists), Genres: jsonString(details.Genres), Status: details.Status, CoverURL: engine.SelectCover(details.CoverURL, details.Covers, engine.PreferredCoverWidth)})
	if err != nil {
		return nil, err
	}
	newIDs := []string{}
	for _, item := range chapters {
		chapter, err := s.repo.UpsertChapter(db.Chapter{
			ID:              adopted[item.ID],
			MangaID:         updated.ID,
			SourceID:        source.SourceID,
			SourceChapterID: item.ID,
			ChapterNumber:   item.Number,
			Volume:          item.Volume,
			Title:           stringPointer(item.Title),
			Language:        stringPointer(item.Language),
			UploadedAt:      item.UploadedAt,
			Scanlator:       stringPointer(item.Scanlator),
		})
		if err != nil {
			return nil, err
		}
		if _, ok := known[chapter.ID]; !ok {
			newIDs = append(newIDs, chapter.ID)
		}
	}
	if err := s.repo.SetMangaDetailsFetched(updated.ID, time.Now().Unix()); err != nil {
		return nil, err
	}
	return newIDs, nil
}

// withDerivedChapterNumbers fills in a number for chapters the source left
// unnumbered, using the chapter title. Deriving the number before matching and
// storage keeps identity, ordering, and migration consistent for releases that
// carry no explicit number. A declared number is never replaced.
func withDerivedChapterNumbers(mangaTitle string, chapters []engine.ChapterItem) []engine.ChapterItem {
	out := make([]engine.ChapterItem, len(chapters))
	for i, item := range chapters {
		out[i] = item
		if item.Number == nil {
			out[i].Number = chapterrecog.Parse(mangaTitle, item.Title, nil)
		}
	}
	return out
}

// adoptedChapterIDs maps an incoming source chapter to a stored chapter the
// source has re-issued under a different locator, so a refresh rewrites the
// stored locator instead of inserting a second row and leaving the reading
// state, bookmark, and downloads behind on the first. Candidates are paired in
// order of decreasing confidence:
//
//  1. the stored locator is the chapter id the source publishes, which means
//     the pair is already linked and nothing has to move;
//  2. the stored locator is the page URL the source publishes;
//  3. the number with the scanlator and the language it appeared under,
//     either of which may be absent;
//  4. the number with the title it appeared under;
//  5. the title alone, which is all an unnumbered extra or prologue carries.
//
// Only one-to-one matches are paired. A key that fits more than one stored
// chapter or more than one incoming chapter is skipped, because a chapter left
// unlinked is better than reading state attached to the wrong one. No pass
// requires a number to be present, and the title passes compare titles so an
// unnumbered extra or prologue still matches the row it was recorded on.
func adoptedChapterIDs(before []db.Chapter, incoming []engine.ChapterItem) map[string]string {
	passes := []struct {
		stored func(db.Chapter) (string, bool)
		item   func(engine.ChapterItem) (string, bool)
		adopt  bool
	}{
		{
			stored: func(chapter db.Chapter) (string, bool) { return locatorIdentity(chapter.SourceChapterID) },
			item:   func(item engine.ChapterItem) (string, bool) { return locatorIdentity(item.ID) },
		},
		{
			stored: func(chapter db.Chapter) (string, bool) { return locatorIdentity(chapter.SourceChapterID) },
			item:   func(item engine.ChapterItem) (string, bool) { return locatorIdentity(item.URL) },
			adopt:  true,
		},
		{
			stored: func(chapter db.Chapter) (string, bool) {
				return numberAttributesIdentity(chapter.ChapterNumber, textValue(chapter.Scanlator), textValue(chapter.Language))
			},
			item: func(item engine.ChapterItem) (string, bool) {
				return numberAttributesIdentity(item.Number, item.Scanlator, item.Language)
			},
			adopt: true,
		},
		{
			stored: func(chapter db.Chapter) (string, bool) {
				return numberTitleIdentity(chapter.ChapterNumber, textValue(chapter.Title))
			},
			item: func(item engine.ChapterItem) (string, bool) {
				return numberTitleIdentity(item.Number, item.Title)
			},
			adopt: true,
		},
		{
			stored: func(chapter db.Chapter) (string, bool) { return titleIdentity(textValue(chapter.Title)) },
			item:   func(item engine.ChapterItem) (string, bool) { return titleIdentity(item.Title) },
			adopt:  true,
		},
	}

	stored := indexSet(len(before))
	pending := indexSet(len(incoming))
	adopted := map[string]string{}
	for _, pass := range passes {
		for _, pair := range oneToOnePairs(stored, pending, before, incoming, pass.stored, pass.item) {
			if pass.adopt {
				adopted[incoming[pair[1]].ID] = before[pair[0]].ID
			}
			delete(stored, pair[0])
			delete(pending, pair[1])
		}
	}
	return adopted
}

// oneToOnePairs returns the stored and incoming chapters that share a key
// under one pass of the ladder, and only where the key fits exactly one
// chapter on each side.
func oneToOnePairs(stored, pending map[int]bool, before []db.Chapter, incoming []engine.ChapterItem,
	storedKey func(db.Chapter) (string, bool), itemKey func(engine.ChapterItem) (string, bool)) [][2]int {

	storedByKey := map[string]int{}
	storedShared := map[string]bool{}
	for index := range stored {
		key, ok := storedKey(before[index])
		if !ok {
			continue
		}
		if _, seen := storedByKey[key]; seen {
			storedShared[key] = true
			continue
		}
		storedByKey[key] = index
	}
	itemByKey := map[string]int{}
	itemShared := map[string]bool{}
	for index := range pending {
		key, ok := itemKey(incoming[index])
		if !ok {
			continue
		}
		if _, seen := itemByKey[key]; seen {
			itemShared[key] = true
			continue
		}
		itemByKey[key] = index
	}
	pairs := make([][2]int, 0, len(storedByKey))
	for key, storedIndex := range storedByKey {
		if storedShared[key] || itemShared[key] {
			continue
		}
		itemIndex, ok := itemByKey[key]
		if !ok {
			continue
		}
		pairs = append(pairs, [2]int{storedIndex, itemIndex})
	}
	return pairs
}

// locatorIdentity keys a locator exactly. A locator is an opaque string that
// the source that recorded it reads, so it is compared as written.
func locatorIdentity(locator string) (string, bool) {
	locator = strings.TrimSpace(locator)
	return locator, locator != ""
}

// numberAttributesIdentity keys a chapter by its number together with the
// attributes it was published under. An attribute may be absent, so a chapter
// recorded without a scanlator still matches one published without one.
func numberAttributesIdentity(number *float64, attributes ...string) (string, bool) {
	if number == nil {
		return "", false
	}
	key := strconv.FormatFloat(*number, 'f', -1, 64)
	for _, attribute := range attributes {
		key += "\x00" + normalizeText(attribute)
	}
	return key, true
}

// numberTitleIdentity keys a chapter by its number and its title, both of
// which have to be present: a number alone is not an identity.
func numberTitleIdentity(number *float64, title string) (string, bool) {
	title = normalizeText(title)
	if number == nil || title == "" {
		return "", false
	}
	return strconv.FormatFloat(*number, 'f', -1, 64) + "\x00" + title, true
}

// titleIdentity keys a chapter by its title alone. An empty title is not a key,
// because it would make every untitled chapter interchangeable.
func titleIdentity(title string) (string, bool) {
	title = normalizeText(title)
	return title, title != ""
}

// normalizeText folds case and whitespace so two publications of one chapter
// compare equal across cosmetic differences.
func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

// textValue reads a nullable stored string.
func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// indexSet returns the indexes of a slice as a set.
func indexSet(size int) map[int]bool {
	set := make(map[int]bool, size)
	for index := 0; index < size; index++ {
		set[index] = true
	}
	return set
}

// mountLibrary registers the local library endpoints. The library grows with
// the download and tracking subsystems; category management is what the reader
// needs today.
func (s *Server) mountLibrary(r chi.Router) {
	r.Get("/library", s.listLibrary)
	r.Post("/library/bulk/read", s.bulkSetRead)
	r.Post("/library/bulk/library", s.bulkSetLibrary)
	r.Post("/library/bulk/category", s.bulkSetCategory)
	r.Post("/library/bulk/download", s.bulkDownload)
	r.Delete("/library/bulk", s.bulkRemoveFromLibrary)
	r.Get("/manga/{mangaID}", s.getManga)
	r.Get("/manga/{mangaID}/cover", s.mangaCover)
	r.Post("/manga/{mangaID}/library", s.addMangaByID)
	r.Delete("/manga/{mangaID}/library", s.removeMangaByID)
	r.Patch("/manga/{mangaID}/downloads", s.updateMangaDownloads)
	r.Patch("/manga/{mangaID}/reader", s.updateMangaReader)
	r.Post("/manga/{mangaID}/refresh", s.refreshManga)
	r.Post("/manga/{mangaID}/categories/{categoryID}", s.addMangaCategoryByID)
	r.Delete("/manga/{mangaID}/categories/{categoryID}", s.removeMangaCategoryByID)
	r.Get("/history", s.listHistory)
	r.Get("/stats", s.stats)
	r.Delete("/history/{eventID}", s.deleteHistoryEvent)
	r.Post("/chapters/{chapterID}/read", s.setChapterRead)
	r.Post("/manga/{mangaID}/chapters/read", s.setMangaChaptersRead)
	r.Get("/categories", s.listCategories)
	r.Post("/categories", s.createCategory)
	r.Patch("/categories/{categoryID}", s.updateCategory)
	r.Delete("/categories/{categoryID}", s.deleteCategory)
}

func (s *Server) updateMangaDownloads(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	manga, err := s.repo.SetMangaDownloadNewChapters(chi.URLParam(r, "mangaID"), body.Enabled)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, manga)
}

// updateMangaReader records per-title reader overrides. A field that is absent
// keeps its stored value; an explicit null clears the override so the reader
// falls back to the global setting; a string replaces it.
func (s *Server) updateMangaReader(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if !decodeBody(w, r, &body) {
		return
	}
	mangaID := chi.URLParam(r, "mangaID")
	manga, err := s.repo.GetManga(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	mode, err := mergeReaderOverride(body, "mode", manga.ReaderMode)
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	direction, err := mergeReaderOverride(body, "direction", manga.ReaderDirection)
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	fit, err := mergeReaderOverride(body, "fit", manga.ReaderFit)
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	updated, err := s.repo.SetMangaReaderOverrides(mangaID, mode, direction, fit)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// mergeReaderOverride resolves one override field from a request body. An
// absent key keeps the stored value, null or an empty string clears it, and a
// string replaces it.
func mergeReaderOverride(body map[string]json.RawMessage, key string, stored *string) (*string, error) {
	raw, ok := body[key]
	if !ok {
		return stored, nil
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s must be a string or null", key)
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	return &trimmed, nil
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.repo.ReadingStats()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// sourceSeriesURL returns the source page to open for a stored title. A
// locator recorded from a listing wins; otherwise the title is looked up once
// through the search export, the only contract channel that declares a series
// URL. The plugin base URL is the last resort, so the link stays usable while
// the locator is unknown.
func (s *Server) sourceSeriesURL(ctx context.Context, manga db.Manga) string {
	source, err := s.repo.GetMangaSource(manga.ID)
	if err != nil {
		return s.sourceURL(manga.SourceID)
	}
	if source.URL != "" {
		return source.URL
	}
	if resolved := s.searchSeriesURL(ctx, manga.Title, source); resolved != "" {
		return resolved
	}
	return s.sourceURL(manga.SourceID)
}

// searchSeriesURL matches a title against its source and persists the locator
// of the listing the title was created from. A title that does not match is
// attempted once per process so a miss cannot drive a source request on every
// read.
func (s *Server) searchSeriesURL(ctx context.Context, title string, source db.MangaSource) string {
	if s.engine == nil || strings.TrimSpace(title) == "" {
		return ""
	}
	if _, seen := s.seriesURLLookups.LoadOrStore(source.MangaID, struct{}{}); seen {
		return ""
	}
	result, err := s.engine.Search(ctx, source.SourceID, engine.SearchQuery{Query: title, Page: 1})
	if err != nil {
		slog.Debug("series url lookup failed", "manga", source.MangaID, "err", err)
		return ""
	}
	for _, item := range result.Items {
		if item.ID != source.SourceMangaID || item.URL == "" {
			continue
		}
		if err := s.repo.SetSourceURL(source.MangaID, source.SourceID, item.URL); err != nil {
			slog.Warn("series url not persisted", "manga", source.MangaID, "err", err)
		}
		return item.URL
	}
	return ""
}

// getManga is local-first: stored details serve the read directly so the
// library works without connectivity. Only records that never completed a
// details fetch resolve the plugin once to materialize chapters.
func (s *Server) getManga(w http.ResponseWriter, r *http.Request) {
	mangaID := chi.URLParam(r, "mangaID")
	aggregate, err := s.repo.GetMangaAggregate(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if aggregate.Manga.DetailsFetchedAt == nil {
		source, err := s.repo.GetMangaSource(mangaID)
		if err != nil {
			writeLocalError(w, http.StatusNotFound, err)
			return
		}
		if s.engine == nil {
			writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
			return
		}
		if aggregate, err = s.fetchAndStoreDetails(r, source); err != nil {
			writeError(w, err)
			return
		}
	}
	aggregate.SourceName = s.sourceName(aggregate.Manga.SourceID)
	aggregate.SourceURL = s.sourceSeriesURL(r.Context(), aggregate.Manga)
	writeJSON(w, http.StatusOK, aggregate)
}

// refreshManga re-pulls details for a stored title on demand. A failure keeps
// the previously stored aggregate intact for the next cache read.
func (s *Server) refreshManga(w http.ResponseWriter, r *http.Request) {
	mangaID := chi.URLParam(r, "mangaID")
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	newIDs, err := s.RefreshManga(r.Context(), mangaID)
	if err != nil {
		writeError(w, err)
		return
	}
	// The enqueue performs its own source round trip, so it must survive the
	// client disconnecting while the response is still being prepared.
	if err := s.EnqueueNewChapters(s.Lifetime(), mangaID, newIDs); err != nil {
		slog.Warn("automatic download enqueue failed", "manga", mangaID, "err", err)
	}
	aggregate, err := s.repo.GetMangaAggregate(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	aggregate.SourceName = s.sourceName(aggregate.Manga.SourceID)
	aggregate.SourceURL = s.sourceSeriesURL(r.Context(), aggregate.Manga)
	writeJSON(w, http.StatusOK, aggregate)
}

// fetchAndStoreDetails pulls full details through the plugin, persists manga
// and chapters, and stamps the freshness marker.
func (s *Server) fetchAndStoreDetails(r *http.Request, source db.MangaSource) (db.MangaAggregate, error) {
	details, err := s.engine.Details(r.Context(), source.SourceID, source.SourceMangaID)
	if err != nil {
		return db.MangaAggregate{}, err
	}
	updated, err := s.repo.UpsertManga(db.Manga{ID: source.MangaID, SourceID: source.SourceID, SourceMangaID: source.SourceMangaID, Title: details.Title, AltTitles: jsonString(details.AltTitles), Description: stringPointer(details.Description), Authors: jsonString(details.Authors), Artists: jsonString(details.Artists), Genres: jsonString(details.Genres), Status: details.Status, CoverURL: engine.SelectCover(details.CoverURL, details.Covers, engine.PreferredCoverWidth)})
	if err != nil {
		return db.MangaAggregate{}, err
	}
	before, err := s.repo.ListChapters(updated.ID)
	if err != nil {
		return db.MangaAggregate{}, err
	}
	adopted := adoptedChapterIDs(before, details.Chapters)
	for _, item := range details.Chapters {
		if _, err := s.repo.UpsertChapter(db.Chapter{ID: adopted[item.ID], MangaID: updated.ID, SourceID: source.SourceID, SourceChapterID: item.ID, ChapterNumber: item.Number, Volume: item.Volume, Title: stringPointer(item.Title), Language: stringPointer(item.Language), UploadedAt: item.UploadedAt, Scanlator: stringPointer(item.Scanlator)}); err != nil {
			return db.MangaAggregate{}, err
		}
	}
	if err := s.repo.SetMangaDetailsFetched(updated.ID, time.Now().Unix()); err != nil {
		return db.MangaAggregate{}, err
	}
	return s.repo.GetMangaAggregate(updated.ID)
}

// mangaCover streams a canonical manga cover. Source URLs and request details
// stay backend-owned; clients only ever see this route.
func (s *Server) mangaCover(w http.ResponseWriter, r *http.Request) {
	manga, err := s.repo.GetManga(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	data, contentType, err := s.ensureCover(manga)
	if err != nil {
		writeCoverError(w, err)
		return
	}
	writeCoverBytes(w, contentType, data)
}

// ensureCover returns the bytes of a title's cover, reading them from the local
// cache when present and fetching them otherwise. The transfer runs under the
// daemon's own image budget rather than the request context so a navigation
// cannot abandon a download whose result is cached for the next view.
func (s *Server) ensureCover(manga db.Manga) ([]byte, string, error) {
	if data, contentType, ok := readCachedCover(manga); ok {
		return data, contentType, nil
	}
	// A user override wins over the source cover. The override is stored as a
	// URL and is fetched through the same path as the source cover.
	coverURL := manga.CoverURL
	if manga.CustomCoverURL != nil && strings.TrimSpace(*manga.CustomCoverURL) != "" {
		coverURL = strings.TrimSpace(*manga.CustomCoverURL)
	}
	if coverURL == "" {
		return nil, "", engine.CodedError(engine.CodeNotFound, "manga has no cover")
	}
	if s.engine == nil {
		return nil, "", errEngineUnavailable
	}
	source, err := s.repo.GetMangaSource(manga.ID)
	if err != nil {
		return nil, "", err
	}
	if err := s.beginImageFetch(s.Lifetime()); err != nil {
		return nil, "", err
	}
	defer s.endImageFetch()
	// A request that waited for a slot may find the cover already stored by the
	// transfer it waited behind.
	if data, contentType, ok := readCachedCover(manga); ok {
		return data, contentType, nil
	}
	ctx, cancel := s.imageFetchContext()
	defer cancel()
	data, err := s.engine.FetchImage(ctx, source.SourceID, coverURL, nil)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", engine.CodedError(engine.CodeNotFound, "cover data is empty")
	}
	contentType := http.DetectContentType(data)
	if err := s.storeCover(manga, coverURL, data, contentType); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

// readCachedCover returns the stored cover bytes of a title when a readable
// cache file exists.
func readCachedCover(manga db.Manga) ([]byte, string, bool) {
	if manga.CoverCachePath == nil || *manga.CoverCachePath == "" {
		return nil, "", false
	}
	data, err := os.ReadFile(*manga.CoverCachePath)
	if err != nil {
		return nil, "", false
	}
	contentType := "application/octet-stream"
	if manga.CoverContentType != nil && *manga.CoverContentType != "" {
		contentType = *manga.CoverContentType
	}
	return data, contentType, true
}

// storeCover writes a fetched cover to the cache directory and records its
// location so later requests serve it from disk.
func (s *Server) storeCover(manga db.Manga, coverURL string, data []byte, contentType string) error {
	cacheRoot := filepath.Join(s.engine.DataDir(), "covers")
	key := fmt.Sprintf("%x", sha256.Sum256(append([]byte(coverURL), data...)))
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return err
	}
	path := filepath.Join(cacheRoot, key+".img")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	if err := s.repo.SetMangaCover(manga.ID, path, contentType, time.Now().Unix()); err != nil {
		return err
	}
	return nil
}

// writeCoverError maps a cover failure onto the status the web client sees.
// Coded source failures keep their standardized status; local failures are
// reported as such instead of borrowing a source code.
func writeCoverError(w http.ResponseWriter, err error) {
	var coded *engine.Error
	if errors.As(err, &coded) {
		writeError(w, err)
		return
	}
	switch {
	case errors.Is(err, errEngineUnavailable):
		writeLocalError(w, http.StatusServiceUnavailable, err)
	case errors.Is(err, sql.ErrNoRows):
		writeLocalError(w, http.StatusNotFound, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeLocalError(w, http.StatusServiceUnavailable, err)
	default:
		writeLocalError(w, http.StatusInternalServerError, err)
	}
}

func writeCoverBytes(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// addMangaByID marks a canonical manga as in the library. The aggregate is
// returned so clients can navigate straight to the details view.
func (s *Server) addMangaByID(w http.ResponseWriter, r *http.Request) {
	mangaID := chi.URLParam(r, "mangaID")
	wasInLibrary := false
	if current, err := s.repo.GetManga(mangaID); err == nil {
		wasInLibrary = current.InLibrary
	}
	if _, err := s.repo.SetMangaLibrary(mangaID, true); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if s.settings != nil && !wasInLibrary {
		if enabled, settingErr := s.settings.Bool("downloads.auto_download"); settingErr == nil {
			if _, updateErr := s.repo.SetMangaDownloadNewChapters(mangaID, enabled); updateErr != nil {
				writeLocalError(w, http.StatusInternalServerError, updateErr)
				return
			}
		}
	}
	aggregate, err := s.repo.GetMangaAggregate(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	aggregate.SourceName = s.sourceName(aggregate.Manga.SourceID)
	aggregate.SourceURL = s.sourceSeriesURL(r.Context(), aggregate.Manga)
	writeJSON(w, http.StatusOK, aggregate)
}

func (s *Server) removeMangaByID(w http.ResponseWriter, r *http.Request) {
	manga, err := s.repo.SetMangaLibrary(chi.URLParam(r, "mangaID"), false)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, manga)
}

func (s *Server) addMangaCategoryByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "categoryID must be a positive integer")
		return
	}
	if err := s.repo.SetMangaCategory(chi.URLParam(r, "mangaID"), id, true); err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMangaCategoryByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "categoryID must be a positive integer")
		return
	}
	if err := s.repo.SetMangaCategory(chi.URLParam(r, "mangaID"), id, false); err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listLibrary(w http.ResponseWriter, r *http.Request) {
	categoryID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("category")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 {
			writeBadRequest(w, "category must be a positive integer")
			return
		}
		categoryID = value
	}
	items, err := s.repo.ListLibrary(r.URL.Query().Get("q"), categoryID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []db.LibraryManga{}
	}
	for i := range items {
		items[i].SourceName = s.sourceName(items[i].SourceID)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listHistory(w http.ResponseWriter, r *http.Request) {
	events, err := s.repo.ListHistoryEvents(200)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	type historyRecord struct {
		ID         string      `json:"id"`
		Manga      db.Manga    `json:"manga"`
		Chapter    *db.Chapter `json:"chapter,omitempty"`
		Page       *int        `json:"page,omitempty"`
		OccurredAt int64       `json:"occurredAt"`
	}
	records := make([]historyRecord, 0, len(events))
	for _, event := range events {
		manga, err := s.repo.GetManga(event.MangaID)
		if err != nil {
			continue
		}
		var chapter *db.Chapter
		if event.ChapterID != nil {
			if value, err := s.repo.GetChapter(*event.ChapterID); err == nil {
				chapter = &value
			}
		}
		records = append(records, historyRecord{ID: event.ID, Manga: manga, Chapter: chapter, Page: event.Page, OccurredAt: event.OccurredAt})
	}
	if records == nil {
		records = []historyRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) deleteHistoryEvent(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.DeleteHistoryEvent(chi.URLParam(r, "eventID")); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setChapterRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Read bool `json:"read"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	chapterID := chi.URLParam(r, "chapterID")
	chapter, err := s.repo.GetChapter(chapterID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if err := s.repo.SetChapterRead(chapterID, chapter.MangaID, body.Read); err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	state, err := s.repo.GetChapterRead(chapterID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) setMangaChaptersRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChapterIDs []string `json:"chapterIds"`
		Read       bool     `json:"read"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	mangaID := chi.URLParam(r, "mangaID")
	for _, chapterID := range body.ChapterIDs {
		if err := s.repo.SetChapterRead(chapterID, mangaID, body.Read); err != nil {
			writeLocalError(w, http.StatusBadRequest, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := s.repo.ListCategories()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if categories == nil {
		categories = []db.Category{}
	}
	writeJSON(w, http.StatusOK, categories)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeBadRequest(w, "name is required")
		return
	}
	category, err := s.repo.CreateCategory(name, body.SortOrder)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, category)
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "categoryID must be a positive integer")
		return
	}
	var body struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	category, err := s.repo.UpdateCategory(id, body.Name, body.SortOrder)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, category)
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "categoryID must be a positive integer")
		return
	}
	if err := s.repo.DeleteCategory(id); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
