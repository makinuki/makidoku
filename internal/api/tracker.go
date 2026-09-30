package api

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
	"github.com/makinuki/makidoku/internal/tracker"
)

func (s *Server) mountTrackers(r chi.Router) {
	r.Get("/trackers", s.listTrackers)
	r.Get("/trackers/events", s.trackerEventsHandler)
	r.Get("/trackers/{trackerType}/search", s.trackerSearch)
	r.Post("/trackers/{trackerType}/token", s.saveTrackerToken)
	r.Post("/trackers/{trackerType}/login", s.loginTracker)
	r.Delete("/trackers/{trackerType}/credentials", s.deleteTrackerCredentials)
	r.Get("/trackers/{trackerType}/auth/start", s.startTrackerAuth)
	r.Get("/trackers/{trackerType}/auth/callback", s.trackerAuthCallback)
	r.Route("/manga/{mangaID}/trackers", func(manga chi.Router) {
		manga.Get("/", s.listBindings)
		manga.Post("/{trackerType}/bind", s.bindTracker)
		manga.Patch("/{trackerType}", s.updateTrackerBinding)
		manga.Delete("/{trackerType}", s.deleteBinding)
		manga.Get("/status", s.trackerStatuses)
	})
	r.Get("/manga/{mangaID}/suggestions", s.suggestions)
	r.Get("/tracker-sync", s.listSyncJobs)
	r.Get("/progress/{mangaID}", s.getProgress)
	r.Post("/progress", s.updateProgress)
	r.Post("/progress/complete", s.completeProgress)
}

// trackerAuthType classifies how a tracker connects: directly with a username
// and password, through browser authorization, or by pasting a token.
func trackerAuthType(t tracker.Tracker) string {
	if _, ok := t.(tracker.PasswordLogin); ok {
		return "password"
	}
	if t.Capabilities().OAuth {
		return "oauth"
	}
	return "token"
}

