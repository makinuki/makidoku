package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// migrationSearchTimeout bounds one plugin search. A source that never answers
// must not hold the picker open, since the searches run together and the
// dialog waits for the slowest of them.
const migrationSearchTimeout = 20 * time.Second

var errEngineUnavailable = errors.New("sources are unavailable right now; try again later")

type migrationCandidate struct {
	Source engine.InstalledSource `json:"source"`
	Result engine.MangaItem       `json:"result"`
}

// migrationCandidatesResponse separates the usable candidates from the
// plugins that could not be searched, so a partial failure is visible
// instead of looking like an empty catalog.
type migrationCandidatesResponse struct {
	Candidates    []migrationCandidate `json:"candidates"`
	FailedSources int                  `json:"failedSources"`
	Searched      int                  `json:"searched"`
}

type migrationResponse struct {
	Manga      db.MangaAggregate `json:"manga"`
	Source     string            `json:"source"`
	ChapterMap map[string]string `json:"chapterMap"`
}

func (s *Server) mountMigration(r chi.Router) {
	r.Get("/migration/sources", s.migrationSources)
	r.Get("/migration/sources/{sourceID}/manga", s.migrationSourceManga)
	r.Post("/migration/jobs", s.createMigrationJob)
	r.Get("/migration/jobs/{jobID}/events", s.migrationJobEvents)
	r.Post("/migration/jobs/{jobID}/cancel", s.cancelMigrationJob)
	r.Post("/migration/jobs/{jobID}/titles/{mangaID}/cancel", s.cancelMigrationTitle)
	r.Route("/manga/{mangaID}/migration", func(migration chi.Router) {
		migration.Get("/candidates", s.migrationCandidates)
		migration.Post("/apply", s.applyMigration)
	})
}

func (s *Server) migrationSources(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	sources, err := s.engine.Installed()
	if err != nil {
		writeError(w, err)
		return
	}
	counts, err := s.repo.LibrarySourceCounts()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	type entry struct {
		Source engine.InstalledSource `json:"source"`
		Count  int                    `json:"count"`
		// Imported marks a placeholder source created by a restore, which the
		// engine does not serve but whose titles can be moved onto a real one.
		Imported bool `json:"imported,omitempty"`
	}
	out := make([]entry, 0, len(sources))
	for _, source := range sources {
		if counts[source.ID] > 0 {
			out = append(out, entry{Source: source, Count: counts[source.ID]})
		}
	}
	imported, err := s.repo.ListImportedSources()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	for _, source := range imported {
		if counts[source.ID] == 0 {
			continue
		}
		out = append(out, entry{
			Source:   engine.InstalledSource{ID: source.ID, Name: source.Name, BaseURL: source.BaseURL},
			Count:    counts[source.ID],
			Imported: true,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) migrationSourceManga(w http.ResponseWriter, r *http.Request) {
	manga, err := s.repo.ListLibraryBySource(chi.URLParam(r, "sourceID"))
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, manga)
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
	// The searches run concurrently. A dialog that waits for every plugin in
	// turn takes as long as the sum of their latencies, and a single slow or
	// unreachable source is enough to make the picker feel broken.
	targets := make([]engine.InstalledSource, 0, len(sources))
	for _, source := range sources {
		if source.ID == manga.SourceID {
			continue
		}
		targets = append(targets, source)
	}
	searches := make([][]engine.MangaItem, len(targets))
	failures := make([]error, len(targets))
	var group sync.WaitGroup
	for i, source := range targets {
		group.Add(1)
		go func() {
			defer group.Done()
			ctx, cancel := context.WithTimeout(r.Context(), migrationSearchTimeout)
			defer cancel()
			page, searchErr := s.engine.Search(ctx, source.ID, engine.SearchQuery{Query: query, Page: 1})
			if searchErr != nil {
				failures[i] = searchErr
				return
			}
			searches[i] = page.Items
		}()
	}
	group.Wait()

	results := make([]migrationCandidate, 0)
	failed := 0
	for i, source := range targets {
		if failures[i] != nil {
			slog.Warn("migration candidate search failed", "source", source.Name, "err", failures[i])
			failed++
			continue
		}
		for _, result := range searches[i] {
			materialized, materializeErr := s.repo.UpsertMangaStub(db.Manga{SourceID: source.ID, SourceMangaID: result.ID, SourcePageURL: result.URL, Title: result.Title, CoverURL: engine.SelectCover(result.CoverURL, result.Covers, engine.PreferredCoverWidth), Status: "unknown"})
			if materializeErr != nil {
				continue
			}
			result.ID = materialized.ID
			results = append(results, migrationCandidate{Source: source, Result: result})
		}
	}
	writeJSON(w, http.StatusOK, migrationCandidatesResponse{
		Candidates:    results,
		FailedSources: failed,
		Searched:      len(results) + failed,
	})
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
			slog.Warn("migration removing artifact failed", "path", path, "err", err)
		}
	}

	// Materialize the replacement's chapters right away so the title opens
	// with a consistent list. A failed fetch keeps the migration; the title
	// stays empty until the next refresh.
	source, err := s.repo.GetMangaSource(oldID)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	aggregate, fetchErr := s.fetchAndStoreDetails(r, source)
	if fetchErr != nil {
		slog.Warn("migration replacement fetch failed", "manga", oldID, "err", fetchErr)
		aggregate, err = s.repo.GetMangaAggregate(oldID)
		if err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
	}
	// The response must reflect the replacement plugin, matching what a
	// fresh read of the title would return.
	aggregate.SourceName = s.sourceName(aggregate.Manga.SourceID)

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
				slog.Warn("migration remapping progress failed", "manga", oldID, "err", err)
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
