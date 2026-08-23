package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	t.Setenv("MAKIDOKU_ANILIST_CLIENT_ID", "client")
	h := trackerAPIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start?redirect=http%3A%2F%2F127.0.0.1%3A8080%2Fapi%2Ftrackers%2Fanilist%2Fauth%2Fcallback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerAuthDefaultsToRequestLoopbackPort(t *testing.T) {
	t.Setenv("MAKIDOKU_ANILIST_CLIENT_ID", "client")
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
	t.Setenv("MAKIDOKU_ANILIST_CLIENT_ID", "client")
	h := trackerAPIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trackers/anilist/auth/start", nil)
	req.Host = "example.test:9090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrackerStatusRejectsBindingWithoutStatusCapability(t *testing.T) {
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
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestManualScrobbleSkipsUnsupportedBindings(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "mixed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','S','1',1,'en','https://example.test','source.wasm',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "kitsu", RemoteID: "1", RemoteTitle: "M"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "2", RemoteTitle: "M"}); err != nil {
		t.Fatal(err)
	}
	server := NewTrackerServer(repo, nil, nil, tracker.NewRegistry(repo))
	router := chi.NewRouter()
	server.Mount(router)
	req := httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/trackers/scrobble", strings.NewReader(`{"chapterNumber":4}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	jobs, err := repo.ListTrackerSyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs=%+v", jobs)
	}
	binding, err := repo.GetTrackerBinding(manga.ID, "anilist")
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].BindingID != binding.ID {
		t.Fatalf("job binding=%d want=%d", jobs[0].BindingID, binding.ID)
	}
}
