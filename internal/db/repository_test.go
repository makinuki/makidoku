package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestReadingSessionsAccumulateAndSummarize(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "session-title", Title: "Session title",
		Status: "ongoing", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "session-chapter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 2, SessionSeconds: 17}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 2, TotalPages: 2, SessionSeconds: 8}); err != nil {
		t.Fatal(err)
	}
	seconds, err := repo.ReadingSeconds(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seconds != 25 {
		t.Fatalf("reading seconds = %d, want 25", seconds)
	}
}

func TestReadingSessionRejectsNegativeSecondsAtSchemaBoundary(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "session-invalid", Title: "Invalid", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB().Exec(`INSERT INTO reading_sessions(id,manga_id,seconds,occurred_at) VALUES(?,?,?,?)`, "bad", manga.ID, -1, time.Now().Unix())
	if err == nil {
		t.Fatal("negative reading session was accepted")
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

func TestMangaDetailsFetchedAtLifecycle(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Yosuga no Sora",
		Status: "completed", CoverURL: "https://covers.test/cover.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manga.DetailsFetchedAt != nil {
		t.Fatalf("fresh upsert claims fetched details: %+v", manga.DetailsFetchedAt)
	}

	stamp := int64(1700000000)
	if err := repo.SetMangaDetailsFetched(manga.ID, stamp); err != nil {
		t.Fatal(err)
	}

	// Search-level upserts must not clobber the freshness marker.
	if _, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "title-id", Title: "Yosuga no Sora",
		Status: "completed", CoverURL: "https://covers.test/cover.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetManga(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DetailsFetchedAt == nil || *stored.DetailsFetchedAt != stamp {
		t.Fatalf("details fetched at = %+v, want %d", stored.DetailsFetchedAt, stamp)
	}
}

// A queue item canceled while the archive was being written must not flip
// back to completed: the chapter stays unmarked and the row stays canceled.
func TestMarkChapterDownloadedRespectsCanceledQueueItem(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter-id"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.EnqueueChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextQueueItem(); err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelQueueItem(item.ID); err != nil {
		t.Fatal(err)
	}

	if err := repo.MarkChapterDownloaded(chapter.ID, `C:\manga\chapter.cbz`); err == nil {
		t.Fatal("marked a chapter whose download was canceled")
	}
	stored, err := repo.GetChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Downloaded || stored.DownloadPath != nil {
		t.Fatalf("canceled chapter kept a downloaded state: %+v", stored)
	}
	row, err := repo.GetQueueItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != QueueCanceled {
		t.Fatalf("queue status = %q, want CANCELED", row.Status)
	}
}

// Completion is monotonic: navigating back a page after finishing must not
// clear the completed flag (the remote tracker keeps it completed).
func TestUpsertReadingProgressKeepsCompletion(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "one", Title: "One", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "chapter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 10, TotalPages: 10, IsCompleted: true}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 9, TotalPages: 10, IsCompleted: false})
	if err != nil {
		t.Fatal(err)
	}
	if !stored.IsCompleted {
		t.Fatal("navigating back cleared the completed flag")
	}
}

func TestSettingsRoundTripAndChapterReadState(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "settings", Title: "Settings", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "settings-chapter", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSetting("reader.default_mode", `"double"`); err != nil {
		t.Fatal(err)
	}
	setting, err := repo.GetSetting("reader.default_mode")
	if err != nil || setting.Value != `"double"` {
		t.Fatalf("setting = %+v, err = %v", setting, err)
	}
	if err := repo.SetChapterRead(chapter.ID, manga.ID, true); err != nil {
		t.Fatal(err)
	}
	read, err := repo.GetChapterRead(chapter.ID)
	if err != nil || !read.Read || read.MangaID != manga.ID {
		t.Fatalf("read state = %+v, err = %v", read, err)
	}
}

