package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
)

func TestMangaCustomInfoAndBookmark(t *testing.T) {
	server, repo := testServer(t)
	router := chi.NewRouter()
	server.Mount(router)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "meta-1", Title: "Original", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "meta-c1"})
	if err != nil {
		t.Fatal(err)
	}

	patch := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		return recorder
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	recorder := patch("/api/manga/"+manga.ID+"/custom", `{"title":"Custom","description":"Custom description","coverUrl":"https://example.test/c.png"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("custom patch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, err := repo.GetManga(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CustomTitle == nil || *stored.CustomTitle != "Custom" || stored.CustomCoverURL == nil {
		t.Fatalf("custom fields = %+v", stored)
	}
	if display := stored.MarshalJSON; display == nil {
		t.Fatal("display method missing")
	}

	// An empty string clears the override while the source title stays put.
	recorder = patch("/api/manga/"+manga.ID+"/custom", `{"title":""}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("custom clear status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, err = repo.GetManga(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CustomTitle != nil {
		t.Fatalf("custom title not cleared: %v", stored.CustomTitle)
	}
	if stored.Title != "Original" {
		t.Fatalf("source title changed: %q", stored.Title)
	}

	recorder = post("/api/chapters/"+chapter.ID+"/bookmark", `{"bookmark":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("bookmark status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var bookmark bool
	if err := repo.DB().Get(&bookmark, `SELECT bookmark FROM chapters WHERE id=?`, chapter.ID); err != nil {
		t.Fatal(err)
	}
	if !bookmark {
		t.Fatal("bookmark not stored")
	}
}

func TestSavedSearchesAndFeeds(t *testing.T) {
	server, repo := testServer(t)
	router := chi.NewRouter()
	server.Mount(router)
	if _, err := repo.DB().Exec(`INSERT INTO source_feeds(id,source_id,is_global,feed_order) VALUES('feed-1','s',1,0)`); err != nil {
		t.Fatal(err)
	}

	get := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}

	recorder := get("/api/feeds")
	if recorder.Code != http.StatusOK {
		t.Fatalf("feeds status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var feeds []db.Feed
	if err := json.Unmarshal(recorder.Body.Bytes(), &feeds); err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].ID != "feed-1" {
		t.Fatalf("feeds = %+v", feeds)
	}

	create := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/saved-searches", strings.NewReader(`{"sourceId":"s","name":"Favourites","query":"action","filters":"[]"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(create, request)
	if create.Code != http.StatusCreated {
		t.Fatalf("create search status=%d body=%s", create.Code, create.Body.String())
	}
	var search db.SavedSearch
	if err := json.Unmarshal(create.Body.Bytes(), &search); err != nil {
		t.Fatal(err)
	}

	recorder = get("/api/saved-searches?sourceId=s")
	var searches []db.SavedSearch
	if err := json.Unmarshal(recorder.Body.Bytes(), &searches); err != nil {
		t.Fatal(err)
	}
	if len(searches) != 1 || searches[0].Name != "Favourites" {
		t.Fatalf("searches = %+v", searches)
	}

	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/saved-searches/"+search.ID, nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete search status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	recorder = get("/api/saved-searches?sourceId=s")
	if err := json.Unmarshal(recorder.Body.Bytes(), &searches); err != nil {
		t.Fatal(err)
	}
	if len(searches) != 0 {
		t.Fatalf("searches not deleted: %+v", searches)
	}
}
