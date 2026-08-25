package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/tracker"
)

func trackerAPIHandler(t *testing.T) http.Handler {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	repo := db.NewRepository(handle)
	t.Setenv("MAKIDOKU_SECRET", "api-secret")
	server := NewTrackerServer(repo, nil, nil, tracker.NewRegistry(repo))
	r := chi.NewRouter()
	server.Mount(r)
	return r
}

// rewriteTransport redirects every request to one target host so provider
// endpoints can be served by a local test server.
type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	rewritten := *request.URL
	rewritten.Scheme = rt.target.Scheme
	rewritten.Host = rt.target.Host
	cloned.URL = &rewritten
	return rt.base.RoundTrip(cloned)
}

// newIsolatedTrackerServerWithSecret builds a mounted API server whose
// credential store is constructed with exactly the given secret, mirroring
// how the daemon captures MAKIDOKU_SECRET at startup.
func newIsolatedTrackerServerWithSecret(t *testing.T, secret string) (*Server, *chi.Mux, *tracker.Registry) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "trackers.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	repo := db.NewRepository(handle)
	t.Setenv("MAKIDOKU_SECRET", secret)
	registry := tracker.NewRegistry(repo)
	server := NewTrackerServer(repo, nil, nil, registry)
	mux := chi.NewRouter()
	server.Mount(mux)
	return server, mux, registry
}

func newIsolatedTrackerServer(t *testing.T) (*Server, *chi.Mux, *tracker.Registry) {
	return newIsolatedTrackerServerWithSecret(t, "api-secret")
}

