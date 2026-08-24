package downloader

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
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
	if err := queue.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	stored, err := repo.GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.QueueCompleted || stored.Progress != 100 {
		t.Fatalf("queue item = %+v", stored)
	}
	chapter, err := repo.GetChapter(stored.ChapterID)
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
	if err := queue.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	stored, err := repo.GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := repo.ListPages(stored.ChapterID)
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
	items, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{}, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	eng.afterFirstFetch = func() {
		if err := repo.PauseQueueItem(items[0].ID); err != nil {
			t.Errorf("pause: %v", err)
		}
	}
	if err := queue.Drain(context.Background()); err != nil {
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

func TestEnqueueMangaKeepsStoredDownloadFormatWhenOmitted(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{Workers: 1, DownloadDir: filepath.Join(dataDir, "downloads")})
	mangaID := seedLibrary(t, repo)
	if _, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{}, FormatFolder); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.EnqueueManga(context.Background(), mangaID, ChapterSelection{}, ""); err != nil {
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