func TestUpsertReadingProgressCoalescesHistoryEvent(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "history-event", Title: "History", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "history-chapter"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "history-chapter-2"})
	if err != nil {
		t.Fatal(err)
	}
	writes := []ReadingProgress{
		{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 3},
		{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 2, TotalPages: 3},
		{MangaID: manga.ID, LastReadChapterID: other.ID, LastReadPage: 1, TotalPages: 3},
		{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 3, TotalPages: 3},
	}
	for _, write := range writes {
		if _, err := repo.UpsertReadingProgress(write); err != nil {
			t.Fatal(err)
		}
	}
	events, err := repo.ListHistoryEvents(10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	byChapter := map[string]HistoryEvent{}
	for _, event := range events {
		if event.ChapterID == nil {
			t.Fatalf("event without chapter = %+v", event)
		}
		byChapter[*event.ChapterID] = event
	}
	if byChapter[chapter.ID].Page == nil || *byChapter[chapter.ID].Page != 3 {
		t.Fatalf("chapter event = %+v", byChapter[chapter.ID])
	}
	if byChapter[other.ID].Page == nil || *byChapter[other.ID].Page != 1 {
		t.Fatalf("other chapter event = %+v", byChapter[other.ID])
	}
}

func TestAcknowledgeUpdatesMarksChapterRead(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "update-read", Title: "Update", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "update-chapter"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := repo.RecordUpdate(manga.ID, chapter.ID, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AcknowledgeUpdates([]string{entry.ID}); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetChapterRead(chapter.ID)
	if err != nil || !state.Read {
		t.Fatalf("read state = %+v, err = %v", state, err)
	}
}

// Finished sync history is trimmed to the newest entries; pending and
// running jobs are never touched.
func TestPruneTerminalTrackerSyncJobsKeepsNewest(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "sync", Title: "Sync", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.UpsertTrackerBinding(TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "S"})
	if err != nil {
		t.Fatal(err)
	}
	var oldest, kept int64
	for i := 0; i < 4; i++ {
		job, err := repo.EnqueueTrackerSync(manga.ID, binding.ID, float64(i+1))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldest = job.ID
			if err := repo.CompleteTrackerSync(job.ID); err != nil {
				t.Fatal(err)
			}
		} else if i == 3 {
			kept = job.ID
			if err := repo.CompleteTrackerSync(job.ID); err != nil {
				t.Fatal(err)
			}
		} else if i == 1 {
			if err := repo.FailTrackerSync(job.ID, false, "gone"); err != nil {
				t.Fatal(err)
			}
		}
	}
	removed, err := repo.PruneTerminalTrackerSyncJobs(2)
	if err != nil || removed != 1 {
		t.Fatalf("pruned = %d, err = %v", removed, err)
	}
	jobs, err := repo.ListTrackerSyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID == oldest {
			t.Fatal("oldest terminal job survived the prune")
		}
		if job.Status == QueueCompleted && job.ID != kept {
			t.Fatalf("unexpected retained completed job %d", job.ID)
		}
	}
	if len(jobs) != 3 {
		t.Fatalf("remaining jobs = %d, want pending + failed + newest completed", len(jobs))
	}
	var foundKept bool
	for _, job := range jobs {
		if job.ID == kept && job.Status == QueueCompleted {
			foundKept = true
		}
	}
	if !foundKept {
		t.Fatal("the newest completed job was pruned out of order")
	}
}

// A progress row whose chapter has vanished (retired by a migration) must be
// skipped instead of blanking the whole history page.
func TestListHistorySkipsOrphanedProgress(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "h", Title: "History", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 5}); err != nil {
		t.Fatal(err)
	}
	// Simulate a row from a legacy or partially restored database by writing
	// one progress entry whose chapter does not exist.
	if _, err := repo.DB().Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB().Exec(`INSERT INTO reading_progress(manga_id,last_read_chapter_id,last_read_page,total_pages,is_completed,last_read_at) VALUES('ghost-manga', 'ghost-chapter', 1, 5, 0, 99)`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB().Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListHistory()
	if err != nil {
		t.Fatalf("list history failed on an orphaned row: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want the valid entry with the ghost skipped", len(items))
	}
}

