package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
)

type fakeDownloadRunner struct {
	mangaID   string
	selection downloader.ChapterSelection
	format    string
	drained   bool
	drainErr  error
}

func (f *fakeDownloadRunner) EnqueueManga(ctx context.Context, mangaID string, selection downloader.ChapterSelection, format string) ([]db.DownloadQueueItem, error) {
	f.mangaID, f.selection, f.format = mangaID, selection, format
	return []db.DownloadQueueItem{
		{DownloadQueue: db.DownloadQueue{ID: 1, Status: db.QueueCompleted}},
		{DownloadQueue: db.DownloadQueue{ID: 2, Status: db.QueueFailed}},
	}, nil
}

func (f *fakeDownloadRunner) Drain(ctx context.Context) error {
	f.drained = true
	return f.drainErr
}

func (f *fakeDownloadRunner) List() ([]db.DownloadQueueItem, error) {
	return []db.DownloadQueueItem{
		{DownloadQueue: db.DownloadQueue{ID: 1, Status: db.QueueCompleted}},
		{DownloadQueue: db.DownloadQueue{ID: 2, Status: db.QueueFailed}},
	}, nil
}

func TestExecuteDownloadEnqueuesRangeAndDrains(t *testing.T) {
	runner := &fakeDownloadRunner{}
	count, err := executeDownload(context.Background(), runner, "mangadex:title-id", "1-50", downloader.FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || !runner.drained {
		t.Fatalf("count = %d, drained = %v", count, runner.drained)
	}
	if runner.mangaID != "mangadex:title-id" || runner.selection.Range != "1-50" || runner.format != downloader.FormatCBZ {
		t.Fatalf("enqueue = %q, %+v, %q", runner.mangaID, runner.selection, runner.format)
	}
}

// A failed drain must report only the chapters that actually finished.
func TestExecuteDownloadCountsCompletedOnFailure(t *testing.T) {
	runner := &fakeDownloadRunner{drainErr: errors.New("interrupted")}
	count, err := executeDownload(context.Background(), runner, "mangadex:title-id", "", downloader.FormatCBZ)
	if err == nil {
		t.Fatal("expected the drain error to propagate")
	}
	if count != 1 {
		t.Fatalf("count = %d, want only the completed chapter", count)
	}
}
