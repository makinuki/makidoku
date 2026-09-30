package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/languages"
)

// chapterLanguages resolves the selection in effect for a source: an explicit
// per-source selection, else the global default, else no selection at all. An
// empty selection leaves every chapter visible.
func (s *Server) chapterLanguages(sourceID string) []string {
	if sourceID != "" && s.engine != nil {
		if source, err := s.engine.Get(sourceID); err == nil && len(source.Languages) > 0 {
			return source.Languages
		}
	}
	if s.settings != nil {
		if raw, err := s.settings.String("browse.chapter_languages"); err == nil {
			if codes := languages.ParseList(raw); len(codes) > 0 {
				return codes
			}
		}
	}
	return nil
}

// chapterLanguage reads the stored language of a chapter. A chapter without a
// language is unknown rather than non-matching.
func chapterLanguage(chapter db.Chapter) string {
	if chapter.Language == nil {
		return ""
	}
	return *chapter.Language
}

// filterChapters drops the chapters a selection excludes. Chapters with no
// language always pass, because their language is unknown.
func filterChapters(chapters []db.Chapter, selection []string) []db.Chapter {
	out := make([]db.Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		if languages.Allows(selection, chapterLanguage(chapter)) {
			out = append(out, chapter)
		}
	}
	return out
}

// applyLanguageFilter reduces a chapter list to the selection and reports the
// selection on the aggregate so the client can explain the reduced list.
func applyLanguageFilter(aggregate db.MangaAggregate, selection []string) db.MangaAggregate {
	if len(selection) == 0 {
		return aggregate
	}
	aggregate.Chapters = filterChapters(aggregate.Chapters, selection)
	aggregate.LanguageFilter = selection
	return aggregate
}

// annotateAvailableLanguages fills AvailableLanguages on each source from the
// chapter language codes stored for it. The distinct set is computed in one
// query so listing sources stays a single round trip.
func (s *Server) annotateAvailableLanguages(sources []engine.InstalledSource) {
	if len(sources) == 0 {
		return
	}
	observed, err := s.repo.SourceChapterLanguages()
	if err != nil {
		slog.Warn("reading source chapter languages failed", "err", err)
		return
	}
	for index := range sources {
		sources[index].AvailableLanguages = observed[sources[index].ID]
	}
}

// filterChapterIDs keeps the listed chapters that the selection allows.
// Identifiers with no stored chapter are dropped, because an automatic
// download cannot act on a chapter the store does not hold.
func (s *Server) filterChapterIDs(mangaID string, ids []string, selection []string) ([]string, error) {
	if len(selection) == 0 {
		return ids, nil
	}
	chapters, err := s.repo.ListChapters(mangaID)
	if err != nil {
		return nil, err
	}
	stored := make(map[string]db.Chapter, len(chapters))
	for _, chapter := range chapters {
		stored[chapter.ID] = chapter
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		chapter, ok := stored[id]
		if !ok {
			continue
		}
		if languages.Allows(selection, chapterLanguage(chapter)) {
			out = append(out, id)
		}
	}
	return out, nil
}

// putSourceLanguages stores the chapter language selection for one source. An
// empty list clears the selection, so the source falls back to the global
// default.
func (s *Server) putSourceLanguages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Languages []string `json:"languages"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	source, err := s.engine.SetSourceLanguages(chi.URLParam(r, "sourceID"), body.Languages)
	if err != nil {
		writeError(w, err)
		return
	}
	annotated := []engine.InstalledSource{source}
	s.annotateAvailableLanguages(annotated)
	writeJSON(w, http.StatusOK, annotated[0])
}