func getJSON(t *testing.T, handler http.Handler, target string, out any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status=%d body=%s", target, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestTrackerAuthRejectsNonLoopbackRedirect(t *testing.T) {
	h := trackerAPIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start?redirect=https%3A%2F%2Fevil.example%2Fcallback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "loopback") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerAuthAcceptsLoopbackRedirect(t *testing.T) {
	t.Setenv("ANILIST_CLIENT_ID", "client")
	h := trackerAPIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start?redirect=http%3A%2F%2F127.0.0.1%3A8080%2Fapi%2Ftrackers%2Fanilist%2Fauth%2Fcallback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerAuthDefaultsToRequestLoopbackPort(t *testing.T) {
	t.Setenv("ANILIST_CLIENT_ID", "client")
	h := trackerAPIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start", nil)
	req.Host = "127.0.0.1:9090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "127.0.0.1:9090") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerAuthRejectsNonLoopbackRequestHost(t *testing.T) {
	t.Setenv("ANILIST_CLIENT_ID", "client")
	h := trackerAPIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start", nil)
	req.Host = "example.test:9090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerStatusSkipsBindingsWithoutStatusCapability(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	now := time.Now().Unix()
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','S','1',1,'en','https://example.test','source.wasm',?)`, now); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "https://example.test/cover"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "kitsu", RemoteID: "1", RemoteTitle: "M"}); err != nil {
		t.Fatal(err)
	}
	server := NewTrackerServer(repo, nil, nil, tracker.NewRegistry(repo))
	router := chi.NewRouter()
	server.Mount(router)
	req := httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID+"/trackers/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	// A search-only binding contributes nothing instead of failing the list.
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerStatusIdentifiesProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"findMangaById":{"titles":{"preferred":"M"},"chapterCount":14,"myLibraryEntry":{"id":"e1","progress":8,"status":"CURRENT"}}}}`))
	}))
	defer server.Close()

	_, mux, _, mangaID, trackerType, _ := newBindingFixture(t, server.URL+"/api/graphql")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+mangaID+"/trackers/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var statuses []tracker.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].TrackerType != trackerType {
		t.Fatalf("statuses = %+v", statuses)
	}
}

type trackerListItem struct {
	Name        string `json:"name"`
	Credential  bool   `json:"credential"`
	AuthType    string `json:"authType"`
	Configured  bool   `json:"configured"`
	ConfigHint  string `json:"configHint"`
	ConnectedAs string `json:"connectedAs"`
}

func TestListTrackersExposesConnectionMetadata(t *testing.T) {
	_, mux, registry := newIsolatedTrackerServer(t)
	var items []trackerListItem
	getJSON(t, mux, "/api/trackers", &items)
	byName := map[string]trackerListItem{}
	for _, entry := range items {
		byName[entry.Name] = entry
	}
	for _, name := range []string{"kitsu", "mangaupdates"} {
		entry := byName[name]
		if entry.AuthType != "password" || !entry.Configured || entry.ConfigHint != "" {
			t.Fatalf("%s = %+v", name, entry)
		}
	}
	anilist := byName["anilist"]
	if anilist.AuthType != "oauth" || anilist.Configured || !strings.Contains(anilist.ConfigHint, "ANILIST_CLIENT_ID") {
		t.Fatalf("unconfigured anilist = %+v", anilist)
	}

	t.Setenv("ANILIST_CLIENT_ID", "client")
	items = nil
	getJSON(t, mux, "/api/trackers", &items)
	for _, entry := range items {
		if entry.Name == "anilist" && (!entry.Configured || entry.ConfigHint != "") {
			t.Fatalf("configured anilist = %+v", entry)
		}
	}

	if err := registry.Store.Save("kitsu", tracker.Credential{AccessToken: "tok", Metadata: map[string]string{"username": "kitsu-user"}}); err != nil {
		t.Fatal(err)
	}
	items = nil
	getJSON(t, mux, "/api/trackers", &items)
	for _, entry := range items {
		if entry.Name == "kitsu" && (!entry.Credential || entry.ConnectedAs != "kitsu-user") {
			t.Fatalf("connected kitsu = %+v", entry)
		}
	}
}

func TestLoginTrackerPasswordGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "password" || r.Form.Get("username") != "user@example.com" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
		case "/api/graphql":
			_, _ = w.Write([]byte(`{"data":{"currentAccount":{"profile":{"name":"kitsu-user"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, mux, registry := newIsolatedTrackerServer(t)
	item, ok := registry.Get("kitsu")
	if !ok {
		t.Fatal("kitsu provider missing")
	}
	kitsu := item.(*tracker.Kitsu)
	kitsu.Client.HTTP = server.Client()
	kitsu.TokenURL = server.URL + "/api/oauth/token"
	kitsu.GraphQLURL = server.URL + "/api/graphql"

	body := strings.NewReader(`{"username":"user@example.com","password":"pw"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trackers/kitsu/login", body))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var items []trackerListItem
	getJSON(t, mux, "/api/trackers", &items)
	for _, entry := range items {
		if entry.Name == "kitsu" && (!entry.Credential || entry.ConnectedAs != "kitsu-user") {
			t.Fatalf("kits after login = %+v", entry)
		}
	}
}

func TestLoginTrackerRejectsBadCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	_, mux, registry := newIsolatedTrackerServer(t)
	item, ok := registry.Get("kitsu")
	if !ok {
		t.Fatal("kitsu provider missing")
	}
	kitsu := item.(*tracker.Kitsu)
	kitsu.Client.HTTP = server.Client()
	kitsu.TokenURL = server.URL + "/api/oauth/token"

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trackers/kitsu/login", strings.NewReader(`{"username":"u","password":"bad"}`)))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "rejected these credentials") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginTrackerValidatesRequest(t *testing.T) {
	_, mux, _ := newIsolatedTrackerServer(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trackers/kitsu/login", strings.NewReader(`{"username":"","password":""}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trackers/anilist/login", strings.NewReader(`{"username":"u","password":"p"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oauth tracker status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trackers/nonsense/login", strings.NewReader(`{"username":"u","password":"p"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown tracker status=%d", rec.Code)
	}
}

func TestTrackerCallbackRendersHTMLAndStoresCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") != "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at","expires_in":31536000}`))
		case "/":
			// AniList serves GraphQL from the bare host path.
			_, _ = w.Write([]byte(`{"data":{"Viewer":{"name":"viewer-user","mediaListOptions":{"scoreFormat":"POINT_100"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("ANILIST_CLIENT_ID", "client")
	_, mux, registry := newIsolatedTrackerServer(t)
	target, _ := url.Parse(server.URL)
	registry.HTTP = &http.Client{Transport: rewriteTransport{target: target, base: server.Client().Transport}}

	var start struct {
		AuthorizationURL string `json:"authorizationUrl"`
		RedirectURI      string `json:"redirectUri"`
	}
	startReq := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start", nil)
	startReq.Host = "127.0.0.1:6254"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, startReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	authURL, _ := url.Parse(start.AuthorizationURL)
	state := authURL.Query().Get("state")

	callbackReq := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/callback?code=abc&state="+url.QueryEscape(state), nil)
	callbackReq.Host = "127.0.0.1:6254"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, callbackReq)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "Authorization complete") {
		t.Fatalf("status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	var items []trackerListItem
	getJSON(t, mux, "/api/trackers", &items)
	for _, entry := range items {
		if entry.Name == "anilist" && (!entry.Credential || entry.ConnectedAs != "viewer-user") {
			t.Fatalf("anilist after callback = %+v", entry)
		}
	}
	// The account's scoring configuration is captured for later conversions.
	if credential, err := registry.Store.Load("anilist"); err != nil || credential.Metadata["score_format"] != "POINT_100" {
		t.Fatalf("credential metadata = %+v err=%v", credential.Metadata, err)
	}
}

func TestTrackerEventsSocketBroadcastsCredentialChanges(t *testing.T) {
	_, mux, registry := newIsolatedTrackerServer(t)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/trackers/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	if err := registry.Store.Save("kitsu", tracker.Credential{AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/trackers/kitsu/credentials", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", rec.Code)
	}

	var event TrackerEvent
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "credentials" || event.Tracker != "kitsu" {
		t.Fatalf("event = %+v", event)
	}
}

// newBindingFixture builds a server whose database holds one manga with one
// chapter and a kitsu binding, and whose kitsu provider talks to the given
// GraphQL fake.
func newBindingFixture(t *testing.T, graphqlURL string) (*Server, *chi.Mux, *tracker.Registry, string, string, db.Chapter) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "binding.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	repo := db.NewRepository(handle)
	t.Setenv("MAKIDOKU_SECRET", "api-secret")
	registry := tracker.NewRegistry(repo)
	item, ok := registry.Get("kitsu")
	if !ok {
		t.Fatal("kitsu provider missing")
	}
	kitsu := item.(*tracker.Kitsu)
	kitsu.GraphQLURL = graphqlURL
	kitsu.Client.HTTP = http.DefaultClient
	server := NewTrackerServer(repo, nil, nil, registry)
	mux := chi.NewRouter()
	server.Mount(mux)

	now := time.Now().Unix()
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','S','1',1,'en','https://example.test','source.wasm',?)`, now); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "https://example.test/cover"})
	if err != nil {
		t.Fatal(err)
	}
	chapterNumber := 8.0
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "c", ChapterNumber: &chapterNumber})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "kitsu", RemoteID: "1703", RemoteTitle: "M"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Store.Save("kitsu", tracker.Credential{AccessToken: "tok", Metadata: map[string]string{"rating_system": "advanced"}}); err != nil {
		t.Fatal(err)
	}
	return server, mux, registry, manga.ID, "kitsu", chapter
}

