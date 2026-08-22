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
	r.Route("/manga/{sourceId}/{mangaId}/migration", func(migration chi.Router) {
		migration.Get("/candidates", s.migrationCandidates)
		migration.Post("/apply", s.applyMigration)
	})
}

func (s *Server) migrationCandidates(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	manga, err := s.repo.GetManga(composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId")))
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
	oldID := composeMangaID(chi.URLParam(r, "sourceId"), chi.URLParam(r, "mangaId"))
	old, err := s.repo.GetManga(oldID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if old.SourceID == body.SourceID {
		writeBadRequest(w, "replacement source must differ from the current source")
		return
	}
	details, err := s.engine.Details(r.Context(), body.SourceID, body.MangaID)
	if err != nil {
		writeError(w, err)
		return
	}
	replacement := db.Manga{
		SourceID: body.SourceID, SourceMangaID: details.ID, Title: details.Title,
		AltTitles: jsonString(details.AltTitles), Description: stringPointer(details.Description),
		Authors: jsonString(details.Authors), Artists: jsonString(details.Artists), Genres: jsonString(details.Genres),
		Status: details.Status, CoverURL: details.CoverURL, DownloadFormat: old.DownloadFormat,
	}
	if replacement.SourceMangaID == "" {
		replacement.SourceMangaID = body.MangaID
	}
	chapters := make([]db.Chapter, 0, len(details.Chapters))
	for _, chapter := range details.Chapters {
		chapters = append(chapters, db.Chapter{SourceChapterID: chapter.ID, ChapterNumber: chapter.Number, Title: stringPointer(chapter.Title), Language: stringPointer(chapter.Language), UploadedAt: chapter.UploadedAt, Scanlator: stringPointer(chapter.Scanlator)})
	}
	aggregate, err := s.repo.MigrateManga(oldID, replacement, chapters)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, migrationResponse{Manga: aggregate, Source: body.SourceID, ChapterMap: map[string]string{}})
}
