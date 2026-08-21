package db

import (
	"path/filepath"
	"testing"
	"time"
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

func TestUpsertMangaAndChapterBuildCompositeIDs(t *testing.T) {
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
	if manga.ID != "mangadex:title-id" {
		t.Fatalf("manga id = %q", manga.ID)
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
	if chapter.ID != "mangadex:title-id:chapter-id" {
		t.Fatalf("chapter id = %q", chapter.ID)
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
