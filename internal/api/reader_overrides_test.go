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

// Reader overrides are per-title: an absent field keeps its stored value, an
// explicit null clears it back to the global fallback, and unknown values are
// rejected before they reach the store.
func TestMangaReaderOverrideEndpoint(t *testing.T) {
	server, repo := testServer(t)
	router := chi.NewRouter()
	server.Mount(router)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "reader", Title: "Reader", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/"+manga.ID+"/reader", strings.NewReader(`{"mode":"double","direction":"rtl"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("set status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var updated db.Manga
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ReaderMode == nil || *updated.ReaderMode != "double" || updated.ReaderDirection == nil || *updated.ReaderDirection != "rtl" || updated.ReaderFit != nil {
		t.Fatalf("stored overrides = %+v", updated)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/"+manga.ID+"/reader", strings.NewReader(`{"direction":null,"fit":"height"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	updated = db.Manga{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ReaderMode == nil || *updated.ReaderMode != "double" {
		t.Fatalf("absent field was not preserved: %+v", updated)
	}
	if updated.ReaderDirection != nil {
		t.Fatalf("explicit null did not clear the field: %+v", updated)
	}
	if updated.ReaderFit == nil || *updated.ReaderFit != "height" {
		t.Fatalf("fit override = %+v", updated)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/manga/"+manga.ID+"/reader", strings.NewReader(`{"mode":"scroll"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// A non-string override value is a client error, not a silent ignore.
func TestMergeReaderOverrideRejectsNonString(t *testing.T) {
	body := map[string]json.RawMessage{"mode": json.RawMessage(`5`)}
	if _, err := mergeReaderOverride(body, "mode", nil); err == nil {
		t.Fatal("numeric override was accepted")
	}
}
