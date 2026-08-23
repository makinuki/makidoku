package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
func coverTestRouter(t *testing.T, hits *int) (*db.Repository, chi.Router, string) {
	t.Helper()
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(coverBody))
	}))
	t.Cleanup(sourceServer.Close)

	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	sourceID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?)`, sourceID, "demo", "Demo", 1, 1, "en", sourceServer.URL, "missing.wasm", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	router := chi.NewRouter()
	NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()})).Mount(router)
	return repo, router, sourceServer.URL
}

func TestMangaCoverFetchesOnceThenServesCache(t *testing.T) {
	hits := 0
	repo, router, upstream := coverTestRouter(t, &hits)
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
	repo, router, _ := coverTestRouter(t, &hits)
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
	repo, router, _ := coverTestRouter(t, &hits)
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
