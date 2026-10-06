package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/settings"
)

func TestSettingsAPIValidatesAndPersistsValues(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "settings-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	server := NewServer(repo, nil)
	server.SetSettings(settings.New(repo))
	router := chi.NewRouter()
	server.Mount(router)

	request := httptest.NewRequest(http.MethodPut, "/api/settings/reader.default_mode", strings.NewReader(`{"value":"double"}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var entry struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&entry); err != nil || entry.Value != "double" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/settings/reader.default_mode", strings.NewReader(`{"value":"bad"}`))
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// A user agent written through the interface reaches the fetch layer as the
// identity itself rather than as the stored JSON literal, so a live change
// presents the same value the next start reads.
func TestUserAgentSettingNotifiesWithABareIdentity(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "user-agent-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	server := NewServer(repo, nil)
	server.SetSettings(settings.New(repo))
	var seen []string
	server.SetUserAgentObserver(func(agent string) { seen = append(seen, agent) })
	router := chi.NewRouter()
	server.Mount(router)

	const identity = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36"
	payload, err := json.Marshal(map[string]string{"value": identity})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/settings/anti_bot.user_agent", strings.NewReader(string(payload)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(seen) != 1 || seen[0] != identity {
		t.Fatalf("observer saw %q, want the bare identity", seen)
	}
}

func TestChapterReadAndHistoryEndpoints(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "read-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(repo, nil)
	router := chi.NewRouter()
	server.Mount(router)
	req := httptest.NewRequest(http.MethodPost, "/api/chapters/"+chapter.ID+"/read", strings.NewReader(`{"read":true}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", rec.Code, rec.Body.String())
	}
	state, err := repo.GetChapterRead(chapter.ID)
	if err != nil || !state.Read {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if _, err := repo.UpsertReadingProgress(db.ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 2}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/history", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "occurredAt") {
		t.Fatalf("history status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStatsEndpointReturnsReadingTime(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "stats-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(db.ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 2, SessionSeconds: 23}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(repo, nil)
	router := chi.NewRouter()
	server.Mount(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"readingSeconds":23`) || !strings.Contains(body, `"titleCount":0`) || !strings.Contains(body, `"chapterCount":0`) || !strings.Contains(body, `"daily":[`) {
		t.Fatalf("stats status=%d body=%s", rec.Code, rec.Body.String())
	}
}
