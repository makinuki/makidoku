package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/settings"
	"github.com/makinuki/makidoku/internal/tracker"
)

// While incognito is on a progress write records nothing: no reading progress,
// no history event, and no reading session. Explicit mark-read still works,
// and turning incognito off restores normal recording.
func TestIncognitoSuppressesProgressRecording(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "incognito.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','demo','Demo','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	server := NewTrackerServer(repo, nil, newFakeDownloads(), tracker.NewRegistry(repo))
	server.SetSettings(settings.New(repo))
	router := chi.NewRouter()
	server.Mount(router)

	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "incognito", Title: "Incognito", Status: "ongoing", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "incognito-c1"})
	if err != nil {
		t.Fatal(err)
	}

	post := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return recorder
	}
	progressBody := `{"mangaId":"` + manga.ID + `","lastReadChapterId":"` + chapter.ID + `","lastReadPage":1,"totalPages":10,"sessionSeconds":30}`

	if recorder := post("/api/incognito", `{"enabled":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("enable incognito status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, err := repo.GetSetting("privacy.incognito")
	if err != nil || stored.Value != "true" {
		t.Fatalf("persisted incognito setting = %+v err=%v", stored, err)
	}

	if recorder := post("/api/progress", progressBody); recorder.Code != http.StatusOK {
		t.Fatalf("incognito progress status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := repo.GetReadingProgress(manga.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reading progress recorded in incognito: err=%v", err)
	}
	events, err := repo.ListHistoryEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("history recorded in incognito: %+v", events)
	}
	stats, err := repo.ReadingStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ReadingSeconds != 0 {
		t.Fatalf("reading session recorded in incognito: %d", stats.ReadingSeconds)
	}

	// Manual mark-read is a deliberate action and stays available.
	if recorder := post("/api/chapters/"+chapter.ID+"/read", `{"read":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("manual mark read status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	state, err := repo.GetChapterRead(chapter.ID)
	if err != nil || !state.Read {
		t.Fatalf("manual mark read did not apply: %+v err=%v", state, err)
	}

	// Turning incognito off restores normal recording.
	if recorder := post("/api/incognito", `{"enabled":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("disable incognito status=%d", recorder.Code)
	}
	if recorder := post("/api/progress", progressBody); recorder.Code != http.StatusOK {
		t.Fatalf("normal progress status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := repo.GetReadingProgress(manga.ID); err != nil {
		t.Fatalf("progress not recorded after incognito off: %v", err)
	}
	stats, err = repo.ReadingStats()
	if err != nil || stats.ReadingSeconds != 30 {
		t.Fatalf("reading session after incognito off = %d err=%v", stats.ReadingSeconds, err)
	}
}
