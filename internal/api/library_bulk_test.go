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

func decodeBulk(t *testing.T, recorder *httptest.ResponseRecorder) bulkResult {
	t.Helper()
	var result bulkResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode bulk result: %v (body %s)", err, recorder.Body.String())
	}
	return result
}

// A batch action applies to every listed title, reports per-id failures without
// failing the request, and rejects an empty or oversized id list.
func TestBulkLibraryActions(t *testing.T) {
	server, repo := testServer(t)
	router := chi.NewRouter()
	server.Mount(router)
	first, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "bulk-1", Title: "Bulk One", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "bulk-2", Title: "Bulk Two", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: first.ID, SourceChapterID: "bulk-c1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChapter(db.Chapter{MangaID: first.ID, SourceChapterID: "bulk-c2"}); err != nil {
		t.Fatal(err)
	}
	category, err := repo.CreateCategory("Read", 0)
	if err != nil {
		t.Fatal(err)
	}

	post := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return recorder
	}

	recorder := post("/api/library/bulk/library", `{"ids":["`+first.ID+`","`+second.ID+`"],"inLibrary":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("bulk library status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if result := decodeBulk(t, recorder); result.Updated != 2 || len(result.Failed) != 0 {
		t.Fatalf("bulk library result = %+v", result)
	}

	recorder = post("/api/library/bulk/read", `{"ids":["`+first.ID+`"],"read":true}`)
	if result := decodeBulk(t, recorder); result.Updated != 1 {
		t.Fatalf("bulk read result = %+v (status %d)", result, recorder.Code)
	}
	state, err := repo.GetChapterRead(chapter.ID)
	if err != nil || !state.Read {
		t.Fatalf("chapter read state = %+v err=%v", state, err)
	}

	recorder = post("/api/library/bulk/category", `{"ids":["`+first.ID+`","`+second.ID+`"],"categoryId":`+itoa(category.ID)+`,"enabled":true}`)
	if result := decodeBulk(t, recorder); result.Updated != 2 {
		t.Fatalf("bulk category result = %+v (status %d)", result, recorder.Code)
	}

	downloads := server.downloads.(*fakeDownloads)
	recorder = post("/api/library/bulk/download", `{"ids":["`+first.ID+`"],"chapters":"all"}`)
	if result := decodeBulk(t, recorder); result.Updated != 1 {
		t.Fatalf("bulk download result = %+v (status %d)", result, recorder.Code)
	}
	if downloads.mangaID != first.ID || len(downloads.selection.IDs) != 2 {
		t.Fatalf("download enqueue = %q %+v", downloads.mangaID, downloads.selection)
	}

	// Unknown ids surface in the failure list while the rest still apply.
	recorder = post("/api/library/bulk/read", `{"ids":["`+first.ID+`","missing"],"read":false}`)
	result := decodeBulk(t, recorder)
	if result.Updated != 1 || len(result.Failed) != 1 || result.Failed[0].ID != "missing" {
		t.Fatalf("partial failure result = %+v", result)
	}

	// An empty list is a client error.
	if recorder := post("/api/library/bulk/read", `{"ids":[],"read":true}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty list status = %d", recorder.Code)
	}

	// Removal uses the collection route.
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/library/bulk", strings.NewReader(`{"ids":["`+first.ID+`","`+second.ID+`"]}`)))
	if result := decodeBulk(t, recorder); result.Updated != 2 {
		t.Fatalf("bulk remove result = %+v (status %d)", result, recorder.Code)
	}
	stored, err := repo.GetManga(second.ID)
	if err != nil || stored.InLibrary {
		t.Fatalf("removed title still in library: %+v err=%v", stored, err)
	}
}

func itoa(value int64) string {
	digits := ""
	if value == 0 {
		return "0"
	}
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
