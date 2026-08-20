package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

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
		source.Get("/details", s.details)
		source.Get("/pages", s.pages)
		source.Post("/clearance", s.submitClearance)
	})
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
	writeJSON(w, http.StatusOK, result)
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