// requireCredentialsReady refuses flows that create or read stored
// credentials when encryption is unavailable. Failing here, before any
// provider interaction, spares the user a browser round-trip that could not
// complete anyway.
func (s *Server) requireCredentialsReady(w http.ResponseWriter) bool {
	if err := s.trackers.CredentialsReady(); err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func (s *Server) listTrackers(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name         string               `json:"name"`
		Capabilities tracker.Capabilities `json:"capabilities"`
		Credential   bool                 `json:"credential"`
		AuthType     string               `json:"authType"`
		Configured   bool                 `json:"configured"`
		ConfigHint   string               `json:"configHint,omitempty"`
		ConnectedAs  string               `json:"connectedAs,omitempty"`
	}
	items := make([]item, 0)
	credentialsReady := s.trackers.CredentialsReady() == nil
	for _, t := range s.trackers.List() {
		name := t.Name()
		entry := item{
			Name:         name,
			Capabilities: t.Capabilities(),
			AuthType:     trackerAuthType(t),
			Configured:   true,
		}
		if !credentialsReady {
			// Without the encryption secret no credential can be stored or
			// read, so every tracker is effectively unusable.
			entry.Configured = false
			entry.ConfigHint = "Set MAKIDOKU_SECRET (required to encrypt tracker credentials)"
			items = append(items, entry)
			continue
		}
		credential, err := s.trackers.Store.Load(name)
		if err != nil {
			entry.Configured = s.trackers.OAuthConfigured(name)
			if !entry.Configured {
				entry.ConfigHint = s.trackers.OAuthConfigHint(name)
			}
		} else {
			entry.Credential = true
			entry.ConnectedAs = credential.Metadata["username"]
		}
		items = append(items, entry)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) trackerSearch(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.trackers.Get(chi.URLParam(r, "trackerType"))
	if !ok {
		writeBadRequest(w, "unknown tracker")
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
	if !s.requireCredentialsReady(w) {
		return
	}
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
	s.publishTrackerCredentials(typ)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteTrackerCredentials(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "trackerType")
	if err := s.trackers.Repo.DeleteTrackerCredential(typ); err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	s.publishTrackerCredentials(typ)
	w.WriteHeader(http.StatusNoContent)
}

// loginTracker exchanges a username and password pair for trackers that
// authenticate directly. Credentials travel only over the local loopback and
// are never logged.
func (s *Server) loginTracker(w http.ResponseWriter, r *http.Request) {
	if !s.requireCredentialsReady(w) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	typ := chi.URLParam(r, "trackerType")
	if _, ok := s.trackers.Get(typ); !ok {
		writeBadRequest(w, "unknown tracker")
		return
	}
	username := strings.TrimSpace(body.Username)
	if username == "" || body.Password == "" {
		writeBadRequest(w, "username and password are required")
		return
	}
	if err := s.trackers.Login(r.Context(), typ, username, body.Password); err != nil {
		var httpErr *tracker.HTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden || httpErr.Status == http.StatusBadRequest) {
			writeLocalError(w, http.StatusUnauthorized, errors.New("the tracker rejected these credentials"))
			return
		}
		if errors.Is(err, tracker.ErrUnsupported) {
			writeLocalError(w, http.StatusBadRequest, err)
			return
		}
		writeTrackerError(w, err)
		return
	}
	s.publishTrackerCredentials(typ)
	w.WriteHeader(http.StatusNoContent)
}

// publishTrackerCredentials notifies websocket subscribers that stored
// credentials for one tracker changed.
func (s *Server) publishTrackerCredentials(trackerType string) {
	s.trackerEvents.publish(TrackerEvent{Type: trackerEventCredentials, Tracker: trackerType})
}

func (s *Server) startTrackerAuth(w http.ResponseWriter, r *http.Request) {
	if !s.requireCredentialsReady(w) {
		return
	}
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

// trackerCallbackPage renders the page a provider redirects back to after the
// user authorizes. The tab has no API client attached, so it speaks HTML and
// tells the user they can close it while the websocket notifies the app.
func trackerCallbackPage(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	page := `<!doctype html>
<html>
<head><meta charset="utf-8"><title>MakiDoku</title></head>
<body style="margin:0;height:100vh;display:grid;place-items:center;background:#09090b;color:#e4e4e7;font-family:system-ui,sans-serif">
<div style="text-align:center;max-width:28rem;padding:1rem">
<h1 style="font-size:1.25rem">` + html.EscapeString(title) + `</h1>
<p style="color:#a1a1aa">` + html.EscapeString(detail) + `</p>
</div>
</body>
</html>`
	_, _ = w.Write([]byte(page))
}

func (s *Server) trackerAuthCallback(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "trackerType")
	if errValue := r.URL.Query().Get("error"); errValue != "" {
		trackerCallbackPage(w, http.StatusBadRequest, "Authorization failed", "The provider reported: "+errValue)
		return
	}
	if _, ok := s.trackers.Get(typ); !ok {
		trackerCallbackPage(w, http.StatusNotFound, "Unknown tracker", typ+" is not registered on this server.")
		return
	}
	if err := s.trackers.CompleteOAuth(r.Context(), typ, r.URL.Query().Get("code"), r.URL.Query().Get("state"), ""); err != nil {
		trackerCallbackPage(w, http.StatusBadRequest, "Authorization failed", err.Error())
		return
	}
	s.publishTrackerCredentials(typ)
	trackerCallbackPage(w, http.StatusOK, "Authorization complete", strings.ToUpper(typ[:1])+typ[1:]+" is now connected. You can close this tab.")
}

// trackerEventsHandler streams credential-change events so open clients
// refresh their tracker state the moment an authorization completes.
func (s *Server) trackerEventsHandler(w http.ResponseWriter, r *http.Request) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()

	ctx := connection.CloseRead(s.Lifetime())
	events, unsubscribe := s.trackerEvents.subscribe()
	defer unsubscribe()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				_ = connection.Close(websocket.StatusNormalClosure, "")
				return
			}
			if err := wsjson.Write(ctx, connection, event); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
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
	// Validate the title reference up front so a typo cannot surface as a raw
	// foreign key error.
	mangaID := chi.URLParam(r, "mangaID")
	if _, err := s.repo.GetManga(mangaID); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	b, err := s.trackers.Repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: mangaID, TrackerType: typ, RemoteID: strings.TrimSpace(body.RemoteID), RemoteTitle: body.RemoteTitle, RemoteScore: body.RemoteScore, RemoteStatus: body.RemoteStatus, TotalRemoteChapters: body.TotalRemoteChapters})
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

