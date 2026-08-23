package api

import (
	"crypto/sha256"
	"encoding/json"
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

// mountSources registers the source management and browsing endpoints. Title
// and chapter identifiers travel as query parameters because sources are free
// to use slugs and full URLs as identifiers.
func (s *Server) mountSources(r chi.Router) {
	r.Get("/sources", s.listSources)
	r.Get("/sources/catalog", s.catalog)
	r.Post("/sources/install", s.installSource)
	r.Route("/sources/{sourceID}", func(source chi.Router) {
		source.Get("/", s.getSource)
		source.Delete("/", s.uninstallSource)
		source.Get("/filters", s.sourceFilters)
		source.Get("/search", s.search)
		source.Post("/library", s.saveSourceManga)
		source.Post("/clearance", s.submitClearance)
	})
}

func (s *Server) saveSourceManga(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MangaID string `json:"mangaId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	mangaID := strings.TrimSpace(body.MangaID)
	if mangaID == "" {
		writeBadRequest(w, "mangaId is required")
		return
	}
	if _, err := s.repo.SetMangaLibrary(mangaID, true); err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	aggregate, err := s.repo.GetMangaAggregate(mangaID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, aggregate)
}

func jsonString(values []string) *string {
	if len(values) == 0 {
		return nil
	}
	b, _ := json.Marshal(values)
	v := string(b)
	return &v
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	installed, err := s.engine.Installed()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, installed)
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	entries, err := s.engine.Catalog(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request) {
	source, err := s.engine.Get(chi.URLParam(r, "sourceID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, source)
}

// installSource installs a catalog source by id, or a locally built binary by
// path.
func (s *Server) installSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if !decodeBody(w, r, &body) {
		return
	}

	var (
		source engine.InstalledSource
		err    error
	)
	switch {
	case strings.TrimSpace(body.ID) != "":
		source, err = s.engine.Install(r.Context(), strings.TrimSpace(body.ID))
	case strings.TrimSpace(body.Path) != "":
		source, err = s.engine.InstallFile(r.Context(), strings.TrimSpace(body.Path))
	default:
		writeBadRequest(w, "provide either a catalog id or a path to a plugin binary")
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, source)
}

func (s *Server) uninstallSource(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.Uninstall(r.Context(), chi.URLParam(r, "sourceID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sourceFilters(w http.ResponseWriter, r *http.Request) {
	filters, err := s.engine.Filters(r.Context(), chi.URLParam(r, "sourceID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, filters)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}

	search := engine.SearchQuery{Query: query.Get("q"), Page: page}
	if raw := query.Get("filters"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &search.Filters); err != nil {
			writeBadRequest(w, "filters must be a JSON object: "+err.Error())
			return
		}
	}

	result, err := s.engine.Search(r.Context(), chi.URLParam(r, "sourceID"), search)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		manga, upsertErr := s.repo.UpsertManga(db.Manga{SourceID: chi.URLParam(r, "sourceID"), SourceMangaID: item.ID, Title: item.Title, CoverURL: item.CoverURL, Status: "unknown"})
		if upsertErr != nil {
			writeLocalError(w, http.StatusConflict, upsertErr)
			return
		}
		items = append(items, map[string]any{"id": manga.ID, "title": item.Title, "coverUrl": "/api/manga/" + manga.ID + "/cover", "latestChapter": item.LatestChapter, "url": ""})
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": result.Page, "hasNextPage": result.HasNextPage, "items": items})
}

func (s *Server) details(w http.ResponseWriter, r *http.Request) {
	mangaID := r.URL.Query().Get("mangaId")
	if mangaID == "" {
		writeBadRequest(w, "mangaId is required")
		return
	}
	details, err := s.engine.Details(r.Context(), chi.URLParam(r, "sourceID"), mangaID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *Server) pages(w http.ResponseWriter, r *http.Request) {
	chapterID := r.URL.Query().Get("chapterId")
	if chapterID == "" {
		writeBadRequest(w, "chapterId is required")
		return
	}
	pages, err := s.engine.Pages(r.Context(), chi.URLParam(r, "sourceID"), chapterID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pages)
}

func (s *Server) materializePages(w http.ResponseWriter, r *http.Request) {
	chapterID := chi.URLParam(r, "chapterID")
	chapter, err := s.repo.GetChapter(chapterID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	pageItems, err := s.engine.Pages(r.Context(), chapter.SourceID, chapter.SourceChapterID)
	if err != nil {
		writeError(w, err)
		return
	}
	rows := make([]db.Page, 0, len(pageItems))
	for _, item := range pageItems {
		headers, marshalErr := json.Marshal(item.Headers)
		if marshalErr != nil {
			writeLocalError(w, http.StatusInternalServerError, marshalErr)
			return
		}
		headersJSON := string(headers)
		rows = append(rows, db.Page{ChapterID: chapter.ID, PageIndex: item.Index, RemoteURL: item.URL, HeadersJSON: &headersJSON, IsScrambled: item.IsScrambled})
	}
	pages, err := s.repo.UpsertPages(chapter.ID, chapter.SourceID, rows)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	if pages == nil {
		pages = []db.Page{}
	}
	writeJSON(w, http.StatusOK, pages)
}

func (s *Server) pageImage(w http.ResponseWriter, r *http.Request) {
	page, err := s.repo.GetPage(chi.URLParam(r, "pageID"))
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	cacheRoot := filepath.Join(s.engine.DataDir(), "image-cache")
	if cache, err := s.repo.GetPageCache(page.ID); err == nil {
		if data, readErr := os.ReadFile(cache.BytePath); readErr == nil {
			w.Header().Set("Content-Type", cache.ContentType)
			w.Header().Set("Cache-Control", "private, max-age=86400")
			_, _ = w.Write(data)
			return
		}
	}
	chapter, err := s.repo.GetChapter(page.ChapterID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	var headers map[string]string
	if page.HeadersJSON != nil && *page.HeadersJSON != "" {
		if err := json.Unmarshal([]byte(*page.HeadersJSON), &headers); err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
	}
	data, err := s.engine.FetchImage(r.Context(), chapter.SourceID, page.RemoteURL, headers)
	if err != nil {
		writeError(w, err)
		return
	}
	if page.IsScrambled {
		data, err = s.engine.Unscramble(r.Context(), chapter.SourceID, data)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if len(data) == 0 {
		writeError(w, engine.CodedError(engine.CodeUnscrambleFailed, "image data is empty"))
		return
	}
	key := fmt.Sprintf("%x", sha256.Sum256(append([]byte(page.RemoteURL), data...)))
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
	if err := s.repo.SetPageCache(page.ID, key, path, contentType, int64(len(data)), time.Now().Unix()); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	s.enforceImageCacheRetention()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// enforceImageCacheRetention applies the bounded retention policy after cache
// writes. Sweeps are throttled so a burst of image requests performs at most
// one directory walk per interval.
func (s *Server) enforceImageCacheRetention() {
	if s.imageCache == nil {
		return
	}
	s.imageCache.AfterWrite(func() (map[string]bool, error) {
		paths, err := s.repo.ListCachedPaths()
		if err != nil {
			return nil, err
		}
		keep := make(map[string]bool, len(paths))
		for _, path := range paths {
			keep[path] = true
		}
		return keep, nil
	})
}

// submitClearance records the cf_clearance cookie and matching user agent an
// operator obtained by solving a challenge in a browser. The stored cookie is
// never read back through the API.
func (s *Server) submitClearance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cookie    string `json:"cookie"`
		UserAgent string `json:"userAgent"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	sourceID := chi.URLParam(r, "sourceID")
	if err := s.engine.SubmitClearance(sourceID, strings.TrimSpace(body.Cookie), strings.TrimSpace(body.UserAgent)); err != nil {
		writeError(w, err)
		return
	}
	source, err := s.engine.Get(sourceID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, source)
}
