package updater

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type Refresh func(context.Context, string) ([]string, error)
type Enqueue func(context.Context, string, []string) error

type Service struct {
	repo    *db.Repository
	refresh Refresh
	enqueue Enqueue
}

func (s *Service) RunTicker(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("library updater failed", "err", err)
			}
		}
	}
}

func New(repo *db.Repository, refresh Refresh) *Service {
	return &Service{repo: repo, refresh: refresh}
}

func (s *Service) SetEnqueue(enqueue Enqueue) { s.enqueue = enqueue }

func (s *Service) Run(ctx context.Context) (int, error) {
	if err := s.repo.SetLibraryUpdateState("running", time.Now().Unix()); err != nil {
		return 0, err
	}
	var mangaIDs []string
	if err := s.repo.DB().Select(&mangaIDs, `SELECT id FROM manga WHERE in_library=1 ORDER BY id`); err != nil {
		return 0, err
	}
	count := 0
	failures := 0
	existing, err := s.repo.ListUpdateLogs(true)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		seen[item.MangaID+"\x00"+item.ChapterID] = struct{}{}
	}
	for _, mangaID := range mangaIDs {
		chapters, err := s.refresh(ctx, mangaID)
		if err != nil {
			failures++
			continue
		}
		for _, chapterID := range chapters {
			key := mangaID + "\x00" + chapterID
			if _, ok := seen[key]; ok {
				continue
			}
			if _, err := s.repo.RecordUpdate(mangaID, chapterID, time.Now().Unix()); err != nil {
				_ = s.repo.SetLibraryUpdateState("failed", time.Now().Unix())
				return count, err
			}
			seen[key] = struct{}{}
			count++
		}
		if len(chapters) > 0 && s.enqueue != nil {
			if enqueueErr := s.enqueue(ctx, mangaID, chapters); enqueueErr != nil {
				failures++
			}
		}
	}
	status := "completed"
	if failures > 0 {
		status = "completed_with_errors"
	}
	if err := s.repo.SetLibraryUpdateState(status, time.Now().Unix()); err != nil {
		return count, err
	}
	return count, nil
}