// updateTrackerBinding applies a user's score and date edits: the values are
// pushed to the provider in its expected scale first and only persisted when
// that succeeds, so local state never claims an update the provider rejected.
func (s *Server) updateTrackerBinding(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Score      *float64 `json:"score"`
		StartedAt  *int64   `json:"startedAt"`
		FinishedAt *int64   `json:"finishedAt"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if !s.requireCredentialsReady(w) {
		return
	}
	mangaID := chi.URLParam(r, "mangaID")
	trackerType := chi.URLParam(r, "trackerType")
	provider, ok := s.trackers.Get(trackerType)
	if !ok {
		writeBadRequest(w, "unknown tracker")
		return
	}
	if body.Score != nil && (*body.Score < 0 || *body.Score > 10) {
		writeBadRequest(w, "score must be between 0 and 10")
		return
	}
	cred, err := s.trackers.Credential(trackerType)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	binding, err := s.repo.GetTrackerBinding(mangaID, trackerType)
	if err != nil {
		writeLocalError(w, http.StatusNotFound, err)
		return
	}
	update := tracker.TrackingUpdate{Chapter: binding.LastSyncedChapter, Score: body.Score}
	if body.StartedAt != nil {
		started := time.Unix(*body.StartedAt, 0)
		update.StartedAt = &started
	}
	if body.FinishedAt != nil {
		finished := time.Unix(*body.FinishedAt, 0)
		update.FinishedAt = &finished
	}
	if err := provider.UpdateTracking(r.Context(), binding, update, cred); err != nil {
		writeTrackerError(w, err)
		return
	}
	binding, err = s.repo.UpdateTrackerTracking(mangaID, trackerType, body.Score, body.StartedAt, body.FinishedAt)
	if err != nil {
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, binding)
}

func (s *Server) deleteBinding(w http.ResponseWriter, r *http.Request) {
	if err := s.trackers.Repo.DeleteTrackerBinding(chi.URLParam(r, "mangaID"),
		chi.URLParam(r, "trackerType")); err != nil {
		writeLocalError(w, http.StatusNotFound, err)
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
		c, e := s.trackers.Credential(b.TrackerType)
		if e != nil {
			continue
		}
		status, e := p.FetchUserStatus(r.Context(), b, c)
		if e != nil {
			writeTrackerError(w, e)
			return
		}
		status.TrackerType = b.TrackerType
		out = append(out, status)
	}
	writeJSON(w, http.StatusOK, out)
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
	if body.SessionSeconds < 0 || body.SessionSeconds > 300 {
		writeBadRequest(w, "sessionSeconds must be between 0 and 300")
		return
	}
	if complete {
		body.IsCompleted = true
	}
	if s.incognito.Load() {
		// No-trace policy: report the stored position without recording
		// history, read state, sessions, or automatic tracker activity.
		if stored, err := s.trackers.Repo.GetReadingProgress(body.MangaID); err == nil {
			writeJSON(w, http.StatusOK, stored)
			return
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	p, err := s.trackers.Repo.UpsertReadingProgress(body)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}
	if s.syncer != nil {
		// Tracker enqueue failures must not fail the progress write, but they
		// stay visible in the logs instead of vanishing.
		if err := s.syncer.EnqueueForProgress(body.MangaID, body.LastReadChapterID, body.IsCompleted, body.LastReadPage, body.TotalPages); err != nil {
			slog.Warn("tracker enqueueing sync jobs failed", "err", err)
		}
	}
	if s.downloads != nil && s.settings != nil && body.LastReadPage*100 >= body.TotalPages*80 {
		go s.enqueueAhead(s.Lifetime(), body)
	}
	if body.IsCompleted {
		// Completion records the finish date on providers that support dates.
		// The push runs off the request so it cannot delay the response.
		go s.pushFinishDates(s.Lifetime(), body)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) suggestions(w http.ResponseWriter, r *http.Request) {
	binding, err := s.repo.GetTrackerBinding(chi.URLParam(r, "mangaID"), "anilist")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, []tracker.Recommendation{})
			return
		}
		writeLocalError(w, http.StatusInternalServerError, err)
		return
	}
	provider, ok := s.trackers.Get("anilist")
	if !ok {
		writeJSON(w, http.StatusOK, []tracker.Recommendation{})
		return
	}
	recommendations, ok := provider.(tracker.RecommendationsProvider)
	if !ok {
		writeJSON(w, http.StatusOK, []tracker.Recommendation{})
		return
	}
	credential, err := s.trackers.Credential("anilist")
	if err != nil {
		writeJSON(w, http.StatusOK, []tracker.Recommendation{})
		return
	}
	items, err := recommendations.Recommendations(r.Context(), binding, credential)
	if err != nil {
		writeTrackerError(w, err)
		return
	}
	if items == nil {
		items = []tracker.Recommendation{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) enqueueAhead(ctx context.Context, progress db.ReadingProgress) {
	count, err := s.settings.Int("downloads.download_ahead")
	if err != nil || count <= 0 {
		return
	}
	manga, err := s.repo.GetManga(progress.MangaID)
	if err != nil {
		return
	}
	chapterIDs, err := s.repo.NextChapterIDs(progress.MangaID, progress.LastReadChapterID, count, s.chapterLanguages(manga.SourceID))
	if err != nil || len(chapterIDs) == 0 {
		return
	}
	if _, err := s.downloads.EnqueueManga(ctx, progress.MangaID, downloader.ChapterSelection{IDs: chapterIDs}, ""); err != nil {
		slog.Warn("downloader enqueueing chapters ahead failed", "err", err)
	}
}

// pushFinishDates marks completion on every bound provider. Failures are
// logged only: the queued progress jobs remain the source of truth for
// chapter numbers, and the next manual edit can repair a missed date.
func (s *Server) pushFinishDates(ctx context.Context, body db.ReadingProgress) {
	chapter, err := s.repo.GetChapter(body.LastReadChapterID)
	if err != nil || chapter.ChapterNumber == nil {
		return
	}
	bindings, err := s.trackers.Repo.ListTrackerBindings(body.MangaID)
	if err != nil {
		return
	}
	finished := time.Now()
	for _, binding := range bindings {
		cred, err := s.trackers.Credential(binding.TrackerType)
		if err != nil {
			continue
		}
		provider, ok := s.trackers.Get(binding.TrackerType)
		if !ok {
			continue
		}
		update := tracker.TrackingUpdate{Chapter: *chapter.ChapterNumber, FinishedAt: &finished}
		if err := provider.UpdateTracking(ctx, binding, update, cred); err != nil {
			slog.Warn("tracker recording finish failed", "tracker", binding.TrackerType, "err", err)
		}
	}
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
