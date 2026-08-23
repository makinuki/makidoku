package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/identity"
)

func testRepository(t *testing.T) *Repository {
	t.Helper()
	handle, err := Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.Exec(`INSERT INTO sources(
		id, name, version, abi_version, lang, base_url, wasm_path, installed_at
	) VALUES('mangadex', 'MangaDex', '1.0.0', 1, 'multi', 'https://mangadex.org', 'mangadex.wasm', ?)`, time.Now().Unix()); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	return NewRepository(handle)
}

func TestUpsertMangaAndChapterAssignOpaqueIDs(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID:       "mangadex",
		SourceMangaID:  "title-id",
		Title:          "Yosuga no Sora",
		Status:         "completed",
		CoverURL:       "https://example.test/cover.jpg",
		DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatalf("upsert manga: %v", err)
	}
	if !identity.IsValid(manga.ID) {
		t.Fatalf("manga id = %q, want a UUIDv7", manga.ID)
	}

	number := 1.0
	chapter, err := repo.UpsertChapter(Chapter{
		MangaID:         manga.ID,
		SourceChapterID: "chapter-id",
		ChapterNumber:   &number,
	})
	if err != nil {
		t.Fatalf("upsert chapter: %v", err)
	}
	if !identity.IsValid(chapter.ID) {
		t.Fatalf("chapter id = %q, want a UUIDv7", chapter.ID)
	}
	stored, err := repo.GetChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SourceChapterID != "chapter-id" || stored.SourceID != "mangadex" {
		t.Fatalf("source resolution = %+v", stored)
	}
	reread, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter-id"})
	if err != nil {
		t.Fatal(err)
	}
	if reread.ID != chapter.ID {
		t.Fatalf("re-upsert changed the chapter id from %q to %q", chapter.ID, reread.ID)
	}
}

func TestUpsertChapterPreservesDownloadedArtifact(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title",
		Status: "ongoing", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter-id"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapter.ID, `C:\manga\chapter.cbz`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter-id"}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Downloaded || stored.DownloadPath == nil || *stored.DownloadPath == "" {
		t.Fatalf("download state was lost: %+v", stored)
	}
}

func TestQueueStateMachine(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title",
		Status: "ongoing", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter-id"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.EnqueueChapter(chapter.ID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if item.Status != QueuePending {
		t.Fatalf("status = %q", item.Status)
	}

	if err := repo.PauseQueueItem(item.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResumeQueueItem(item.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextQueueItem()
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != item.ID || claimed.Status != QueueDownloading {
		t.Fatalf("claimed = %+v", claimed)
	}
	if err := repo.UpdateQueueProgress(item.ID, 4, 2, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelQueueItem(item.ID); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("queue length = %d", len(items))
	}
	if items[0].Status != QueueCanceled || items[0].Progress != 50 {
		t.Fatalf("queue item = %+v", items[0])
	}
	if err := repo.ResumeQueueItem(item.ID); err == nil {
		t.Fatal("a canceled item must not resume")
	}
}

func TestUpsertChapterPersistsVolume(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Yosuga no Sora",
		Status: "completed", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	number := 10.5
	volume := int64(3)
	chapter, err := repo.UpsertChapter(Chapter{
		MangaID: manga.ID, SourceChapterID: "ch-10-5",
		ChapterNumber: &number, Volume: &volume,
	})
	if err != nil {
		t.Fatalf("upsert chapter: %v", err)
	}
	stored, err := repo.GetChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Volume == nil || *stored.Volume != 3 {
		t.Fatalf("stored volume = %+v, want 3", stored.Volume)
	}

	if _, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "ch-10-5", ChapterNumber: &number}); err != nil {
		t.Fatal(err)
	}
	cleared, err := repo.GetChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Volume != nil {
		t.Fatalf("refreshed volume = %+v, want absent after a refresh without one", cleared.Volume)
	}
}

func TestQueueItemsCarryVolume(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title",
		Status: "ongoing", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	volume := int64(2)
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter-id", Volume: &volume})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueChapter(chapter.ID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items, err := repo.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Volume == nil || *items[0].Volume != 2 {
		t.Fatalf("queue items = %+v, want volume 2", items)
	}
}

func TestUpsertMangaCoverChangeInvalidatesCache(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title",
		Status: "ongoing", CoverURL: "https://covers.test/old.jpg", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaCover(manga.ID, "covers/1.img", "image/jpeg", 100); err != nil {
		t.Fatal(err)
	}

	same, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title",
		Status: "ongoing", CoverURL: "https://covers.test/old.jpg", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if same.CoverCachePath == nil || *same.CoverCachePath != "covers/1.img" {
		t.Fatalf("cache was dropped although the URL is unchanged: %+v", same)
	}

	changed, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title",
		Status: "ongoing", CoverURL: "https://covers.test/new.512.jpg", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.CoverURL != "https://covers.test/new.512.jpg" {
		t.Fatalf("cover url = %q", changed.CoverURL)
	}
	if changed.CoverCachePath != nil || changed.CoverContentType != nil || changed.CoverFetchedAt != nil {
		t.Fatalf("stale cache survived a cover url change: %+v", changed)
	}
}

func TestReadingProgressRequiresChapterFromManga(t *testing.T) {
	repo := testRepository(t)
	first, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "one", Title: "One", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "two", Title: "Two", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: second.ID, SourceChapterID: "chapter"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.UpsertReadingProgress(ReadingProgress{MangaID: first.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 10})
	if err == nil {
		t.Fatal("accepted chapter from another manga")
	}
}

func TestRebindingTrackerClearsOldSyncHistory(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "tracker", Title: "Tracker", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.UpsertTrackerBinding(TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.EnqueueTrackerSync(manga.ID, binding.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteTrackerSync(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTrackerSyncedChapter(binding.ID, 3); err != nil {
		t.Fatal(err)
	}
	rebound, err := repo.UpsertTrackerBinding(TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "2", RemoteTitle: "New"})
	if err != nil {
		t.Fatal(err)
	}
	if rebound.LastSyncedChapter != 0 {
		t.Fatalf("last synced chapter = %v", rebound.LastSyncedChapter)
	}
	jobs, err := repo.ListTrackerSyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("old jobs remained: %+v", jobs)
	}
	queued, err := repo.EnqueueTrackerSync(manga.ID, rebound.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != SyncPending {
		t.Fatalf("queued status = %q", queued.Status)
	}
}
