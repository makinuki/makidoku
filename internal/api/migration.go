package api

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// migrationSearchTimeout bounds one plugin search. A source that never answers
// must not hold a job open, since the searches run together and the job waits
// for the slowest of them.
const migrationSearchTimeout = 20 * time.Second

var errEngineUnavailable = errors.New("sources are unavailable right now; try again later")

type migrationResponse struct {
	Manga  db.MangaAggregate `json:"manga"`
	Source string            `json:"source"`
}

func (s *Server) mountMigration(r chi.Router) {
	r.Get("/migration/sources", s.migrationSources)
	r.Post("/migration/jobs", s.createMigrationJob)
	r.Get("/migration/jobs/{jobID}/events", s.migrationJobEvents)
	r.Post("/migration/jobs/{jobID}/cancel", s.cancelMigrationJob)
	r.Post("/migration/jobs/{jobID}/titles/{mangaID}/cancel", s.cancelMigrationTitle)
	r.Route("/manga/{mangaID}/migration", func(migration chi.Router) {
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

func (s *Server) applyMigration(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	var body struct {
		SourceID string `json:"sourceId"`
		MangaID  string `json:"mangaId"`
		// Flags overrides the stored migration preferences for this call. It
		// is a bitmask; see migrationFlag.
		Flags *int `json:"flags"`
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

	flags := s.migrationFlags()
	if body.Flags != nil {
		candidate := migrationFlag(*body.Flags)
		if !candidate.valid() {
			writeBadRequest(w, "flags contains an unsupported value")
			return
		}
		flags = candidate
	}
	// Read state and bookmarks live on the chapters a migration retires, so
	// capture them before the retirement deletes those rows.
	progress, progressErr := s.repo.GetReadingProgress(oldID)
	hasProgress := progressErr == nil
	var highestRead *float64
	var bookmarks []float64
	if flags.has(migrationFlagChapter) {
		highestRead, err = s.repo.HighestReadChapterNumber(oldID)
		if err != nil {
			slog.Warn("migration reading highest read chapter failed", "manga", oldID, "err", err)
			highestRead = nil
		}
		bookmarks, err = s.repo.ListBookmarkedChapterNumbers(oldID)
		if err != nil {
			slog.Warn("migration listing bookmarks failed", "manga", oldID, "err", err)
			bookmarks = nil
		}
	}

	// Retire the previous source's chapters and attach the replacement in one
	// transaction.
	_, artifacts, err := s.repo.MigrateMangaSource(oldID, discoveredSource.SourceID, body.MangaID)
	if err != nil {
		writeLocalError(w, http.StatusConflict, err)
		return
	}
	if flags.has(migrationFlagRemoveDownload) {
		for _, path := range artifacts {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				slog.Warn("migration removing artifact failed", "path", path, "err", err)
			}
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

	if flags.has(migrationFlagChapter) {
		s.carryReadState(oldID, aggregate.Chapters, highestRead, bookmarks, progress, hasProgress)
	}
	writeJSON(w, http.StatusOK, migrationResponse{Manga: aggregate, Source: body.SourceID})
}

// carryReadState moves read state onto a replacement's chapters. Read state
// carries as a boundary: every replacement chapter at or below the highest
// chapter number read becomes read, because chapter numbering drifts between
// sources. Bookmarks name specific chapters, so they carry by number. The
// reading progress pointer follows the highest carried chapter.
func (s *Server) carryReadState(mangaID string, chapters []db.Chapter, highestRead *float64, bookmarks []float64, previous db.ReadingProgress, hasProgress bool) {
	if highestRead != nil {
		if _, err := s.repo.MarkChaptersReadThrough(mangaID, *highestRead); err != nil {
			slog.Warn("migration carrying read state failed", "manga", mangaID, "err", err)
		}
	}
	for _, number := range bookmarks {
		if err := s.repo.MarkChapterBookmarkedByNumber(mangaID, number); err != nil {
			slog.Warn("migration carrying bookmark failed", "manga", mangaID, "err", err)
		}
	}
	if !hasProgress || highestRead == nil {
		return
	}
	boundary := highestAtOrBelow(chapters, *highestRead)
	if boundary == nil {
		return
	}
	mapped := previous
	mapped.MangaID = mangaID
	mapped.LastReadChapterID = boundary.ID
	if mapped.TotalPages < 1 {
		mapped.TotalPages = 1
	}
	if mapped.LastReadPage < 1 || mapped.LastReadPage > mapped.TotalPages {
		mapped.LastReadPage = 1
	}
	mapped.IsCompleted = previous.IsCompleted && boundaryIsLast(chapters, boundary)
	if _, err := s.repo.UpsertReadingProgress(mapped); err != nil {
		slog.Warn("migration carrying progress failed", "manga", mangaID, "err", err)
	}
}

// highestAtOrBelow returns the chapter with the greatest number at or below
// the given value, or nil when none qualifies.
func highestAtOrBelow(chapters []db.Chapter, number float64) *db.Chapter {
	var best *db.Chapter
	for i := range chapters {
		current := &chapters[i]
		if current.ChapterNumber == nil || *current.ChapterNumber > number {
			continue
		}
		if best == nil || *best.ChapterNumber < *current.ChapterNumber {
			best = current
		}
	}
	return best
}

// boundaryIsLast reports whether the chapter carries the greatest number in
// the list, which decides whether a carried series counts as finished.
func boundaryIsLast(chapters []db.Chapter, boundary *db.Chapter) bool {
	last := highestAtOrBelow(chapters, math.MaxFloat64)
	return last != nil && last.ID == boundary.ID
}
