package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

var errEngineUnavailable = errors.New("engine is unavailable")

type migrationCandidate struct {
	Source engine.InstalledSource `json:"source"`
	Result engine.MangaItem       `json:"result"`
}

type migrationResponse struct {
	Manga      db.MangaAggregate `json:"manga"`
	Source     string            `json:"source"`
	ChapterMap map[string]string `json:"chapterMap"`
}

func (s *Server) mountMigration(r chi.Router) {
	r.Route("/manga/{mangaID}/migration", func(migration chi.Router) {
		migration.Get("/candidates", s.migrationCandidates)
		migration.Post("/apply", s.applyMigration)
	})
}

func (s *Server) migrationCandidates(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	manga, err := s.repo.GetManga(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	sources, err := s.engine.Installed()
	if err != nil {
		writeError(w, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = manga.Title
	}
	results := make([]migrationCandidate, 0)
	for _, source := range sources {
		if source.ID == manga.SourceID {
			continue
		}
		page, searchErr := s.engine.Search(r.Context(), source.ID, engine.SearchQuery{Query: query, Page: 1})
		if searchErr != nil {
			continue
		}
		for _, result := range page.Items {
			materialized, materializeErr := s.repo.UpsertManga(db.Manga{SourceID: source.ID, SourceMangaID: result.ID, Title: result.Title, CoverURL: result.CoverURL, Status: "unknown"})
			if materializeErr != nil {
				continue
			}
			result.ID = materialized.ID
			results = append(results, migrationCandidate{Source: source, Result: result})
		}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) applyMigration(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	var body struct {
		SourceID string `json:"sourceId"`
		MangaID  string `json:"mangaId"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	body.SourceID = strings.TrimSpace(body.SourceID)
	body.MangaID = strings.TrimSpace(body.MangaID)
	if body.SourceID == "" || body.MangaID == "" {
		writeBadRequest(w, "sourceId and mangaId are required")
		return
	}
	oldID := chi.URLParam(r, "mangaID")
	old, err := s.repo.GetManga(oldID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if old.ID == body.MangaID || strings.TrimSpace(body.SourceID) == "" {
		writeBadRequest(w, "a distinct replacement manga is required")
		return
	}
	aggregate, err := s.repo.AttachMangaSource(oldID, body.MangaID)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, migrationResponse{Manga: aggregate, Source: body.SourceID, ChapterMap: map[string]string{}})
}
