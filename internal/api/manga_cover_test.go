package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/identity"
)

const coverBody = "\x89PNG\r\n\x1a\nfake-image-payload"

// coverTestRouter wires a mounted API server against a fake upstream that
// serves a fixed PNG body and counts every fetch. The returned URL is the
// upstream base address for use as a stored cover locator.
func coverTestRouter(t *testing.T, hits *int) (*db.Repository, chi.Router, string, string) {
	t.Helper()
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(coverBody))
	}))
	t.Cleanup(sourceServer.Close)

	repo, router, sourceID := coverRouterForUpstream(t, sourceServer.URL)
	return repo, router, sourceServer.URL, sourceID
}

// coverRouterForUpstream mounts the API server against an already running
// upstream image server and returns the repository, router and source id.
func coverRouterForUpstream(t *testing.T, upstreamURL string) (*db.Repository, chi.Router, string) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	sourceID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?)`, sourceID, "demo", "Demo", 1, 1, "en", upstreamURL, "missing.wasm", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	router := chi.NewRouter()
	NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()})).Mount(router)
	return repo, router, sourceID
}

func TestMangaCoverFetchesOnceThenServesCache(t *testing.T) {
	hits := 0
	repo, router, upstream, _ := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "demo", SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing", CoverURL: upstream + "/cover.png"})
	if err != nil {
		t.Fatal(err)
	}

	path := "/api/manga/" + manga.ID + "/cover"
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != coverBody {
			t.Fatalf("request %d: status=%d body=%q", i+1, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Fatalf("request %d: content type = %q", i+1, got)
		}
	}
	if hits != 1 {
		t.Fatalf("upstream requests = %d, want one cached fetch", hits)
	}
	stored, err := repo.GetManga(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CoverCachePath == nil || stored.CoverContentType == nil || *stored.CoverContentType != "image/png" {
		t.Fatalf("cover cache record = %+v", stored)
	}
}

func TestMangaCoverRejectsMissingSourceData(t *testing.T) {
	hits := 0
	repo, router, _, _ := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "demo", SourceMangaID: "no-cover", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID+"/cover", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want an error for a title without a cover", rec.Code)
	}
}

// addMangaByID must answer with the aggregate envelope the web client uses to
// navigate to the details view.
func TestAddMangaByIDReturnsAggregate(t *testing.T) {
	hits := 0
	repo, router, _, _ := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "demo", SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/library", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manga db.Manga `json:"manga"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	if payload.Manga.ID != manga.ID || !payload.Manga.InLibrary {
		t.Fatalf("aggregate manga = %+v", payload.Manga)
	}
}

// A client that navigates away cancels its request. The cover transfer is
// owned by the daemon, so it must still finish and cache its bytes instead of
// being abandoned and re-fetched on the next visit.
func TestMangaCoverCompletesAfterClientCancel(t *testing.T) {
	hits := 0
	repo, router, upstream, _ := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "demo", SourceMangaID: "cancelled", Title: "Demo", Status: "ongoing", CoverURL: upstream + "/cover.png"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID+"/cover", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if hits != 1 {
		t.Fatalf("upstream requests = %d, want one", hits)
	}
	stored, err := repo.GetManga(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CoverCachePath == nil || *stored.CoverCachePath == "" {
		t.Fatalf("cover was not cached: %+v", stored)
	}
}

// A grid of covers must not open an unbounded number of upstream transfers:
// each one holds a browser connection for its whole duration.
func TestMangaCoverBoundsConcurrentUpstreamFetches(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = w.Write([]byte(coverBody))
	}))
	t.Cleanup(upstream.Close)

	repo, router, _ := coverRouterForUpstream(t, upstream.URL)
	paths := make([]string, 0, imageFetchConcurrency*2)
	for index := 0; index < imageFetchConcurrency*2; index++ {
		manga, err := repo.UpsertManga(db.Manga{
			SourceID:      "demo",
			SourceMangaID: fmt.Sprintf("remote-%d", index),
			Title:         "Demo",
			Status:        "ongoing",
			CoverURL:      fmt.Sprintf("%s/cover-%d.png", upstream.URL, index),
		})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, "/api/manga/"+manga.ID+"/cover")
	}

	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Errorf("cover status = %d body = %s", recorder.Code, recorder.Body.String())
			}
		}(path)
	}
	wg.Wait()

	if peak > imageFetchConcurrency {
		t.Fatalf("peak concurrent upstream fetches = %d, want at most %d", peak, imageFetchConcurrency)
	}
}