func TestUpdateTrackerBindingPersistsAndPushesScore(t *testing.T) {
	var mutations []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Query, "findMangaById") {
			_, _ = w.Write([]byte(`{"data":{"findMangaById":{"titles":{"preferred":"M"},"chapterCount":14,"myLibraryEntry":{"id":"e1","progress":8,"status":"CURRENT"}}}}`))
			return
		}
		mutations = append(mutations, payload.Variables)
		_, _ = w.Write([]byte(`{"data":{"libraryEntry":{"update":{"errors":[],"libraryEntry":{"id":"e1"}}}}}`))
	}))
	defer server.Close()

	_, mux, registry, mangaID, trackerType, _ := newBindingFixture(t, server.URL+"/api/graphql")

	body := strings.NewReader(`{"score":8,"startedAt":1700000000,"finishedAt":1700086400}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/manga/"+mangaID+"/trackers/"+trackerType, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var binding db.TrackerBinding
	if err := json.Unmarshal(rec.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding.RemoteScore == nil || *binding.RemoteScore != 8 || binding.StartedAt == nil || binding.FinishedAt == nil {
		t.Fatalf("binding = %+v", binding)
	}
	if len(mutations) != 1 {
		t.Fatalf("mutations = %d", len(mutations))
	}
	if mutations[0]["rating"] != float64(16) {
		t.Fatalf("pushed variables = %#v", mutations[0])
	}

	// The persisted values survive a refetch.
	stored, err := registry.Repo.GetTrackerBinding(mangaID, trackerType)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RemoteScore == nil || *stored.RemoteScore != 8 {
		t.Fatalf("stored binding = %+v", stored)
	}
}

func TestUpdateTrackerBindingRejectsInvalidInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer server.Close()

	_, mux, _, mangaID, trackerType, _ := newBindingFixture(t, server.URL+"/api/graphql")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/manga/"+mangaID+"/trackers/"+trackerType, strings.NewReader(`{"score":57}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("score range status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/manga/"+mangaID+"/trackers/nonsense", strings.NewReader(`{"score":8}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown tracker status=%d", rec.Code)
	}
}

func TestUpdateTrackerBindingPreservesOmittedDates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Query, "findMangaById") {
			_, _ = w.Write([]byte(`{"data":{"findMangaById":{"titles":{"preferred":"M"},"chapterCount":14,"myLibraryEntry":{"id":"e1","progress":8,"status":"CURRENT"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"libraryEntry":{"update":{"errors":[],"libraryEntry":{"id":"e1"}}}}}`))
	}))
	defer server.Close()

	_, mux, registry, mangaID, trackerType, _ := newBindingFixture(t, server.URL+"/api/graphql")
	startedAt, finishedAt := int64(1700000000), int64(1700086400)
	if _, err := registry.Repo.UpdateTrackerTracking(mangaID, trackerType, nil, &startedAt, &finishedAt); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/manga/"+mangaID+"/trackers/"+trackerType, strings.NewReader(`{"score":8}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var binding db.TrackerBinding
	if err := json.Unmarshal(rec.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding.StartedAt == nil || *binding.StartedAt != startedAt || binding.FinishedAt == nil || *binding.FinishedAt != finishedAt {
		t.Fatalf("dates were cleared: %+v", binding)
	}
}

func TestProgressCompletionPushesFinishDate(t *testing.T) {
	var mutations []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Query, "findMangaById") {
			_, _ = w.Write([]byte(`{"data":{"findMangaById":{"titles":{"preferred":"M"},"chapterCount":14,"myLibraryEntry":{"id":"e1","progress":7,"status":"CURRENT"}}}}`))
			return
		}
		mutations = append(mutations, payload.Variables)
		_, _ = w.Write([]byte(`{"data":{"libraryEntry":{"update":{"errors":[],"libraryEntry":{"id":"e1"}}}}}`))
	}))
	defer server.Close()

	_, mux, _, mangaID, _, chapter := newBindingFixture(t, server.URL+"/api/graphql")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(fmt.Sprintf(
		`{"mangaId":%q,"lastReadChapterId":%q,"lastReadPage":20,"totalPages":20,"isCompleted":true}`, mangaID, chapter.ID))))
	if rec.Code != http.StatusOK {
		t.Fatalf("progress status=%d body=%s", rec.Code, rec.Body.String())
	}

	// The finish push runs off the request; wait for the mutation to arrive.
	deadline := time.Now().Add(2 * time.Second)
	for len(mutations) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(mutations) != 1 {
		t.Fatalf("mutations = %d", len(mutations))
	}
	if mutations[0]["progress"] != float64(8) || mutations[0]["finishedAt"] == nil {
		t.Fatalf("finish variables = %#v", mutations[0])
	}
}

func TestCredentialFlowsRefuseMissingEncryptionSecret(t *testing.T) {
	_, mux, _ := newIsolatedTrackerServerWithSecret(t, "")

	startReq := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start", nil)
	startReq.Host = "127.0.0.1:6254"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, startReq)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MAKIDOKU_SECRET") {
		t.Fatalf("auth/start status=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, target := range []string{"/api/trackers/kitsu/login", "/api/trackers/anilist/token"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{"accessToken":"t","username":"u","password":"p"}`)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MAKIDOKU_SECRET") {
			t.Fatalf("%s status=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestListTrackersReportsMissingEncryptionSecretOnEveryCard(t *testing.T) {
	_, mux, _ := newIsolatedTrackerServerWithSecret(t, "")
	var items []trackerListItem
	getJSON(t, mux, "/api/trackers", &items)
	if len(items) == 0 {
		t.Fatal("no trackers listed")
	}
	for _, entry := range items {
		if entry.Configured || entry.Credential || !strings.Contains(entry.ConfigHint, "MAKIDOKU_SECRET") {
			t.Fatalf("%s = %+v", entry.Name, entry)
		}
	}
}
