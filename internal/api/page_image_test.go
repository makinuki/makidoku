package api

import (
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

func TestPageImageHidesRemoteLocatorAndCachesProcessedBytes(t *testing.T) {
	requests := 0
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	defer sourceServer.Close()

	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	sourceID, _ := identity.New()
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?)`, sourceID, "demo", "Demo", 1, 1, "en", sourceServer.URL, "missing.wasm", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: sourceID, SourceChapterID: "remote-chapter"})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := repo.UpsertPages(chapter.ID, []db.Page{{PageIndex: 0,
		RemoteURL:   sourceServer.URL + "/remote.png",
		IsScrambled: false}})
	if err != nil {
		t.Fatal(err)
	}

	server := NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()}))
	router := chi.NewRouter()
	server.Mount(router)
	imagePath := "/api/pages/" + pages[0].ID + "/image"
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, imagePath, nil))
	if first.Code != http.StatusOK || first.Body.String() != "png-data" {
		t.Fatalf("first image response = %d %q", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, imagePath, nil))
	if second.Code != http.StatusOK || second.Body.String() != "png-data" {
		t.Fatalf("cached image response = %d %q", second.Code, second.Body.String())
	}
	if requests != 1 {
		t.Fatalf("source requests = %d, want one cached fetch", requests)
	}
}
