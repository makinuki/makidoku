package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/tracker"
)

func (s *Server) mountTrackers(r chi.Router) {
	r.Get("/trackers", s.listTrackers)
	r.Get("/trackers/{trackerType}/search", s.trackerSearch)
	r.Post("/trackers/{trackerType}/token", s.saveTrackerToken)
	r.Delete("/trackers/{trackerType}/credentials", s.deleteTrackerCredentials)
	r.Get("/trackers/{trackerType}/auth/start", s.startTrackerAuth)
	r.Get("/trackers/{trackerType}/auth/callback", s.trackerAuthCallback)
	r.Route("/manga/{mangaID}/trackers", func(manga chi.Router) {
		manga.Get("/", s.listBindings)
		manga.Post("/{trackerType}/bind", s.bindTracker)
		manga.Delete("/{trackerType}", s.deleteBinding)
		manga.Get("/status", s.trackerStatuses)
		manga.Post("/scrobble", s.manualScrobble)
	})
	r.Get("/tracker-sync", s.listSyncJobs)
	r.Get("/progress/{mangaID}", s.getProgress)
	r.Post("/progress", s.updateProgress)
	r.Post("/progress/complete", s.completeProgress)
}

func (s *Server) listTrackers(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name         string               `json:"name"`
		Capabilities tracker.Capabilities `json:"capabilities"`
		Credential   bool                 `json:"credential"`
	}
	items := make([]item, 0)
	for _, t := range s.trackers.List() {
		_, err := s.trackers.Store.Load(t.Name())
		items = append(items, item{Name: t.Name(), Capabilities: t.Capabilities(), Credential: err == nil})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) trackerSearch(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.trackers.Get(chi.URLParam(r, "trackerType"))
	if !ok {
		writeBadRequest(w, "unknown tracker")
		return
	}
	if !provider.Capabilities().Search {
		writeLocalError(w, http.StatusNotImplemented, tracker.ErrUnsupported)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeBadRequest(w, "q is required")
		return
	}
	results, err := provider.Search(r.Context(), query)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	if results == nil {
		results = []tracker.SearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) saveTrackerToken(w http.ResponseWriter, r *http.Request) {
	type request struct {
		AccessToken  string            `json:"accessToken"`
		RefreshToken string            `json:"refreshToken"`
		ExpiresAt    *int64            `json:"expiresAt"`
		Metadata     map[string]string `json:"metadata"`
	}
	var body request
	if !decodeBody(w, r, &body) {
		return
	}
	typ := chi.URLParam(r, "trackerType")
	if _, ok := s.trackers.Get(typ); !ok {
		writeBadRequest(w, "unknown tracker")
		return
	}
	cred := tracker.Credential{AccessToken: strings.TrimSpace(body.AccessToken), RefreshToken: body.RefreshToken, Metadata: body.Metadata}
	if body.ExpiresAt != nil {
		v := time.Unix(*body.ExpiresAt, 0)
		cred.ExpiresAt = &v
	}
	if cred.AccessToken == "" {
		writeBadRequest(w, "accessToken is required")
		return
	}
	if err := s.trackers.Store.Save(typ, cred); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteTrackerCredentials(w http.ResponseWriter, r *http.Request) {
	if err := s.trackers.Repo.DeleteTrackerCredential(chi.URLParam(r, "trackerType")); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startTrackerAuth(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "trackerType")
	redirect := strings.TrimSpace(r.URL.Query().Get("redirect"))
	if redirect == "" {
		redirect = "http://" + r.Host + "/api/trackers/" + typ + "/auth/callback"
	}
	if !validLoopbackRedirect(redirect, typ) {
		writeBadRequest(w, "redirect must be a loopback callback for this tracker")
		return
	}
	url, err := s.trackers.StartOAuth(typ, redirect)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": url, "redirectUri": redirect})
}
func (s *Server) trackerAuthCallback(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "trackerType")
	if errValue := r.URL.Query().Get("error"); errValue != "" {
		writeLocalError(w, http.StatusBadRequest, errors.New(errValue))
		return
	}
	if err := s.trackers.CompleteOAuth(r.Context(), typ, r.URL.Query().Get("code"), r.URL.Query().Get("state"), ""); err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tracker": typ})
}

func (s *Server) listBindings(w http.ResponseWriter, r *http.Request) {
	items, err := s.trackers.Repo.ListTrackerBindings(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []db.TrackerBinding{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) bindTracker(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RemoteID            string   `json:"remoteId"`
		RemoteTitle         string   `json:"remoteTitle"`
		RemoteScore         *float64 `json:"remoteScore"`
		RemoteStatus        *string  `json:"remoteStatus"`
		TotalRemoteChapters *int     `json:"totalRemoteChapters"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	typ := chi.URLParam(r, "trackerType")
	if _, ok := s.trackers.Get(typ); !ok {
		writeBadRequest(w, "unknown tracker")
		return
	}
	if strings.TrimSpace(body.RemoteID) == "" || strings.TrimSpace(body.RemoteTitle) == "" {
		writeBadRequest(w, "remoteId and remoteTitle are required")
		return
	}
	b, err := s.trackers.Repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: chi.URLParam(r, "mangaID"), TrackerType: typ, RemoteID: strings.TrimSpace(body.RemoteID), RemoteTitle: body.RemoteTitle, RemoteScore: body.RemoteScore, RemoteStatus: body.RemoteStatus, TotalRemoteChapters: body.TotalRemoteChapters})
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}
func (s *Server) deleteBinding(w http.ResponseWriter, r *http.Request) {
	if err := s.trackers.Repo.DeleteTrackerBinding(chi.URLParam(r, "mangaID"), chi.URLParam(r, "trackerType")); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) trackerStatuses(w http.ResponseWriter, r *http.Request) {
	bindings, err := s.trackers.Repo.ListTrackerBindings(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]tracker.Status, 0, len(bindings))
	for _, b := range bindings {
		p, ok := s.trackers.Get(b.TrackerType)
		if !ok {
			writeBadRequest(w, "unknown tracker: "+b.TrackerType)
			return
		}
		if !p.Capabilities().Status {
			writeLocalError(w, http.StatusNotImplemented, tracker.ErrUnsupported)
			return
		}
		c, e := s.trackers.Credential(b.TrackerType)
		if e != nil {
			continue
		}
		status, e := p.FetchUserStatus(r.Context(), b, c)
		if e != nil {
			writeTrackerError(w, e)
			return
		}
		out = append(out, status)
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) manualScrobble(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChapterNumber float64 `json:"chapterNumber"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	bindings, err := s.trackers.Repo.ListTrackerBindings(chi.URLParam(r, "mangaID"))
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	for _, b := range bindings {
		provider, ok := s.trackers.Get(b.TrackerType)
		if !ok {
			writeBadRequest(w, "unknown tracker: "+b.TrackerType)
			return
		}
		if !provider.Capabilities().Scrobble {
			continue
		}
		if _, err := s.trackers.Repo.EnqueueTrackerSync(b.MangaID, b.ID, body.ChapterNumber); err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if s.syncer != nil {
		_ = s.syncer.RunOnce(r.Context())
	}
	w.WriteHeader(http.StatusAccepted)
}
func (s *Server) listSyncJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.trackers.Repo.ListTrackerSyncJobs()
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	if jobs == nil {
		jobs = []db.TrackerSyncJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) updateProgress(w http.ResponseWriter, r *http.Request)   { s.progress(w, r, false) }
func (s *Server) completeProgress(w http.ResponseWriter, r *http.Request) { s.progress(w, r, true) }
func (s *Server) getProgress(w http.ResponseWriter, r *http.Request) {
	p, err := s.trackers.Repo.GetReadingProgress(chi.URLParam(r, "mangaID"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
func (s *Server) progress(w http.ResponseWriter, r *http.Request, complete bool) {
	var body db.ReadingProgress
	if !decodeBody(w, r, &body) {
		return
	}
	if complete {
		body.IsCompleted = true
	}
	p, err := s.trackers.Repo.UpsertReadingProgress(body)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	if s.syncer != nil {
		_ = s.syncer.EnqueueForProgress(body.MangaID, body.LastReadChapterID, body.IsCompleted, body.LastReadPage, body.TotalPages)
	}
	writeJSON(w, http.StatusOK, p)
}

func validLoopbackRedirect(value, trackerType string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() == "" {
		return false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	return u.Path == "/api/trackers/"+trackerType+"/auth/callback"
}

func writeTrackerError(w http.ResponseWriter, err error) {
	var h *tracker.HTTPError
	if errors.As(err, &h) {
		status := http.StatusBadGateway
		if h.Status == http.StatusUnauthorized {
			status = http.StatusUnauthorized
		}
		if h.Status == http.StatusTooManyRequests {
			status = http.StatusTooManyRequests
		}
		writeLocalError(w, status, err)
		return
	}
	writeLocalError(w, http.StatusBadGateway, err)
}
