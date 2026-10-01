package downloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

func int64Pointer(v int64) *int64 { return &v }

func TestPolicyPreferenceOrderIsOverrideThenHintThenDefault(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	eng.rateHints = engine.RateLimitHints{IntervalMs: int64Pointer(800), Burst: int64Pointer(4)}
	eng.retryHints = engine.RetryHints{MaxAttempts: int64Pointer(5), BackoffMs: int64Pointer(1500)}
	q := NewQueue(repo, eng, Options{Workers: 1, PageInterval: 500 * time.Millisecond, DownloadDir: dataDir, MaxRetries: 3})

	policy := q.policyFor(context.Background(), "mangadex")
	if policy.interval != 800*time.Millisecond || policy.retries != 5 || policy.backoff != 1500*time.Millisecond || policy.concurrent != 4 {
		t.Fatalf("hint policy = %+v", policy)
	}

	// An override replaces the hint on the fields it sets and leaves the
	// rest of the resolved policy alone. It only takes effect once the
	// cached policy is invalidated, which the API does when storing it.
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{
		SourceID: "mangadex", IntervalMs: int64Pointer(120), MaxAttempts: int64Pointer(1), Burst: int64Pointer(1),
	}); err != nil {
		t.Fatal(err)
	}
	if cached := q.policyFor(context.Background(), "mangadex"); cached.interval != 800*time.Millisecond {
		t.Fatalf("policy changed without invalidation: %+v", cached)
	}
	q.InvalidateSourcePolicy("mangadex")
	policy = q.policyFor(context.Background(), "mangadex")
	if policy.interval != 120*time.Millisecond || policy.retries != 1 || policy.backoff != 1500*time.Millisecond || policy.concurrent != 1 {
		t.Fatalf("override policy = %+v", policy)
	}

	// Clearing the override returns the source to its suggestion.
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{SourceID: "mangadex"}); err != nil {
		t.Fatal(err)
	}
	q.InvalidateSourcePolicy("mangadex")
	if policy = q.policyFor(context.Background(), "mangadex"); policy.interval != 800*time.Millisecond || policy.retries != 5 || policy.concurrent != 4 {
		t.Fatalf("policy after clearing override = %+v", policy)
	}
}

func TestPolicyOverrideAppliesWhenHintsUnavailable(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	eng.hintsErr = errors.New("plugin cannot load")
	q := NewQueue(repo, eng, Options{Workers: 1, PageInterval: 500 * time.Millisecond, DownloadDir: dataDir, MaxRetries: 3})

	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{SourceID: "mangadex", IntervalMs: int64Pointer(60)}); err != nil {
		t.Fatal(err)
	}
	policy := q.policyFor(context.Background(), "mangadex")
	if policy.interval != 60*time.Millisecond {
		t.Fatalf("policy with failing hints = %+v", policy)
	}
	// A policy resolved from failing hints is not cached, so a source that
	// starts answering again is picked up without any invalidation.
	eng.hintsErr = nil
	eng.rateHints = engine.RateLimitHints{IntervalMs: int64Pointer(900)}
	if policy = q.policyFor(context.Background(), "mangadex"); policy.interval != 60*time.Millisecond {
		t.Fatalf("override must keep winning over the revived hint: %+v", policy)
	}
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{SourceID: "mangadex"}); err != nil {
		t.Fatal(err)
	}
	q.InvalidateSourcePolicy("mangadex")
	if policy = q.policyFor(context.Background(), "mangadex"); policy.interval != 900*time.Millisecond {
		t.Fatalf("revived hint policy = %+v", policy)
	}
}

func TestQueueDefaultsReportResolvedGlobalPolicy(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	q := NewQueue(repo, queueFixture(), Options{Workers: 1, PageInterval: 750 * time.Millisecond, DownloadDir: dataDir, MaxRetries: 0})
	interval, attempts, backoff := q.Defaults()
	if interval != 750*time.Millisecond || attempts != 0 || backoff != DefaultRetryBackoff {
		t.Fatalf("defaults = %v, %d, %v", interval, attempts, backoff)
	}
}

