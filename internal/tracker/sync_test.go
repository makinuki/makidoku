package tracker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

func TestProgressEnqueuesAtNinetyPercent(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	_, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','S','1',1,'en','https://x','x',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	m, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "c"})
	if err != nil {
		t.Fatal(err)
	}
	n := 3.0
	c, err := repo.UpsertChapter(db.Chapter{MangaID: m.ID, SourceChapterID: "c", ChapterNumber: &n})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: m.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	w := &SyncWorker{Repo: repo, Registry: NewRegistry(repo)}
	if err := w.EnqueueForProgress(m.ID, c.ID, false, 89, 100); err != nil {
		t.Fatal(err)
	}
	jobs, _ := repo.ListTrackerSyncJobs()
	if len(jobs) != 0 {
		t.Fatal("89 percent enqueued a job")
	}
	if err := w.EnqueueForProgress(m.ID, c.ID, false, 90, 100); err != nil {
		t.Fatal(err)
	}
	jobs, _ = repo.ListTrackerSyncJobs()
	if len(jobs) != 1 || jobs[0].BindingID != b.ID || jobs[0].ChapterNumber != 3 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestPermanentTrackerFailureIsNotReclaimed(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	_, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s2','S','1',1,'en','https://x','x',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	m, err := repo.UpsertManga(db.Manga{SourceID: "s2", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "c"})
	if err != nil {
		t.Fatal(err)
	}
	ch := 1.0
	c, err := repo.UpsertChapter(db.Chapter{MangaID: m.ID, SourceChapterID: "c", ChapterNumber: &ch})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: m.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.EnqueueTrackerSync(m.ID, b.ID, *c.ChapterNumber)
	if err != nil {
		t.Fatal(err)
	}
	// Claim five seconds past enqueue so a second boundary between the two
	// repository calls cannot push next_attempt_at past the claim time.
	claimed, err := repo.ClaimTrackerSync(now + 5)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v, err=%v", claimed, err)
	}
	if err := repo.FailTrackerSync(job.ID, false, "bad credentials"); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimTrackerSync(now + 105)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("permanent failure was reclaimed: %+v", claimed)
	}
}

func TestRetryableTrackerFailureReturnsToPending(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	_, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s3','S','1',1,'en','https://x','x',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	m, err := repo.UpsertManga(db.Manga{SourceID: "s3", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "c"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: m.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.EnqueueTrackerSync(m.ID, b.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimTrackerSync(now); err != nil {
		t.Fatal(err)
	}
	if err := repo.FailTrackerSync(job.ID, true, "rate limited"); err != nil {
		t.Fatal(err)
	}
	jobs, err := repo.ListTrackerSyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].Status != db.SyncPending || jobs[0].Attempts != 1 || jobs[0].NextAttemptAt <= now {
		t.Fatalf("retry job = %+v", jobs[0])
	}
}

func TestPrepareRequeuesInterruptedTrackerJob(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	_, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s4','S','1',1,'en','https://x','x',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s4", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "c"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueTrackerSync(manga.ID, binding.ID, 1); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimTrackerSync(now); err != nil || claimed == nil {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	worker := &SyncWorker{Repo: repo, Registry: NewRegistry(repo)}
	if err := worker.Prepare(); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimTrackerSync(time.Now().Unix())
	if err != nil || claimed == nil {
		t.Fatalf("reclaim=%+v err=%v", claimed, err)
	}
}

// Every outbound tracker request must carry a timeout so a hung connection
// cannot stall the sync worker or hold the refresh lock.
func TestRegistryHTTPClientHasTimeout(t *testing.T) {
	repo := trackerRepo(t)
	registry := NewRegistry(repo)
	if registry.HTTP.Timeout <= 0 {
		t.Fatalf("http client timeout = %v, want a positive bound", registry.HTTP.Timeout)
	}
}

