package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

// The details envelope carries the series page recorded for a title, and falls
// back to the plugin base URL when the page is unknown. The demo plugin binary
// is absent, so the search lookup cannot improve on the fallback here.
func TestGetMangaServesSeriesPage(t *testing.T) {
	hits := 0
	repo, router, upstream, sourceID := coverTestRouter(t, &hits)
	series := "https://example.test/comics/demo"
	known, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", SourcePageURL: series, Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaDetailsFetched(known.ID, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if got := readSourcePage(t, router, known.ID); got != series {
		t.Fatalf("series page = %q, want %q", got, series)
	}

	unknown, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "other-manga", Title: "Other", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaDetailsFetched(unknown.ID, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if got := readSourcePage(t, router, unknown.ID); got != upstream {
		t.Fatalf("fallback series page = %q, want the plugin base URL %q", got, upstream)
	}
}

func readSourcePage(t *testing.T, router http.Handler, mangaID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+mangaID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		SourceURL string `json:"sourceUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode aggregate: %v", err)
	}
	return payload.SourceURL
}