// Pausing one source releases its in-flight chapter and stops further claims
// from it, while a resume drains it to completion. The staged progress of the
// released chapter carries over, exactly like the downloader-wide pause.
func TestQueuePauseSourceReleasesInFlightItem(t *testing.T) {
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
	eng.afterFirstFetch = func() { queue.PauseSource("mangadex") }
	if finished, err := queue.Drain(context.Background()); err != nil || finished != 0 {
		t.Fatalf("drain while paused: %v (finished %d)", err, finished)
	}
	stored, err := repo.GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.QueuePending || stored.TotalPages != 2 {
		t.Fatalf("released item = %+v", stored)
	}
	if paused := queue.PausedSources(); len(paused) != 1 || paused[0] != "mangadex" {
		t.Fatalf("paused sources = %v", paused)
	}

	queue.ResumeSource("mangadex")
	if paused := queue.PausedSources(); len(paused) != 0 {
		t.Fatalf("paused sources after resume = %v", paused)
	}
	if finished, err := queue.Drain(context.Background()); err != nil || finished != 1 {
		t.Fatalf("resume drain: %v (finished %d)", err, finished)
	}
	chapter, err := repo.GetChapter(items[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if !chapter.Downloaded || chapter.DownloadPath == nil {
		t.Fatalf("chapter after resume = %+v", chapter)
	}
}

// The claim gates resolve paused sources plus each live source's chapter cap,
// with the burst hint in play and a stored override winning over it.
func TestClaimOptionsResolveCapsAndPauses(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	eng.rateHints = engine.RateLimitHints{Burst: int64Pointer(1)}
	q := NewQueue(repo, eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 0,
		MaxActiveSources: 2, ChaptersPerSource: 3,
	})
	seedChapterQueueItem(t, repo)

	options := q.claimOptions(context.Background())
	if options.DefaultCap != 3 || options.MaxActiveSources != 2 {
		t.Fatalf("global gates = %+v", options)
	}
	if len(options.Caps) != 1 || options.Caps[0].SourceID != "mangadex" || options.Caps[0].Max != 1 {
		t.Fatalf("resolved caps = %+v", options.Caps)
	}

	// A stored override replaces the hint in the resolved caps.
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{SourceID: "mangadex", Burst: int64Pointer(4)}); err != nil {
		t.Fatal(err)
	}
	q.InvalidateSourcePolicy("mangadex")
	options = q.claimOptions(context.Background())
	if len(options.Caps) != 1 || options.Caps[0].Max != 4 {
		t.Fatalf("override caps = %+v", options.Caps)
	}

	// A paused source is reported so the claim skips it, and a source whose
	// cap equals the default needs no caps entry.
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{SourceID: "mangadex"}); err != nil {
		t.Fatal(err)
	}
	q.InvalidateSourcePolicy("mangadex")
	q.PauseSource("mangadex")
	options = q.claimOptions(context.Background())
	if len(options.PausedSources) != 1 || options.PausedSources[0] != "mangadex" {
		t.Fatalf("paused gate = %+v", options.PausedSources)
	}
	if len(options.Caps) != 1 || options.Caps[0].Max != 1 {
		t.Fatalf("caps after clearing override should fall back to the hint: %+v", options.Caps)
	}
}

// seedChapterQueueItem stores one live queue row for the mangadex fixture so
// QueueSourceIDs reports the source.
func seedChapterQueueItem(t *testing.T, repo *db.Repository) {
	t.Helper()
	manga, err := repo.UpsertManga(db.Manga{
		SourceID: "mangadex", SourceMangaID: "claim-options", Title: "Claim Options",
		Status: "ongoing", CoverURL: "cover", DownloadFormat: "cbz",
	})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "claim-options-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueChapter(chapter.ID); err != nil {
		t.Fatal(err)
	}
}
