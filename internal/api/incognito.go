package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountPrivacy(r chi.Router) {
	r.Get("/incognito", s.getIncognito)
	r.Post("/incognito", s.setIncognito)
}

// getIncognito reports the runtime no-trace flag.
func (s *Server) getIncognito(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.incognito.Load()})
}

// setIncognito flips the runtime no-trace flag and persists it as the default
// for the next start when the settings service is available.
func (s *Server) setIncognito(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	s.incognito.Store(body.Enabled)
	if s.settings != nil {
		if err := s.settings.Set("privacy.incognito", strconv.FormatBool(body.Enabled)); err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": body.Enabled})
}
