package tracker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type SyncWorker struct {
	Repo     *db.Repository
	Registry *Registry
	Interval time.Duration
}

// syncJobHistoryLimit caps how many finished sync jobs are retained.
const syncJobHistoryLimit = 200

func (w *SyncWorker) Prepare() error {
	return w.Repo.ResetInterruptedTrackerSync()
}

func (w *SyncWorker) Run(ctx context.Context) error {
	if err := w.Prepare(); err != nil {
		return err
	}
	if w.Interval <= 0 {
		w.Interval = 2 * time.Second
	}
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		// A failed poll, claim, or bookkeeping write must not stop the
		// worker: a transient storage error would otherwise take the whole
		// daemon down. Only cancellation ends the loop.
		if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("tracker sync: %v", err)
		}
		if _, err := w.Repo.PruneTerminalTrackerSyncJobs(syncJobHistoryLimit); err != nil {
			log.Printf("tracker sync: pruning history failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *SyncWorker) RunOnce(ctx context.Context) error {
	_, err := w.ProcessOne(ctx, "")
	return err
}

func (w *SyncWorker) RunOnceForTracker(ctx context.Context, trackerType string) error {
	_, err := w.ProcessOne(ctx, trackerType)
	return err
}

func (w *SyncWorker) ProcessOne(ctx context.Context, trackerType string) (bool, error) {
	var job *db.TrackerSyncJob
	var err error
	if trackerType == "" {
		job, err = w.Repo.ClaimTrackerSync(time.Now().Unix())
	} else {
		job, err = w.Repo.ClaimTrackerSyncForTracker(time.Now().Unix(), trackerType)
	}
	if err != nil || job == nil {
		return false, err
	}
	binding, err := w.Repo.GetTrackerBindingByID(job.BindingID)
	if err != nil {
		w.failJob(job.ID, false, err.Error())
		return true, nil
	}
	provider, ok := w.Registry.Get(binding.TrackerType)
	if !ok {
		w.failJob(job.ID, false, "unknown tracker: "+binding.TrackerType)
		return true, nil
	}
	cred, err := w.Registry.Credential(binding.TrackerType)
	if err != nil {
		// Missing credentials are permanent until the user connects the
		// tracker; every other failure (unreachable refresh endpoint,
		// storage hiccup) classifies like any other provider error.
		retry := !errors.Is(err, ErrCredentialMissing) && retryableTrackerError(err)
		w.failJob(job.ID, retry, err.Error())
		return true, nil
	}
	err = provider.UpdateTracking(ctx, binding, TrackingUpdate{Chapter: job.ChapterNumber}, cred)
	if err == nil {
		_ = w.Repo.UpdateTrackerSyncedChapter(binding.ID, job.ChapterNumber)
		return true, w.Repo.CompleteTrackerSync(job.ID)
	}
	w.failJob(job.ID, retryableTrackerError(err), err.Error())
	return true, nil
}

// failJob records a job failure and logs a failed bookkeeping write instead
// of discarding it, so a wedged RUNNING job stays visible in the logs.
func (w *SyncWorker) failJob(jobID int64, retry bool, message string) {
	if err := w.Repo.FailTrackerSync(jobID, retry, message); err != nil {
		log.Printf("tracker sync: recording failure for job %d: %v", jobID, err)
	}
}

func retryableTrackerError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == http.StatusTooManyRequests || httpErr.Status >= http.StatusInternalServerError
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func (w *SyncWorker) EnqueueForProgress(mangaID, chapterID string, completed bool, page, total int) error {
	if total < 1 || page < 1 || page > total {
		return fmt.Errorf("invalid reading progress")
	}
	if !completed && page*100 < total*90 {
		return nil
	}
	chapter, err := w.Repo.GetChapter(chapterID)
	if err != nil {
		return err
	}
	if chapter.ChapterNumber == nil {
		return nil
	}
	bindings, err := w.Repo.ListTrackerBindings(mangaID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if _, err := w.Repo.EnqueueTrackerSync(mangaID, binding.ID, *chapter.ChapterNumber); err != nil {
			return err
		}
	}
	return nil
}