// When a source re-parents an external chapter to another series, the
// canonical record must follow instead of staying on the stale entry.
func TestUpsertChapterReParentsOnUpdate(t *testing.T) {
	repo := testRepository(t)
	mangaA, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "series-a", Title: "Series A", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	mangaB, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "series-b", Title: "Series B", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	original, err := repo.UpsertChapter(Chapter{MangaID: mangaA.ID, SourceChapterID: "ext-1", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}

	moved, err := repo.UpsertChapter(Chapter{MangaID: mangaB.ID, SourceChapterID: "ext-1", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}
	if moved.MangaID != mangaB.ID {
		t.Fatalf("chapter stayed on %q, want re-parented to %q", moved.MangaID, mangaB.ID)
	}
	chaptersA, err := repo.ListChapters(mangaA.ID)
	if err != nil || len(chaptersA) != 0 {
		t.Fatalf("old series still holds %d chapters, err = %v", len(chaptersA), err)
	}
	_ = original
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

func TestUpsertTrackerBindingKeepsExistingScoreForSameRemote(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "tracker-score", Title: "Tracker", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	score := 8.5
	if _, err := repo.UpsertTrackerBinding(TrackerBinding{
		MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "Old", RemoteScore: &score,
	}); err != nil {
		t.Fatal(err)
	}
	newScore := 6.0
	updated, err := repo.UpsertTrackerBinding(TrackerBinding{
		MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "New", RemoteScore: &newScore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RemoteScore == nil || *updated.RemoteScore != score || updated.RemoteTitle != "New" {
		t.Fatalf("binding = %+v", updated)
	}
}

func TestUpsertMangaStubCreatesBareEntry(t *testing.T) {
	repo := testRepository(t)
	stub, err := repo.UpsertMangaStub(Manga{
		SourceID: "mangadex", SourceMangaID: "remote-1",
		Title: "Search Hit", CoverURL: "cover", Status: "unknown",
	})
	if err != nil {
		t.Fatalf("stub upsert: %v", err)
	}
	stored, err := repo.GetManga(stub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Search Hit" || stored.Status != "unknown" || stored.CoverURL != "cover" {
		t.Fatalf("bare entry = %+v", stored)
	}
}

func TestUpsertMangaStubKeepsStoredDetails(t *testing.T) {
	repo := testRepository(t)
	description := "A long synopsis"
	authors := "[\"Author\"]"
	genres := "[\"Drama\"]"
	full, err := repo.UpsertManga(Manga{
		SourceID: "mangadex", SourceMangaID: "remote-1",
		Title: "Stored Title", Status: "ongoing", CoverURL: "stored-cover",
		Description: &description, Authors: &authors, Genres: &genres,
		InLibrary: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	stub, err := repo.UpsertMangaStub(Manga{
		SourceID: "mangadex", SourceMangaID: "remote-1",
		Title: "Search Title", CoverURL: "search-cover", Status: "unknown",
	})
	if err != nil {
		t.Fatalf("stub upsert: %v", err)
	}
	if stub.ID != full.ID {
		t.Fatalf("stub id = %q, want %q", stub.ID, full.ID)
	}
	stored, err := repo.GetManga(full.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Stored Title" || stored.Status != "ongoing" || stored.CoverURL != "stored-cover" ||
		stored.Description == nil || *stored.Description != description ||
		stored.Authors == nil || *stored.Authors != authors ||
		stored.Genres == nil || *stored.Genres != genres || !stored.InLibrary {
		t.Fatalf("listing upsert degraded stored details: %+v", stored)
	}
}

func TestUpsertMangaStubRejectsMissingSourceReference(t *testing.T) {
	repo := testRepository(t)
	if _, err := repo.UpsertMangaStub(Manga{SourceID: "nope", SourceMangaID: "remote-1", Title: "X"}); err == nil {
		t.Fatal("accepted an unknown source reference")
	}
	if _, err := repo.UpsertMangaStub(Manga{SourceID: "mangadex", SourceMangaID: "  ", Title: "X"}); err == nil {
		t.Fatal("accepted an empty source manga id")
	}
}

// The library payload must expose the backend cover route, not the raw
// source URL, so covers keep working offline and behind anti-bot checks.
func TestListLibraryExposesBackendCoverRoute(t *testing.T) {
	repo := testRepository(t)
	if _, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "cover", Title: "Covered", Status: "ongoing", CoverURL: "https://upstream.test/cover.png", InLibrary: true}); err != nil {
		t.Fatal(err)
	}
	library, err := repo.ListLibrary("", 0)
	if err != nil || len(library) != 1 {
		t.Fatalf("library = %+v, err = %v", library, err)
	}
	payload, err := json.Marshal(library[0])
	if err != nil {
		t.Fatal(err)
	}
	var item struct {
		CoverURL string `json:"coverUrl"`
	}
	if err := json.Unmarshal(payload, &item); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(item.CoverURL, "/api/manga/") || !strings.HasSuffix(item.CoverURL, "/cover") {
		t.Fatalf("coverUrl = %q, want the backend route", item.CoverURL)
	}
}

func TestMigrateMangaSourceRetiresAndAttaches(t *testing.T) {
	repo := testRepository(t)
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('asura','Asura','1',1,'en','https://asura.test','x',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "old", Title: "Old", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 4.0
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceID: "mangadex", SourceChapterID: "c-old", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "Chapter 4.cbz")
	if err := os.WriteFile(artifact, []byte("pages"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapter.ID, artifact); err != nil {
		t.Fatal(err)
	}
	discovered, err := repo.UpsertManga(Manga{SourceID: "asura", SourceMangaID: "new", Title: "New", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}

	// Keep the replacement source and retire everything from mangadex.
	retired, artifacts, err := repo.MigrateMangaSource(manga.ID, "asura", discovered.ID)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(retired) != 1 || retired[0].ID != chapter.ID {
		t.Fatalf("retired = %+v", retired)
	}
	if len(artifacts) != 1 || artifacts[0] != artifact {
		t.Fatalf("artifacts = %+v, want the downloaded path", artifacts)
	}
	chapters, err := repo.ListChapters(manga.ID)
	if err != nil || len(chapters) != 0 {
		t.Fatalf("chapters = %+v, err = %v", chapters, err)
	}
	source, err := repo.GetMangaSource(manga.ID)
	if err != nil || source.SourceID != "asura" {
		t.Fatalf("primary source = %+v, err = %v", source, err)
	}
	// The canonical row itself must move so name resolution and exports
	// reflect the replacement everywhere.
	stored, err := repo.GetManga(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SourceID != "asura" {
		t.Fatalf("canonical source id = %q, want asura", stored.SourceID)
	}
}

// A failed attach must roll the retirement back too: the title keeps its
// previous source's chapter list instead of ending up empty.
func TestMigrateMangaSourceRollsBackWhenAttachFails(t *testing.T) {
	repo := testRepository(t)
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('asura','Asura','1',1,'en','https://asura.test','x',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "old", Title: "Old", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	if _, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceID: "mangadex", SourceChapterID: "c-old", ChapterNumber: &number}); err != nil {
		t.Fatal(err)
	}
	// The replacement has a manga row but no source link, so the attach step
	// cannot resolve it.
	discovered, err := repo.UpsertManga(Manga{SourceID: "asura", SourceMangaID: "new", Title: "New", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB().Exec(`DELETE FROM manga_sources WHERE manga_id=?`, discovered.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := repo.MigrateMangaSource(manga.ID, "asura", discovered.ID); err == nil {
		t.Fatal("expected the migration to fail without a replacement source link")
	}
	chapters, err := repo.ListChapters(manga.ID)
	if err != nil || len(chapters) != 1 {
		t.Fatalf("chapters = %+v, err = %v, want the old list intact", chapters, err)
	}
}

// A source that re-issues a chapter under a new identifier must keep the
// canonical record: the caller passes the existing chapter ID, and the stored
// identity is re-pointed at the new source identifier while download and read
// state stay attached to the same row.
func TestUpsertChapterReusesCanonicalIDForReissuedSource(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "title-id", Title: "Title", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 7.0
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "old-url", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapter.ID, `C:\manga\chapter.cbz`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetChapterRead(chapter.ID, manga.ID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.UpsertChapter(Chapter{ID: chapter.ID, MangaID: manga.ID, SourceChapterID: "new-url", ChapterNumber: &number}); err != nil {
		t.Fatal(err)
	}
	chapters, err := repo.ListChapters(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 1 {
		t.Fatalf("chapters = %d, want the re-issued chapter to reuse the existing record", len(chapters))
	}
	if chapters[0].ID != chapter.ID || chapters[0].SourceChapterID != "new-url" {
		t.Fatalf("chapter = %+v, want canonical id %s linked to new-url", chapters[0], chapter.ID)
	}
	if !chapters[0].Downloaded || chapters[0].DownloadPath == nil || *chapters[0].DownloadPath == "" {
		t.Fatalf("download state was lost: %+v", chapters[0])
	}
	state, err := repo.GetChapterRead(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Read {
		t.Fatalf("read state was lost: %+v", state)
	}
}

// The manga row carries no source-side identifier: the link lives in
// manga_sources and resolves through GetMangaSource.
func TestMangaSchemaHasNoSourceMangaIDColumn(t *testing.T) {
	repo := testRepository(t)
	var columns int
	if err := repo.DB().Get(&columns, `SELECT COUNT(*) FROM pragma_table_info('manga') WHERE name='source_manga_id'`); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatalf("manga.source_manga_id still present after migration")
	}
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "yosuga", Title: "Yosuga", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := repo.GetMangaSource(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if link.SourceID != "mangadex" || link.SourceMangaID != "yosuga" {
		t.Fatalf("source link = %+v", link)
	}
}

// The series page a listing declares belongs to the source link, so it
// survives detail upserts that carry no listing URL.
func TestMangaSourceStoresSeriesPageURL(t *testing.T) {
	repo := testRepository(t)
	series := "https://mangadex.org/title/uuid/yosuga-no-sora"
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "yosuga", SourcePageURL: series, Title: "Yosuga", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := repo.GetMangaSource(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != series {
		t.Fatalf("series page = %q, want %q", link.URL, series)
	}

	// A details refresh carries no locator and must not clear the stored one.
	if _, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "yosuga", Title: "Yosuga", Status: "ongoing"}); err != nil {
		t.Fatal(err)
	}
	if link, err = repo.GetMangaSource(manga.ID); err != nil || link.URL != series {
		t.Fatalf("series page after detail upsert = %q (err %v)", link.URL, err)
	}

	// A listing that reappears under a rotated locator refreshes it.
	rotated := "https://mangadex.org/title/uuid/yosuga-no-sora-a1b2c3d4"
	if _, err := repo.UpsertMangaStub(Manga{SourceID: "mangadex", SourceMangaID: "yosuga", SourcePageURL: rotated, Title: "Yosuga"}); err != nil {
		t.Fatal(err)
	}
	if link, err = repo.GetMangaSource(manga.ID); err != nil || link.URL != rotated {
		t.Fatalf("series page after listing = %q (err %v)", link.URL, err)
	}
}

// Per-title reader overrides round-trip, survive an unrelated metadata refresh,
// and clear back to the global fallback with NULLs.
func TestMangaReaderOverridesRoundTrip(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "reader-override", Title: "Reader Override", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	if manga.ReaderMode != nil || manga.ReaderDirection != nil || manga.ReaderFit != nil {
		t.Fatalf("new manga carried overrides: %+v", manga)
	}
	mode, direction, fit := "double", "rtl", "height"
	updated, err := repo.SetMangaReaderOverrides(manga.ID, &mode, &direction, &fit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ReaderMode == nil || *updated.ReaderMode != mode ||
		updated.ReaderDirection == nil || *updated.ReaderDirection != direction ||
		updated.ReaderFit == nil || *updated.ReaderFit != fit {
		t.Fatalf("stored overrides = %+v", updated)
	}
	refreshed, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "reader-override", Title: "Reader Override Renamed", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ReaderMode == nil || *refreshed.ReaderMode != mode {
		t.Fatalf("refresh dropped the override: %+v", refreshed)
	}
	cleared, err := repo.SetMangaReaderOverrides(manga.ID, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ReaderMode != nil || cleared.ReaderDirection != nil || cleared.ReaderFit != nil {
		t.Fatalf("cleared overrides = %+v", cleared)
	}
	bad := "scroll"
	if _, err := repo.SetMangaReaderOverrides(manga.ID, &bad, nil, nil); err == nil {
		t.Fatal("unsupported reader mode was accepted")
	}
}

// The statistics payload reports grouped counters built from local state, plus
// a per-title reading breakdown ordered by recorded time.
func TestReadingStatsGroups(t *testing.T) {
	repo := testRepository(t)
	library, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "stats", Title: "Stats", Status: "ongoing", InLibrary: true, DownloadNewChapters: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "stats-2", Title: "Stats Two", Status: "ongoing", InLibrary: true}); err != nil {
		t.Fatal(err)
	}
	read, err := repo.UpsertChapter(Chapter{MangaID: library.ID, SourceChapterID: "stats-c1"})
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := repo.UpsertChapter(Chapter{MangaID: library.ID, SourceChapterID: "stats-c2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetChapterRead(read.ID, library.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: library.ID, LastReadChapterID: read.ID, LastReadPage: 5, TotalPages: 5, IsCompleted: true, SessionSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(downloaded.ID, "/tmp/stats-c2.cbz"); err != nil {
		t.Fatal(err)
	}
	score := 8.5
	if _, err := repo.UpsertTrackerBinding(TrackerBinding{MangaID: library.ID, TrackerType: "anilist", RemoteID: "42", RemoteTitle: "Stats", RemoteScore: &score}); err != nil {
		t.Fatal(err)
	}

	stats, err := repo.ReadingStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Overview.LibraryMangaCount != 2 || stats.Overview.CompletedMangaCount != 1 || stats.Overview.TotalReadDuration != 120 {
		t.Fatalf("overview = %+v", stats.Overview)
	}
	if stats.Titles.UpdateEnabledCount != 1 || stats.Titles.StartedMangaCount != 1 {
		t.Fatalf("titles = %+v", stats.Titles)
	}
	if stats.Chapters.TotalChapterCount != 2 || stats.Chapters.ReadChapterCount != 1 || stats.Chapters.DownloadCount != 1 {
		t.Fatalf("chapters = %+v", stats.Chapters)
	}
	if stats.Trackers.TrackedTitleCount != 1 || stats.Trackers.TrackerCount != 1 || stats.Trackers.MeanScore != 8.5 {
		t.Fatalf("trackers = %+v", stats.Trackers)
	}
	if len(stats.TopTitles) != 1 || stats.TopTitles[0].Title != "Stats" || stats.TopTitles[0].Seconds != 120 || stats.TopTitles[0].ChaptersRead != 1 {
		t.Fatalf("top titles = %+v", stats.TopTitles)
	}
}
