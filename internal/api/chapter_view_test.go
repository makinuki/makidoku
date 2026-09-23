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

// Chapter list presentation is per-title: an absent field keeps its stored
// value, an empty string resets it to the client defaults, and unknown values
// are rejected before they reach the store.
func TestMangaChapterViewEndpoint(t *testing.T) {
	server, repo := testServer(t)
	router := chi.NewRouter()
	server.Mount(router)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "chapter-view", Title: "Chapter View", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/"+manga.ID+"/chapter-view", strings.NewReader(`{"sort":"number-asc","filter":"unread","language":"en"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("set status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var updated db.Manga
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ChapterSort != "number-asc" || updated.ChapterFilter != "unread" || updated.ChapterLanguage != "en" {
		t.Fatalf("stored chapter view = %+v", updated)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/"+manga.ID+"/chapter-view", strings.NewReader(`{"filter":""}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	updated = db.Manga{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ChapterSort != "number-asc" || updated.ChapterLanguage != "en" {
		t.Fatalf("absent fields were not preserved: %+v", updated)
	}
	if updated.ChapterFilter != "" {
		t.Fatalf("empty string did not reset the field: %+v", updated)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/"+manga.ID+"/chapter-view", strings.NewReader(`{"sort":"random"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/missing/chapter-view", strings.NewReader(`{"sort":"number-asc"}`)))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown title status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// A non-string chapter view value is a client error, not a silent ignore.
func TestMergeChapterViewRejectsNonString(t *testing.T) {
	body := map[string]json.RawMessage{"sort": json.RawMessage(`5`)}
	if _, err := mergeChapterView(body, "sort", ""); err == nil {
		t.Fatal("numeric chapter view was accepted")
	}
}
