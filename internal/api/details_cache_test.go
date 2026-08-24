package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

// seedCachedManga stores a title that already went through a full details
// fetch, including one chapter, so a cache-only read has content to return.
func seedCachedManga(t *testing.T, repo interface {
	UpsertManga(db.Manga) (db.Manga, error)
	UpsertChapter(db.Chapter) (db.Chapter, error)
	SetMangaDetailsFetched(string, int64) error
}, sourceID string) db.Manga {
	t.Helper()
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	if _, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: sourceID, SourceChapterID: "chapter-1", ChapterNumber: &number}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaDetailsFetched(manga.ID, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return manga
}

// The details read is local-first: a stamped title serves from the database
// even though the demo plugin binary is missing, which would fail any plugin
// round-trip.
func TestGetMangaServesCacheWithoutPlugin(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga := seedCachedManga(t, repo, sourceID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manga    db.Manga     `json:"manga"`
		Chapters []db.Chapter `json:"chapters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	if payload.Manga.Title != "Demo" || len(payload.Chapters) != 1 {
		t.Fatalf("cached aggregate = %+v (%d chapters)", payload.Manga, len(payload.Chapters))
	}
}

// Titles without a freshness stamp still trigger the plugin round-trip, so a
// missing binary surfaces as an error until the first fetch succeeds.
func TestGetMangaFetchesWhenNeverFetched(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID, nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a plugin failure for a never-fetched title", rec.Code)
	}
}

// The pages read reuses a persisted page list so previously opened chapters
// stay readable without connectivity. The demo plugin binary is missing, so a
// successful response proves the list came from the database.
func TestMaterializePagesServesPersistedList(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: sourceID, SourceChapterID: "chapter-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertPages(chapter.ID, sourceID, []db.Page{{PageIndex: 0, RemoteURL: "https://upstream.test/p1.png"}, {PageIndex: 1, RemoteURL: "https://upstream.test/p2.png"}}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/chapters/"+chapter.ID+"/pages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var pages []db.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &pages); err != nil {
		t.Fatalf("decode pages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want the persisted list", len(pages))
	}
}

// The explicit refresh endpoint always attempts the plugin round-trip. A
// failure must leave the cached payload readable and untouched.
func TestRefreshMangaKeepsCacheOnFailure(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga := seedCachedManga(t, repo, sourceID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/refresh", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a plugin failure", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cached read after failed refresh: status = %d", rec.Code)
	}
}
