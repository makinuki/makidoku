package db

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// prefsRepository opens a database holding two installed sources so a bulk
// retry can be checked against a source it must not touch.
func prefsRepository(t *testing.T) *Repository {
	t.Helper()
	handle, err := Open(filepath.Join(t.TempDir(), "prefs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	for index, id := range []string{"mangadex", "other"} {
		if _, err := handle.Exec(`INSERT INTO sources(
			id, name, version, abi_version, lang, base_url, wasm_path, installed_at
		) VALUES(?, ?, '1.0.0', 1, 'multi', 'https://example.test', 'x.wasm', ?)`,
			id, id, time.Now().Unix()+int64(index)); err != nil {
			t.Fatal(err)
		}
	}
	return NewRepository(handle)
}

func TestSourceDownloadPrefsRoundTrip(t *testing.T) {
	repo := prefsRepository(t)

	prefs, err := repo.GetSourceDownloadPrefs("mangadex")
	if err != nil || prefs != nil {
		t.Fatalf("unset prefs = %+v, %v", prefs, err)
	}

	interval, attempts := int64(120), int64(1)
	if err := repo.SetSourceDownloadPrefs(SourceDownloadPrefs{SourceID: "mangadex", IntervalMs: &interval, MaxAttempts: &attempts}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSourceDownloadPrefs("mangadex")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.IntervalMs == nil || *got.IntervalMs != 120 || got.MaxAttempts == nil || *got.MaxAttempts != 1 || got.BackoffMs != nil {
		t.Fatalf("stored prefs = %+v", got)
	}

	// Storing a new set replaces the previous one field for field, so a
	// field left out returns to following the source and the defaults.
	backoff := int64(2000)
	if err := repo.SetSourceDownloadPrefs(SourceDownloadPrefs{SourceID: "mangadex", BackoffMs: &backoff}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetSourceDownloadPrefs("mangadex")
	if err != nil {
		t.Fatal(err)
	}
	if got.IntervalMs != nil || got.MaxAttempts != nil || got.BackoffMs == nil || *got.BackoffMs != 2000 {
		t.Fatalf("replaced prefs = %+v", got)
	}

	// A fully empty override deletes the row.
	if err := repo.SetSourceDownloadPrefs(SourceDownloadPrefs{SourceID: "mangadex"}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetSourceDownloadPrefs("mangadex")
	if err != nil || got != nil {
		t.Fatalf("cleared prefs = %+v, %v", got, err)
	}
}

func TestRetryFailedQueueItemsBySource(t *testing.T) {
	repo := prefsRepository(t)

	otherFailedID := queueItemForSource(t, repo, "other", "bulk-retry-b", "chapter-1")
	failedID := queueItemForSource(t, repo, "mangadex", "bulk-retry-a", "chapter-1")
	pendingID := queueItemForSource(t, repo, "mangadex", "bulk-retry-a", "chapter-2")

	// Failures are recorded from the in-flight state, so the rows that must
	// end up failed are claimed and failed in queue order first; the row
	// that stays queued is claimed last and released back to pending.
	claimed, err := repo.ClaimNextQueueItem()
	if err != nil || claimed == nil || claimed.ID != otherFailedID {
		t.Fatalf("first claim = %+v, %v", claimed, err)
	}
	if err := repo.MarkQueueFailed(claimed.ID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimNextQueueItem()
	if err != nil || claimed == nil || claimed.ID != failedID {
		t.Fatalf("second claim = %+v, %v", claimed, err)
	}
	if err := repo.MarkQueueFailed(claimed.ID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimNextQueueItem()
	if err != nil || claimed == nil || claimed.ID != pendingID {
		t.Fatalf("third claim = %+v, %v", claimed, err)
	}
	if err := repo.ReleaseQueueItem(claimed.ID); err != nil {
		t.Fatal(err)
	}

	ids, err := repo.RetryFailedQueueItemsBySource("mangadex")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != failedID {
		t.Fatalf("retried ids = %v, want [%d]", ids, failedID)
	}
	item, err := repo.GetQueueItem(failedID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != QueuePending {
		t.Fatalf("retried item status = %q", item.Status)
	}

	// Rows of the other source and rows that were not failed keep their
	// state, and an empty match reports nothing rather than an error.
	if item, err := repo.GetQueueItem(otherFailedID); err != nil || item.Status != QueueFailed {
		t.Fatalf("other source item = %+v, %v", item, err)
	}
	if item, err := repo.GetQueueItem(pendingID); err != nil || item.Status != QueuePending {
		t.Fatalf("pending item = %+v, %v", item, err)
	}
	ids, err = repo.RetryFailedQueueItemsBySource("mangadex")
	if err != nil || len(ids) != 0 {
		t.Fatalf("second retry = %v, %v", ids, err)
	}
}

// queueItemForSource seeds one chapter of one source and returns the queue
// item id it produced.
func queueItemForSource(t *testing.T, repo *Repository, sourceID, sourceMangaID, sourceChapterID string) int64 {
	t.Helper()
	manga, err := repo.UpsertManga(Manga{
		SourceID: sourceID, SourceMangaID: sourceMangaID, Title: "Bulk Retry",
		Status: "ongoing", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: sourceChapterID})
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.EnqueueChapter(chapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}
