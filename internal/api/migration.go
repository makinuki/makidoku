package api

import (
	"errors"
	"log"
	"net/http"
	"os"
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
			materialized, materializeErr := s.repo.UpsertMangaStub(db.Manga{SourceID: source.ID, SourceMangaID: result.ID, Title: result.Title, CoverURL: engine.SelectCover(result.CoverURL, result.Covers, engine.PreferredCoverWidth), Status: "unknown"})
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
	currentSource, err := s.repo.GetMangaSource(oldID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if oldID == body.MangaID {
		writeBadRequest(w, "a distinct replacement manga is required")
		return
	}
	// The replacement must belong to the claimed source.
	discoveredSource, err := s.repo.GetMangaSource(body.MangaID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if discoveredSource.SourceID != body.SourceID {
		writeBadRequest(w, "the replacement manga does not belong to the claimed source")
		return
	}
	// A same-plugin migration would merge duplicate chapter sets while
	// retiring nothing.
	if discoveredSource.SourceID == currentSource.SourceID {
		writeBadRequest(w, "choose a replacement from another plugin")
		return
	}
	// A replacement that is itself in the library would survive the attach as
	// an empty ghost entry; refuse until it is removed.
	replacement, err := s.repo.GetManga(body.MangaID)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	if replacement.InLibrary {
		writeBadRequest(w, "the replacement title is already in your library; remove it before migrating")
		return
	}

	// Retire the previous source's chapters and attach the replacement in one
	// transaction, remembering the reading state for remapping once the
	// replacement's chapters are known.
	progress, progressErr := s.repo.GetReadingProgress(oldID)
	hasProgress := progressErr == nil
	retired, artifacts, err := s.repo.MigrateMangaSource(oldID, discoveredSource.SourceID, body.MangaID)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	for _, path := range artifacts {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("migration: removing artifact %s failed: %v", path, err)
		}
	}

	// Materialize the replacement's chapters right away so the title opens
	// with a consistent list. A failed fetch keeps the migration; the title
	// simply stays empty until the next refresh.
	source, err := s.repo.GetMangaSource(oldID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	aggregate, fetchErr := s.fetchAndStoreDetails(r, source)
	if fetchErr != nil {
		log.Printf("migration: replacement fetch for %s failed: %v", oldID, fetchErr)
		aggregate, err = s.repo.GetMangaAggregate(oldID)
		if err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
	}

	// Remap reading state and report the old-to-new chapter matches.
	chapterMap := map[string]string{}
	for i := range retired {
		if next := findSameNumber(aggregate.Chapters, retired[i].ChapterNumber); next != nil {
			chapterMap[retired[i].ID] = next.ID
		}
	}
	if hasProgress {
		if mapped := remapProgress(progress, retired, aggregate.Chapters); mapped != nil {
			if _, err := s.repo.UpsertReadingProgress(*mapped); err != nil {
				log.Printf("migration: remapping progress for %s failed: %v", oldID, err)
			}
		}
	}
	writeJSON(w, http.StatusOK, migrationResponse{Manga: aggregate, Source: body.SourceID, ChapterMap: chapterMap})
}

// findSameNumber locates a chapter with the given number in the list.
func findSameNumber(chapters []db.Chapter, number *float64) *db.Chapter {
	if number == nil {
		return nil
	}
	for i := range chapters {
		if chapters[i].ChapterNumber != nil && *chapters[i].ChapterNumber == *number {
			return &chapters[i]
		}
	}
	return nil
}

// remapProgress moves reading state onto the replacement chapter with the
// same number. It reports nil when the state cannot carry over, which keeps
// the progress store free of dangling references.
func remapProgress(progress db.ReadingProgress, retired, replacement []db.Chapter) *db.ReadingProgress {
	var old *db.Chapter
	for i := range retired {
		if retired[i].ID == progress.LastReadChapterID {
			old = &retired[i]
			break
		}
	}
	if old == nil {
		return nil
	}
	next := findSameNumber(replacement, old.ChapterNumber)
	if next == nil {
		return nil
	}
	mapped := progress
	mapped.LastReadChapterID = next.ID
	return &mapped
}
