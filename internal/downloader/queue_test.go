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

func TestQueueDownloadsChapterAndMarksArtifact(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 2, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	items, err := queue.EnqueueManga(context.Background(), "mangadex:title-id", ChapterSelection{Range: "1-1"}, FormatCBZ)
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

func TestQueueStopsBetweenPagesWhenPaused(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	queue := NewQueue(repo, eng, Options{
		Workers: 1, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
	})
	items, err := queue.EnqueueManga(context.Background(), "mangadex:title-id", ChapterSelection{}, FormatCBZ)
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
	if _, err := queue.EnqueueManga(context.Background(), "mangadex:title-id", ChapterSelection{}, FormatFolder); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.EnqueueManga(context.Background(), "mangadex:title-id", ChapterSelection{}, ""); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.GetManga("mangadex:title-id")
	if err != nil {
		t.Fatal(err)
	}
	if manga.DownloadFormat != FormatFolder {
		t.Fatalf("download format = %q", manga.DownloadFormat)
	}
}
