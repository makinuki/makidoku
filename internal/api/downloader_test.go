package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
)

type fakeDownloads struct {
	items      []db.DownloadQueueItem
	stats      downloader.Stats
	selection  downloader.ChapterSelection
	format     string
	mangaID    string
	pausedID   int64
	resumedID  int64
	canceledID int64
	events     chan downloader.Event
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
func (f *fakeDownloads) Cancel(id int64) error { f.canceledID = id; return nil }
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
