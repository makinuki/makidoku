package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// mountLibrary registers the local library endpoints. The library grows with
// the download and tracking subsystems; category management is what the reader
// needs today.
func (s *Server) mountLibrary(r chi.Router) {
	r.Get("/library", s.listLibrary)
	r.Get("/manga/{mangaID}", s.getManga)
	r.Get("/manga/{mangaID}/cover", s.mangaCover)
	r.Post("/manga/{mangaID}/library", s.addMangaByID)
	r.Delete("/manga/{mangaID}/library", s.removeMangaByID)
	r.Post("/manga/{mangaID}/refresh", s.refreshManga)
	r.Get("/manga/{mangaID}/chapters", s.getMangaChapters)
	r.Post("/manga/{mangaID}/categories/{categoryID}", s.addMangaCategoryByID)
	r.Delete("/manga/{mangaID}/categories/{categoryID}", s.removeMangaCategoryByID)
	r.Get("/history", s.listHistory)
	r.Get("/categories", s.listCategories)
	r.Post("/categories", s.createCategory)
	r.Patch("/categories/{categoryID}", s.updateCategory)
	r.Delete("/categories/{categoryID}", s.deleteCategory)
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
	writeJSON(w, http.StatusOK, aggregate)
}

// refreshManga re-pulls details for a stored title on demand. A failure keeps
// the previously stored aggregate intact for the next cache read.
func (s *Server) refreshManga(w http.ResponseWriter, r *http.Request) {
	mangaID := chi.URLParam(r, "mangaID")
	source, err := s.repo.GetMangaSource(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	aggregate, err := s.fetchAndStoreDetails(r, source)
	if err != nil {
		writeError(w, err)
		return
	}
	aggregate.SourceName = s.sourceName(aggregate.Manga.SourceID)
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
	for _, item := range details.Chapters {
		if _, err := s.repo.UpsertChapter(db.Chapter{MangaID: updated.ID, SourceID: source.SourceID, SourceChapterID: item.ID, ChapterNumber: item.Number, Volume: item.Volume, Title: stringPointer(item.Title), Language: stringPointer(item.Language), UploadedAt: item.UploadedAt, Scanlator: stringPointer(item.Scanlator)}); err != nil {
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
	if manga.CoverCachePath != nil && *manga.CoverCachePath != "" {
		if data, readErr := os.ReadFile(*manga.CoverCachePath); readErr == nil {
			contentType := "application/octet-stream"
			if manga.CoverContentType != nil && *manga.CoverContentType != "" {
				contentType = *manga.CoverContentType
			}
			writeCoverBytes(w, contentType, data)
			return
		}
	}
	if manga.CoverURL == "" {
		writeError(w, engine.CodedError(engine.CodeNotFound, "manga has no cover"))
		return
	}
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	source, err := s.repo.GetMangaSource(manga.ID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	data, err := s.engine.FetchImage(r.Context(), source.SourceID, manga.CoverURL, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(data) == 0 {
		writeError(w, engine.CodedError(engine.CodeNotFound, "cover data is empty"))
		return
	}
	cacheRoot := filepath.Join(s.engine.DataDir(), "covers")
	key := fmt.Sprintf("%x", sha256.Sum256(append([]byte(manga.CoverURL), data...)))
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	path := filepath.Join(cacheRoot, key+".img")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	contentType := http.DetectContentType(data)
	if err := s.repo.SetMangaCover(manga.ID, path, contentType, time.Now().Unix()); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeCoverBytes(w, contentType, data)
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
	if _, err := s.repo.SetMangaLibrary(mangaID, true); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	aggregate, err := s.repo.GetMangaAggregate(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	aggregate.SourceName = s.sourceName(aggregate.Manga.SourceID)
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

func (s *Server) getMangaChapters(w http.ResponseWriter, r *http.Request) {
	chapters, err := s.repo.ListChapters(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if chapters == nil {
		chapters = []db.Chapter{}
	}
	writeJSON(w, http.StatusOK, chapters)
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
	history, err := s.repo.ListHistory()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if history == nil {
		history = []db.HistoryItem{}
	}
	writeJSON(w, http.StatusOK, history)
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
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
