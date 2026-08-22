package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
)

// mountLibrary registers the local library endpoints. The library grows with
// the download and tracking subsystems; category management is what the reader
// needs today.
func (s *Server) mountLibrary(r chi.Router) {
	r.Get("/library", s.listLibrary)
	r.Get("/library/{sourceId}/{mangaId}", s.getLibraryManga)
	r.Post("/library/{sourceId}/{mangaId}", s.addLibraryManga)
	r.Delete("/library/{sourceId}/{mangaId}", s.removeLibraryManga)
	r.Get("/history", s.listHistory)
	r.Get("/categories", s.listCategories)
	r.Post("/categories", s.createCategory)
	r.Patch("/categories/{categoryID}", s.updateCategory)
	r.Delete("/categories/{categoryID}", s.deleteCategory)
	r.Post("/manga/{sourceId}/{mangaId}/categories/{categoryID}", s.addMangaCategory)
	r.Delete("/manga/{sourceId}/{mangaId}/categories/{categoryID}", s.removeMangaCategory)
}

func composeMangaID(sourceID, mangaID string) string {
	return strings.TrimSpace(sourceID) + ":" + strings.TrimSpace(mangaID)
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
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getLibraryManga(w http.ResponseWriter, r *http.Request) {
	aggregate, err := s.repo.GetMangaAggregate(composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeLocalError(w, http.StatusNotFound, err)
			return
		}
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, aggregate)
}

func (s *Server) addLibraryManga(w http.ResponseWriter, r *http.Request) {
	manga, err := s.repo.SetMangaLibrary(composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId")), true)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, manga)
}

func (s *Server) removeLibraryManga(w http.ResponseWriter, r *http.Request) {
	manga, err := s.repo.SetMangaLibrary(composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId")), false)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, manga)
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

func (s *Server) addMangaCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "categoryID must be a positive integer")
		return
	}
	if err := s.repo.SetMangaCategory(composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId")), id, true); err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMangaCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || id < 1 {
		writeBadRequest(w, "categoryID must be a positive integer")
		return
	}
	if err := s.repo.SetMangaCategory(composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId")), id, false); err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeOptionalObject decodes a query JSON object without accepting arbitrary
// request bodies on the image proxy.
func decodeOptionalObject(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
