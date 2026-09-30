package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/settings"
)

// languageFixture builds a server over one source holding an English chapter, a
// Japanese chapter, and one chapter whose language is unknown. The returned
// download recorder captures what any enqueue endpoint selected.
func languageFixture(t *testing.T) (*fakeDownloads, chi.Router, string) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "source-languages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })
	repo := db.NewRepository(handle)
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'multi','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapters := []struct {
		id       string
		number   float64
		language *string
	}{
		{id: "en-1", number: 1, language: stringPointer("en")},
		{id: "ja-2", number: 2, language: stringPointer("ja")},
		{id: "unknown-3", number: 3},
	}
	for _, chapter := range chapters {
		if _, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: "s", SourceChapterID: chapter.id, ChapterNumber: &chapter.number, Language: chapter.language}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SetMangaDetailsFetched(manga.ID, 1); err != nil {
		t.Fatal(err)
	}
	server := NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()}), newFakeDownloads())
	server.SetSettings(settings.New(repo))
	router := chi.NewRouter()
	server.Mount(router)
	return server.downloads.(*fakeDownloads), router, manga.ID
}

// chapterLanguagesFrom reads a manga through the API and reports the returned
// chapter languages plus the selection the aggregate reports.
func chapterLanguagesFrom(t *testing.T, router chi.Router, mangaID string) ([]string, []string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/manga/"+mangaID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("manga status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Chapters []struct {
			Language *string `json:"language"`
		} `json:"chapters"`
		LanguageFilter []string `json:"languageFilter"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	codes := make([]string, 0, len(payload.Chapters))
	for _, chapter := range payload.Chapters {
		code := ""
		if chapter.Language != nil {
			code = *chapter.Language
		}
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes, payload.LanguageFilter
}

func TestSourceLanguageSelectionFiltersChapters(t *testing.T) {
	_, router, mangaID := languageFixture(t)

	codes, filter := chapterLanguagesFrom(t, router, mangaID)
	if !reflect.DeepEqual(codes, []string{"", "en", "ja"}) || filter != nil {
		t.Fatalf("unfiltered codes=%v filter=%v", codes, filter)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/sources/s/languages", strings.NewReader(`{"languages":["EN"," en "]}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var source struct {
		Languages []string `json:"languages"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.Languages, []string{"en"}) {
		t.Fatalf("stored languages = %v", source.Languages)
	}

	codes, filter = chapterLanguagesFrom(t, router, mangaID)
	if !reflect.DeepEqual(codes, []string{"", "en"}) || !reflect.DeepEqual(filter, []string{"en"}) {
		t.Fatalf("filtered codes=%v filter=%v", codes, filter)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/sources/s/languages", strings.NewReader(`{"languages":[]}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	codes, filter = chapterLanguagesFrom(t, router, mangaID)
	if !reflect.DeepEqual(codes, []string{"", "en", "ja"}) || filter != nil {
		t.Fatalf("cleared codes=%v filter=%v", codes, filter)
	}
}

func TestGlobalLanguageDefaultIsOverriddenPerSource(t *testing.T) {
	_, router, mangaID := languageFixture(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/settings/browse.chapter_languages", strings.NewReader(`{"value":"ja"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("global status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	codes, filter := chapterLanguagesFrom(t, router, mangaID)
	if !reflect.DeepEqual(codes, []string{"", "ja"}) || !reflect.DeepEqual(filter, []string{"ja"}) {
		t.Fatalf("global codes=%v filter=%v", codes, filter)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/sources/s/languages", strings.NewReader(`{"languages":["en"]}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("source status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	codes, filter = chapterLanguagesFrom(t, router, mangaID)
	if !reflect.DeepEqual(codes, []string{"", "en"}) || !reflect.DeepEqual(filter, []string{"en"}) {
		t.Fatalf("override codes=%v filter=%v", codes, filter)
	}
}

func TestSourceListReportsAvailableLanguages(t *testing.T) {
	_, router, _ := languageFixture(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/sources", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("sources status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var sources []struct {
		ID                 string   `json:"id"`
		AvailableLanguages []string `json:"availableLanguages"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].ID != "s" {
		t.Fatalf("sources = %+v", sources)
	}
	if !reflect.DeepEqual(sources[0].AvailableLanguages, []string{"en", "ja"}) {
		t.Fatalf("availableLanguages = %v", sources[0].AvailableLanguages)
	}
}

func TestGlobalLanguageDefaultRejectsInvalidCodes(t *testing.T) {
	_, router, _ := languageFixture(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/settings/browse.chapter_languages", strings.NewReader(`{"value":"en,not a code"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// A bulk download of every or every-unread chapter enqueues only what the
// language selection shows: the Japanese chapter stays out, the chapter with
// no language stays in.
func TestBulkDownloadHonorsLanguageSelection(t *testing.T) {
	downloads, router, mangaID := languageFixture(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/sources/s/languages", strings.NewReader(`{"languages":["en"]}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/library/bulk/download", strings.NewReader(`{"ids":["`+mangaID+`"],"chapters":"all"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("bulk download status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if downloads.mangaID != mangaID || len(downloads.selection.IDs) != 2 {
		t.Fatalf("download enqueue = %q %+v", downloads.mangaID, downloads.selection)
	}
}
