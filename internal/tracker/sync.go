package tracker

import (
	"context"
	"errors"
	"fmt"
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
		if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
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
		_ = w.Repo.FailTrackerSync(job.ID, false, err.Error())
		return true, nil
	}
	provider, ok := w.Registry.Get(binding.TrackerType)
	if !ok {
		_ = w.Repo.FailTrackerSync(job.ID, false, "unknown tracker: "+binding.TrackerType)
		return true, nil
	}
	if !provider.Capabilities().Scrobble {
		_ = w.Repo.FailTrackerSync(job.ID, false, ErrUnsupported.Error())
		return true, nil
	}
	cred, err := w.Registry.Credential(binding.TrackerType)
	if err != nil {
		_ = w.Repo.FailTrackerSync(job.ID, false, err.Error())
		return true, nil
	}
	err = provider.ScrobbleProgress(ctx, binding, job.ChapterNumber, cred)
	if err == nil {
		_ = w.Repo.UpdateTrackerSyncedChapter(binding.ID, job.ChapterNumber)
		return true, w.Repo.CompleteTrackerSync(job.ID)
	}
	retry := retryableTrackerError(err)
	_ = w.Repo.FailTrackerSync(job.ID, retry, err.Error())
	return true, nil
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
