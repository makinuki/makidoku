package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
)

type fakeDownloads struct {
	items             []db.DownloadQueueItem
	stats             downloader.Stats
	selection         downloader.ChapterSelection
	format            string
	mangaID           string
	pausedID          int64
	resumedID         int64
	canceledID        int64
	retriedID         int64
	retryFailed       int
	retryFailedSource string
	invalidated       string
	pausedSource      string
	resumedSource     string
	pausedSources     []string
	pausedAll         bool
	cancelAll         int64
	cancelIDs         []int64
	order             []int64
	events            chan downloader.Event
}

func newFakeDownloads() *fakeDownloads {
	return &fakeDownloads{events: make(chan downloader.Event, 1)}
}

func (f *fakeDownloads) List() ([]db.DownloadQueueItem, error) { return f.items, nil }
func (f *fakeDownloads) Stats() downloader.Stats               { return f.stats }
func (f *fakeDownloads) EnqueueManga(ctx context.Context, mangaID string, selection downloader.ChapterSelection, format string) ([]db.DownloadQueueItem, error) {
	f.mangaID, f.selection, f.format = mangaID, selection, format
	return f.items, nil
}
func (f *fakeDownloads) Pause(id int64) error  { f.pausedID = id; return nil }
func (f *fakeDownloads) Resume(id int64) error { f.resumedID = id; return nil }
func (f *fakeDownloads) Cancel(id int64) error {
	f.canceledID = id
	f.cancelIDs = append(f.cancelIDs, id)
	return nil
}
func (f *fakeDownloads) Retry(id int64) error { f.retriedID = id; return nil }
func (f *fakeDownloads) RetryFailedItems(sourceID string) (int, error) {
	f.retryFailedSource = sourceID
	return f.retryFailed, nil
}
func (f *fakeDownloads) InvalidateSourcePolicy(sourceID string) { f.invalidated = sourceID }
func (f *fakeDownloads) Defaults() (time.Duration, int, time.Duration) {
	return 500 * time.Millisecond, 3, time.Second
}
func (f *fakeDownloads) ConcurrencyDefaults() (int, int) { return 2, 3 }
func (f *fakeDownloads) PauseSource(sourceID string) {
	f.pausedSource = sourceID
	f.pausedSources = append(f.pausedSources, sourceID)
}
func (f *fakeDownloads) ResumeSource(sourceID string) {
	f.resumedSource = sourceID
	f.pausedSources = nil
}
func (f *fakeDownloads) PausedSources() []string {
	if f.pausedSources == nil {
		return []string{}
	}
	return f.pausedSources
}
func (f *fakeDownloads) PauseAll()                 { f.pausedAll = true }
func (f *fakeDownloads) ResumeAll()                { f.pausedAll = false }
func (f *fakeDownloads) Paused() bool              { return f.pausedAll }
func (f *fakeDownloads) CancelAll() (int64, error) { return f.cancelAll, nil }
func (f *fakeDownloads) Reorder(ids []int64) error {
	f.order = append([]int64(nil), ids...)
	return nil
}
func (f *fakeDownloads) Subscribe() (<-chan downloader.Event, func()) {
	return f.events, func() {}
}

func downloadRouter(downloads downloadQueue) http.Handler {
	router := chi.NewRouter()
	server := &Server{downloads: downloads}
	router.Route("/api", func(api chi.Router) { server.mountDownloads(api) })
	return router
}

func TestDownloadSnapshotAndEnqueue(t *testing.T) {
	downloads := newFakeDownloads()
	downloads.items = []db.DownloadQueueItem{{DownloadQueue: db.DownloadQueue{ID: 7, Status: db.QueuePending}}}
	downloads.stats = downloader.Stats{DownloadedPages: 4}
	handler := downloadRouter(downloads)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/download", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var snapshot downloadSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 || snapshot.Stats.DownloadedPages != 4 {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	body := `{"mangaId":"mangadex:title-id","chapters":["chapter-id"],"range":"1-10","format":"folder"}`
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if downloads.mangaID != "mangadex:title-id" || downloads.format != downloader.FormatFolder {
		t.Fatalf("enqueue = %q, %q", downloads.mangaID, downloads.format)
	}
	if downloads.selection.Range != "1-10" || len(downloads.selection.IDs) != 1 {
		t.Fatalf("selection = %+v", downloads.selection)
	}
}

func TestDownloadControlRoutes(t *testing.T) {
	downloads := newFakeDownloads()
	handler := downloadRouter(downloads)
	for route, want := range map[string]*int64{
		"pause":  &downloads.pausedID,
		"resume": &downloads.resumedID,
		"cancel": &downloads.canceledID,
		"retry":  &downloads.retriedID,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/42/"+route, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d", route, recorder.Code)
		}
		if *want != 42 {
			t.Fatalf("%s id = %d", route, *want)
		}
	}
}

func TestDownloadPauseAllRoutes(t *testing.T) {
	downloads := newFakeDownloads()
	downloads.items = []db.DownloadQueueItem{{DownloadQueue: db.DownloadQueue{ID: 7, Status: db.QueuePending}}}
	handler := downloadRouter(downloads)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/pause-all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("pause-all status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var snapshot downloadSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if !downloads.pausedAll || !snapshot.Paused || len(snapshot.Items) != 1 {
		t.Fatalf("pause-all = %+v, paused flag = %v", snapshot, downloads.pausedAll)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/resume-all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("resume-all status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if downloads.pausedAll || snapshot.Paused {
		t.Fatalf("resume-all = %+v, paused flag = %v", snapshot, downloads.pausedAll)
	}
}

func TestDownloadCancelRoutes(t *testing.T) {
	downloads := newFakeDownloads()
	downloads.cancelAll = 3
	handler := downloadRouter(downloads)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/cancel-all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("cancel-all status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/cancel", strings.NewReader(`{"itemIds":[4,5]}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(downloads.cancelIDs) != 2 || downloads.cancelIDs[0] != 4 || downloads.cancelIDs[1] != 5 {
		t.Fatalf("cancel ids = %v", downloads.cancelIDs)
	}

	// Empty, non-positive, and malformed batches are rejected before the queue
	// is touched.
	for _, body := range []string{`{"itemIds":[]}`, `{"itemIds":[0]}`, `{"itemIds":["x"]}`} {
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/cancel", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d", body, recorder.Code)
		}
	}
}

func TestDownloadReorderRoute(t *testing.T) {
	downloads := newFakeDownloads()
	handler := downloadRouter(downloads)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/reorder", strings.NewReader(`{"itemIds":[9,3,7]}`)))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("reorder status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(downloads.order) != 3 || downloads.order[0] != 9 || downloads.order[1] != 3 || downloads.order[2] != 7 {
		t.Fatalf("order = %v", downloads.order)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/download/reorder", strings.NewReader(`{"itemIds":[]}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty reorder status = %d", recorder.Code)
	}
	if len(downloads.order) != 3 {
		t.Fatalf("rejected reorder changed the order: %v", downloads.order)
	}
}

func TestDownloadEventsWebSocket(t *testing.T) {
	downloads := newFakeDownloads()
	server := httptest.NewServer(downloadRouter(downloads))
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/download/events", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.CloseNow()

	downloads.events <- downloader.Event{
		Type: "progress",
		Item: db.DownloadQueueItem{DownloadQueue: db.DownloadQueue{ID: 9, Progress: 50}},
	}
	var event downloader.Event
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if event.Type != "progress" || event.Item.ID != 9 || event.Item.Progress != 50 {
		t.Fatalf("event = %+v", event)
	}
}
