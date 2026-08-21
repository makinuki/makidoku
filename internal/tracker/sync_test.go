package tracker

import (
	"context"
	"errors"
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
	claimed, err := repo.ClaimTrackerSync(now)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %+v, err=%v", claimed, err)
	}
	if err := repo.FailTrackerSync(job.ID, false, "bad credentials"); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimTrackerSync(now + 100)
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
