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
	burst := int64(2)
	if err := repo.SetSourceDownloadPrefs(SourceDownloadPrefs{SourceID: "mangadex", IntervalMs: &interval, MaxAttempts: &attempts, Burst: &burst}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSourceDownloadPrefs("mangadex")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.IntervalMs == nil || *got.IntervalMs != 120 || got.MaxAttempts == nil || *got.MaxAttempts != 1 ||
		got.Burst == nil || *got.Burst != 2 || got.BackoffMs != nil {
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
	if got.IntervalMs != nil || got.MaxAttempts != nil || got.Burst != nil || got.BackoffMs == nil || *got.BackoffMs != 2000 {
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
	claimed, err := repo.ClaimNextQueueItem(ClaimOptions{})
	if err != nil || claimed == nil || claimed.ID != otherFailedID {
		t.Fatalf("first claim = %+v, %v", claimed, err)
	}
	if err := repo.MarkQueueFailed(claimed.ID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimNextQueueItem(ClaimOptions{})
	if err != nil || claimed == nil || claimed.ID != failedID {
		t.Fatalf("second claim = %+v, %v", claimed, err)
	}
	if err := repo.MarkQueueFailed(claimed.ID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimNextQueueItem(ClaimOptions{})
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

// gateFixture seeds three chapters of mangadex followed by one of other, so
// the queue order always starts with mangadex work.
func gateFixture(t *testing.T) *Repository {
	t.Helper()
	repo := prefsRepository(t)
	queueItemForSource(t, repo, "mangadex", "gate-a", "a1")
	queueItemForSource(t, repo, "mangadex", "gate-a", "a2")
	queueItemForSource(t, repo, "mangadex", "gate-a", "a3")
	queueItemForSource(t, repo, "other", "gate-b", "b1")
	return repo
}

func claimSource(t *testing.T, repo *Repository, options ClaimOptions) string {
	t.Helper()
	item, err := repo.ClaimNextQueueItem(options)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		return ""
	}
	return item.SourceID
}

func TestClaimGatesRespectConcurrencyLimits(t *testing.T) {
	repo := gateFixture(t)

	// A per-source cap of one spreads the queue across sources: the second
	// chapter of mangadex is skipped while its first is in flight.
	options := ClaimOptions{DefaultCap: 1}
	if source := claimSource(t, repo, options); source != "mangadex" {
		t.Fatalf("first claim = %q", source)
	}
	if source := claimSource(t, repo, options); source != "other" {
		t.Fatalf("second claim = %q, want other", source)
	}
	if source := claimSource(t, repo, options); source != "" {
		t.Fatalf("third claim = %q, want none", source)
	}
}

func TestClaimGatesApplyPerSourceCaps(t *testing.T) {
	repo := gateFixture(t)

	// An explicit cap for mangadex lets it run one chapter while other keeps
	// the default cap of five, so both sources stay claimable.
	options := ClaimOptions{DefaultCap: 5, Caps: []SourceConcurrency{{SourceID: "mangadex", Max: 1}}}
	if source := claimSource(t, repo, options); source != "mangadex" {
		t.Fatalf("first claim = %q", source)
	}
	if source := claimSource(t, repo, options); source != "other" {
		t.Fatalf("second claim = %q, want other", source)
	}
	if source := claimSource(t, repo, options); source != "" {
		t.Fatalf("third claim = %q, want none", source)
	}
}

func TestClaimGatesRespectActiveSourceLimit(t *testing.T) {
	repo := gateFixture(t)

	// One active source at a time: mangadex runs its chapters in order and
	// other waits until every mangadex row has left the downloading state.
	options := ClaimOptions{DefaultCap: 5, MaxActiveSources: 1}
	claimed := []int64{}
	for _, want := range []string{"mangadex", "mangadex", "mangadex"} {
		item, err := repo.ClaimNextQueueItem(options)
		if err != nil {
			t.Fatal(err)
		}
		if item == nil || item.SourceID != want {
			t.Fatalf("claim = %+v, want %s", item, want)
		}
		claimed = append(claimed, item.ID)
	}
	if source := claimSource(t, repo, options); source != "" {
		t.Fatalf("fourth claim = %q, want none", source)
	}

	// Completing the mangadex chapters frees the active-source slot.
	for _, item := range claimed {
		full, err := repo.GetQueueItem(item)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.CompleteQueueDownload(full.ChapterID, "/tmp/chapter.cbz"); err != nil {
			t.Fatal(err)
		}
	}
	if source := claimSource(t, repo, options); source != "other" {
		t.Fatalf("claim after completing = %q, want other", source)
	}
}

func TestClaimGatesSkipPausedSources(t *testing.T) {
	repo := gateFixture(t)

	// mangadex sits first in the queue order but is paused, so the only
	// claimable row is the other-source chapter, even with generous caps.
	options := ClaimOptions{PausedSources: []string{"mangadex"}, DefaultCap: 5, MaxActiveSources: 5}
	item, err := repo.ClaimNextQueueItem(options)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.SourceID != "other" {
		t.Fatalf("claim = %+v, want the other-source chapter", item)
	}
	if err := repo.CompleteQueueDownload(item.ChapterID, "/tmp/chapter.cbz"); err != nil {
		t.Fatal(err)
	}
	// With the other chapter finished, the paused backlog is all that is
	// left: nothing else may be claimed.
	if item, err := repo.ClaimNextQueueItem(options); err != nil || item != nil {
		t.Fatalf("claim after finishing = %+v, %v", item, err)
	}
}

func TestQueueSourceIDsListLiveSources(t *testing.T) {
	repo := gateFixture(t)
	ids, err := repo.QueueSourceIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "mangadex" || ids[1] != "other" {
		t.Fatalf("live sources = %v", ids)
	}
}