// Missing credentials are permanent: the job fails without retry until the
// user connects the tracker. A failing token endpoint, however, must leave
// the job retryable.
func TestCredentialFailureClassification(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s6','S','1',1,'en','https://x','x',?)`, now); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s6", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "c"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueTrackerSync(manga.ID, binding.ID, 1); err != nil {
		t.Fatal(err)
	}

	worker := &SyncWorker{Repo: repo, Registry: NewRegistry(repo)}
	processed, err := worker.ProcessOne(context.Background(), "")
	if err != nil || !processed {
		t.Fatalf("processed = %v, err = %v", processed, err)
	}
	jobs, err := repo.ListTrackerSyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].Status != db.SyncFailed || jobs[0].Attempts != 1 {
		t.Fatalf("job after missing credential = %+v, want a permanent failure", jobs[0])
	}

	// Transient refresh failures classify as retryable.
	err = fmt.Errorf("OAuth token exchange failed: 503 Service Unavailable: %w", &HTTPError{Status: 503})
	if !retryableTrackerError(err) {
		t.Fatal("a failing token endpoint must count as retryable")
	}
	if retryableTrackerError(ErrCredentialMissing) {
		t.Fatal("missing credentials must never be retryable")
	}
}

func TestRetryableTrackerErrorClassifiesTransientFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "rate limit", err: &HTTPError{Status: 429}, want: true},
		{name: "upstream", err: &HTTPError{Status: 503}, want: true},
		{name: "unauthorized", err: &HTTPError{Status: 401}, want: false},
		{name: "network", err: &net.DNSError{Err: "temporary", IsTemporary: true}, want: true},
		{name: "canceled", err: context.Canceled, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableTrackerError(tc.err); got != tc.want {
				t.Fatalf("retryable=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestSyncWorkerRunStopsCleanlyOnCancellation(t *testing.T) {
	repo := trackerRepo(t)
	worker := &SyncWorker{Repo: repo, Registry: NewRegistry(repo)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v", err)
	}
}

// Bindings on providers without scrobble support must not create sync jobs:
// every reading session would otherwise queue a guaranteed failure.
func TestProgressSkipsNonScrobbleTrackers(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	_, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('sk','S','1',1,'en','https://x','x',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "sk", SourceMangaID: "m", Title: "M", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapterNumber := 2.0
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "c", ChapterNumber: &chapterNumber})
	if err != nil {
		t.Fatal(err)
	}
	kitsu, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "kitsu", RemoteID: "9", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	anilist, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "8", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}

	w := &SyncWorker{Repo: repo, Registry: NewRegistry(repo)}
	if err := w.EnqueueForProgress(manga.ID, chapter.ID, true, 10, 10); err != nil {
		t.Fatal(err)
	}
	jobs, err := repo.ListTrackerSyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].BindingID != anilist.ID {
		t.Fatalf("jobs = %+v, want exactly the anilist binding %d", jobs, anilist.ID)
	}
	_ = kitsu
}

// A storage failure while claiming work must not end the sync loop: the
// daemon keeps running and polling until it is cancelled.
func TestSyncWorkerSurvivesClaimFailures(t *testing.T) {
	repo := trackerRepo(t)
	now := time.Now().Unix()
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s5','S','1',1,'en','https://x','x',?)`, now); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s5", SourceMangaID: "m", Title: "M", Status: "ongoing", CoverURL: "c"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "unknown-tracker", RemoteID: "1", RemoteTitle: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueTrackerSync(manga.ID, binding.ID, 1); err != nil {
		t.Fatal(err)
	}

	worker := &SyncWorker{Repo: repo, Registry: NewRegistry(repo), Interval: 25 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	go func() { errs <- worker.Run(ctx) }()

	// Wait until the worker processed the seeded job once, proving the loop
	// is up and polling before the store breaks underneath it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		jobs, listErr := repo.ListTrackerSyncJobs()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(jobs) == 1 && jobs[0].Status == db.SyncFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker never processed the seeded job: %+v", jobs)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := repo.DB().Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		t.Fatalf("worker exited on a claim failure: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
