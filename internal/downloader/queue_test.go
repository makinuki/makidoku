package downloader

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

type fakeEngine struct {
	details         engine.MangaDetails
	pages           []engine.PageItem
	fetches         int
	unscrambles     int
	afterFirstFetch func()
}

func (f *fakeEngine) Details(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error) {
	return f.details, nil
}

func (f *fakeEngine) Pages(ctx context.Context, sourceID, chapterID string) ([]engine.PageItem, error) {
	return f.pages, nil
}

func (f *fakeEngine) FetchImage(ctx context.Context, sourceID, target string, headers map[string]string) ([]byte, error) {
	f.fetches++
	if f.fetches == 1 && f.afterFirstFetch != nil {
		f.afterFirstFetch()
	}
	return []byte("image-" + target), nil
}

func (f *fakeEngine) Unscramble(ctx context.Context, sourceID string, data []byte) ([]byte, error) {
	f.unscrambles++
	return append([]byte("plain-"), data...), nil
}

func (f *fakeEngine) TransferHints(ctx context.Context, sourceID string) (engine.RateLimitHints, engine.RetryHints, error) {
	return engine.RateLimitHints{}, engine.RetryHints{}, nil
}

func downloaderRepository(t *testing.T) (*db.Repository, string) {
	t.Helper()
	dataDir := t.TempDir()
	handle, err := db.Open(filepath.Join(dataDir, "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.Exec(`INSERT INTO sources(
		id, name, version, abi_version, lang, base_url, wasm_path, installed_at
	) VALUES('mangadex', 'MangaDex', '1.0.0', 1, 'multi', 'https://mangadex.org', 'mangadex.wasm', ?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return db.NewRepository(handle), dataDir
}

func queueFixture() *fakeEngine {
	number := 1.0
	return &fakeEngine{
		details: engine.MangaDetails{
			ID: "title-id", Title: "Yosuga no Sora", Description: "Summary",
			Authors: []string{"Takashi Mikaze"}, Artists: []string{"Takashi Mikaze"},
			Genres: []string{"Drama"}, Status: "completed", CoverURL: "cover",
			Chapters: []engine.ChapterItem{{ID: "chapter-id", Number: &number, Language: "en"}},
		},
		pages: []engine.PageItem{
			{Index: 0, URL: "https://uploads.example/1.jpg"},
			{Index: 1, URL: "https://uploads.example/2.jpg", IsScrambled: true},
		},
	}
}

// seedLibrary stores the source representation that queue runs refresh from
// and returns its opaque MakiDoku id.
func seedLibrary(t *testing.T, repo *db.Repository) string {
	t.Helper()
	manga, err := repo.UpsertManga(db.Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Yosuga no Sora",
		Status: "completed", CoverURL: "cover", DownloadFormat: FormatCBZ,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manga.ID
}

func TestQueueDownloadsChapterAndMarksArtifact(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 2, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{Range: "1-1"}, FormatCBZ)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queued = %d", len(items))
	}
	finished, err := queue.Drain(context.Background())
	if err != nil || finished != 1 {
		t.Fatalf("drain: %v (finished %d)", err, finished)
	}

	// A finished chapter leaves the queue, so only its chapter record remains.
	listed, err := repo.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("queue after drain = %+v", listed)
	}
	chapter, err := repo.GetChapter(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if !chapter.Downloaded || chapter.DownloadPath == nil {
		t.Fatalf("chapter = %+v", chapter)
	}
	if _, err := os.Stat(*chapter.DownloadPath); err != nil {
		t.Fatalf("artifact: %v", err)
	}
	reader, err := zip.OpenReader(*chapter.DownloadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 3 {
		t.Fatalf("archive entries = %d", len(reader.File))
	}
	if eng.unscrambles != 1 {
		t.Fatalf("unscramble calls = %d", eng.unscrambles)
	}
}

// Enqueueing pulls full details server-side, so the freshness stamp must be
// recorded: opening the title afterwards must not repeat the plugin
// round-trip.
func TestEnqueueMangaStampsDetailsFetched(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	queue := NewQueue(repo, queueFixture(), Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	if _, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{Range: "1-1"}, FormatCBZ); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	stored, err := repo.GetManga(mangaID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DetailsFetchedAt == nil {
		t.Fatal("enqueue left the details freshness stamp unset")
	}
}

// A completed download persists the page list it fetched, so the reader can
// open the chapter from the database without another source round-trip.
func TestQueuePersistsPageListOnCompletion(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{Range: "1-1"}, FormatCBZ)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := queue.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	pages, err := repo.ListPages(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("persisted pages = %d, want 2", len(pages))
	}
	if pages[0].PageIndex != 0 || pages[0].RemoteURL != "https://uploads.example/1.jpg" || pages[0].IsScrambled {
		t.Fatalf("page 0 = %+v", pages[0])
	}
	if pages[1].PageIndex != 1 || pages[1].RemoteURL != "https://uploads.example/2.jpg" || !pages[1].IsScrambled {
		t.Fatalf("page 1 = %+v", pages[1])
	}
}

func TestQueueStopsBetweenPagesWhenPaused(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	eng.afterFirstFetch = func() {
		if err := repo.PauseQueueItem(items[0].ID); err != nil {
			t.Errorf("pause: %v", err)
		}
	}
	if _, err := queue.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.QueuePaused {
		t.Fatalf("status = %s", stored.Status)
	}
	chapter, err := repo.GetChapter(stored.ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if chapter.Downloaded || chapter.DownloadPath != nil {
		t.Fatalf("paused chapter has artifact: %+v", chapter)
	}
}

// Pausing the downloader leaves queued items untouched, and resuming lets the
// worker pick them up again.
func TestQueuePauseAllStopsClaiming(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	queue.PauseAll()
	if !queue.Paused() {
		t.Fatal("queue did not report the paused state")
	}
	if _, err := queue.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.QueuePending {
		t.Fatalf("status = %s, want %s", stored.Status, db.QueuePending)
	}
	if eng.fetches != 0 {
		t.Fatalf("fetches = %d, want 0", eng.fetches)
	}

	queue.ResumeAll()
	if queue.Paused() {
		t.Fatal("queue stayed paused after resume")
	}
	if _, err := queue.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.GetChapter(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if !chapter.Downloaded {
		t.Fatalf("chapter after resume = %+v", chapter)
	}
}

// Pausing while a chapter is in flight returns the item to the queue instead of
// failing or completing it, and a later resume finishes the chapter.
func TestQueuePauseAllReleasesInFlightItem(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	eng.afterFirstFetch = func() { queue.PauseAll() }
	if _, err := queue.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.QueuePending || stored.TotalPages != 2 {
		t.Fatalf("released item = %+v", stored)
	}
	chapter, err := repo.GetChapter(stored.ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if chapter.Downloaded || chapter.DownloadPath != nil {
		t.Fatalf("paused chapter has artifact: %+v", chapter)
	}

	queue.ResumeAll()
	if finished, err := queue.Drain(context.Background()); err != nil || finished != 1 {
		t.Fatalf("resume drain: %v (finished %d)", err, finished)
	}
	// The resumed chapter finishes and its row leaves the queue.
	if _, err := repo.GetQueueItem(items[0].ID); err == nil {
		t.Fatal("finished row survived the drain")
	}
	chapter, err = repo.GetChapter(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if !chapter.Downloaded || chapter.DownloadPath == nil {
		t.Fatalf("chapter after resume = %+v", chapter)
	}
}

// Pausing and resuming the downloader announce the new state, so clients that
// did not send the request update their controls.
func TestQueuePauseAllAnnouncesState(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	queue := NewQueue(repo, queueFixture(), Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	events, unsubscribe := queue.Subscribe()
	defer unsubscribe()
	queue.PauseAll()
	select {
	case event := <-events:
		if event.Type != "state" || !event.Paused {
			t.Fatalf("pause event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("pause was not announced")
	}
	queue.ResumeAll()
	select {
	case event := <-events:
		if event.Type != "state" || event.Paused {
			t.Fatalf("resume event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("resume was not announced")
	}
}

// Cancel all cancels every active row in one call and publishes each row so
// open clients drop them.
func TestQueueCancelAllCancelsActiveItems(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := queue.Subscribe()
	defer unsubscribe()
	canceled, err := queue.CancelAll()
	if err != nil {
		t.Fatal(err)
	}
	if canceled != 1 {
		t.Fatalf("canceled = %d, want 1", canceled)
	}
	// Cancelling removes the rows: the event is what tells a client they went.
	listed, err := repo.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("queue after cancel all = %+v", listed)
	}
	select {
	case event := <-events:
		if event.Type != "canceled" || event.Item.ID != items[0].ID || event.Item.Status != db.QueueCanceled {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled event was not published")
	}
}

// A reorder rewrites the stored order the worker claims in and announces the
// change so other clients refetch the snapshot.
func TestQueueReorderPersistsAndAnnounces(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	second := 2.0
	eng.details.Chapters = append(eng.details.Chapters, engine.ChapterItem{ID: "chapter-two", Number: &second, Language: "en"})
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id", "chapter-two"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ChapterNumber == nil || *items[0].ChapterNumber != 1 {
		t.Fatalf("queued items = %+v", items)
	}
	events, unsubscribe := queue.Subscribe()
	defer unsubscribe()
	if err := queue.Reorder([]int64{items[1].ID, items[0].ID}); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != items[1].ID || listed[1].ID != items[0].ID {
		t.Fatalf("listed = %+v", listed)
	}
	select {
	case event := <-events:
		if event.Type != "reordered" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("reordered event was not published")
	}
}

// A queue row that vanishes mid-download (its chapter was retired by a
// migration, cascading the row away) must halt the worker instead of
// completing into an orphaned artifact.
func TestQueueStopsWhenQueueRowVanishes(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	eng.afterFirstFetch = func() {
		if _, err := repo.DB().Exec(`DELETE FROM download_queue WHERE id=?`, items[0].ID); err != nil {
			t.Errorf("delete queue row: %v", err)
		}
	}
	if _, err := queue.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	chapter, err := repo.GetChapter(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if chapter.Downloaded || chapter.DownloadPath != nil {
		t.Fatalf("vanished download produced an artifact: %+v", chapter)
	}
}

// After a daemon restart mid-download, the persisted page record plus staged
// files let the worker finish without refetching completed pages.
func TestResumeContinuesFromStagedPages(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	downloadDir := filepath.Join(dataDir, "downloads")
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: downloadDir, MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an interrupted attempt: one page fetched and staged, recorded
	// in the queue row, then the process died.
	claimed, err := repo.ClaimNextQueueItem()
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v, err = %v", claimed, err)
	}
	tempDir := filepath.Join(downloadDir, ".tmp", strconv.FormatInt(claimed.ID, 10))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "000000.jpg"), []byte("image-page-0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveQueuePageProgress(claimed.ID, 2, []int{0}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetInterruptedQueue(); err != nil {
		t.Fatal(err)
	}

	if finished, err := queue.Drain(context.Background()); err != nil || finished != 1 {
		t.Fatalf("drain after restart: %v (finished %d)", err, finished)
	}
	if _, err := repo.GetQueueItem(items[0].ID); err == nil {
		t.Fatal("finished row survived the drain")
	}
	chapter, err := repo.GetChapter(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(*chapter.DownloadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 3 {
		t.Fatalf("archive entries = %d, want both pages plus ComicInfo", len(reader.File))
	}
	if eng.fetches != 1 {
		t.Fatalf("image fetches = %d, want only the missing page refetched", eng.fetches)
	}
}

func TestEnqueueMangaKeepsStoredDownloadFormatWhenOmitted(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{Workers: 1, DownloadDir: filepath.Join(dataDir, "downloads")})
	mangaID := seedLibrary(t, repo)
	all := ChapterSelection{IDs: []string{"chapter-id"}}
	if _, err := queue.EnqueueManga(context.Background(), mangaID, all, FormatFolder); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.EnqueueManga(context.Background(), mangaID, all, ""); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.GetManga(mangaID)
	if err != nil {
		t.Fatal(err)
	}
	if manga.DownloadFormat != FormatFolder {
		t.Fatalf("download format = %q", manga.DownloadFormat)
	}
}

// An empty selection must be rejected instead of silently queueing every
// chapter of the title.
func TestEnqueueMangaRejectsEmptySelection(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	queue := NewQueue(repo, queueFixture(), Options{Workers: 1, DownloadDir: filepath.Join(dataDir, "downloads")})
	mangaID := seedLibrary(t, repo)
	if _, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{}, FormatCBZ); err == nil {
		t.Fatal("accepted an empty chapter selection")
	}
}

// A failed download can be returned to the queue and completed on retry; the
// finished row then leaves the queue without touching the chapter record.
func TestRetryThenDrainFinishesDownload(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	mangaID := seedLibrary(t, repo)
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{IDs: []string{"chapter-id"}}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextQueueItem(); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkQueueFailed(items[0].ID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}

	if err := queue.Retry(items[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	retried, err := repo.GetQueueItem(items[0].ID)
	if err != nil || retried.Status != db.QueuePending {
		t.Fatalf("retried status = %+v, err = %v", retried.Status, err)
	}

	if finished, err := queue.Drain(context.Background()); err != nil || finished != 1 {
		t.Fatalf("drain after retry: %v (finished %d)", err, finished)
	}
	if _, err := repo.GetQueueItem(items[0].ID); err == nil {
		t.Fatal("finished row survived the drain")
	}
	chapter, err := repo.GetChapter(items[0].ChapterID)
	if err != nil || !chapter.Downloaded {
		t.Fatalf("chapter after retry = %+v, err = %v", chapter.Downloaded, err)
	}
}
