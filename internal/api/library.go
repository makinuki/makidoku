package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
)

// mountLibrary registers the local library endpoints. The library grows with
// the download and tracking subsystems; category management is what the reader
// needs today.
func (s *Server) mountLibrary(r chi.Router) {
	r.Get("/categories", s.listCategories)
	r.Post("/categories", s.createCategory)
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
