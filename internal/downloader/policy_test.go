package downloader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

func int64Pointer(v int64) *int64 { return &v }

func TestPolicyPreferenceOrderIsOverrideThenHintThenDefault(t *testing.T) {
	repo, dataDir := downloaderRepository(t)
	eng := queueFixture()
	eng.rateHints = engine.RateLimitHints{IntervalMs: int64Pointer(800)}
	eng.retryHints = engine.RetryHints{MaxAttempts: int64Pointer(5), BackoffMs: int64Pointer(1500)}
	q := NewQueue(repo, eng, Options{Workers: 1, PageInterval: 500 * time.Millisecond, DownloadDir: dataDir, MaxRetries: 3})

	policy := q.policyFor(context.Background(), "mangadex")
	if policy.interval != 800*time.Millisecond || policy.retries != 5 || policy.backoff != 1500*time.Millisecond {
		t.Fatalf("hint policy = %+v", policy)
	}

	// An override replaces the hint on the fields it sets and leaves the
	// rest of the resolved policy alone. It only takes effect once the
	// cached policy is invalidated, which the API does when storing it.
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{
		SourceID: "mangadex", IntervalMs: int64Pointer(120), MaxAttempts: int64Pointer(1),
	}); err != nil {
		t.Fatal(err)
	}
	if cached := q.policyFor(context.Background(), "mangadex"); cached.interval != 800*time.Millisecond {
		t.Fatalf("policy changed without invalidation: %+v", cached)
	}
	q.InvalidateSourcePolicy("mangadex")
	policy = q.policyFor(context.Background(), "mangadex")
	if policy.interval != 120*time.Millisecond || policy.retries != 1 || policy.backoff != 1500*time.Millisecond {
		t.Fatalf("override policy = %+v", policy)
	}

	// Clearing the override returns the source to its suggestion.
	if err := repo.SetSourceDownloadPrefs(db.SourceDownloadPrefs{SourceID: "mangadex"}); err != nil {
		t.Fatal(err)
	}
	q.InvalidateSourcePolicy("mangadex")
	if policy = q.policyFor(context.Background(), "mangadex"); policy.interval != 800*time.Millisecond || policy.retries != 5 {
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
