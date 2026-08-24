package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

// Aggregate responses carry the installed plugin display name so clients never
// render raw identifiers. An unknown source id resolves to an empty name.
func TestAggregateExposesSourceName(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
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
		SourceName string `json:"sourceName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	if payload.SourceName != "Demo" {
		t.Fatalf("aggregate sourceName = %q, want Demo", payload.SourceName)
	}

	// Uninstalling keeps the sources row (installed=0), so the title survives
	// while its plugin name resolves to empty.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/sources/"+sourceID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uninstall status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	// The field is omitempty: an uninstalled plugin simply leaves the key out.
	if strings.Contains(rec.Body.String(), `"sourceName"`) {
		t.Fatalf("uninstalled aggregate still exposes sourceName: %s", rec.Body.String())
	}
}

// The library payload must keep the metadata embedded on LibraryManga and add
// the plugin display name. The key assertions double as a regression guard:
// the promoted Manga.MarshalJSON used to drop categories and progress.
func TestLibraryExposesSourceNameAndMetadata(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	if _, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing", InLibrary: true}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, key := range []string{`"categories"`, `"unreadChapters"`, `"sourceName"`} {
		if !strings.Contains(body, key) {
			t.Fatalf("library payload missing %s: %s", key, body)
		}
	}
	var items []struct {
		db.LibraryManga
		SourceName string `json:"sourceName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode library: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("library items = %d, want 1", len(items))
	}
	if items[0].SourceName != "Demo" {
		t.Fatalf("library sourceName = %q, want Demo", items[0].SourceName)
	}
	if items[0].Categories == nil || len(items[0].Categories) != 0 {
		t.Fatalf("library categories = %+v, want an empty list", items[0].Categories)
	}
}
