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
	completed int
	drainErr  error
}

func (f *fakeDownloadRunner) EnqueueManga(ctx context.Context, mangaID string, selection downloader.ChapterSelection, format string) ([]db.DownloadQueueItem, error) {
	f.mangaID, f.selection, f.format = mangaID, selection, format
	return []db.DownloadQueueItem{
		{DownloadQueue: db.DownloadQueue{ID: 1}},
		{DownloadQueue: db.DownloadQueue{ID: 2}},
	}, nil
}

func (f *fakeDownloadRunner) Drain(ctx context.Context) (int, error) {
	f.drained = true
	return f.completed, f.drainErr
}

func TestExecuteDownloadEnqueuesRangeAndDrains(t *testing.T) {
	runner := &fakeDownloadRunner{completed: 1}
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
	runner := &fakeDownloadRunner{completed: 1, drainErr: errors.New("interrupted")}
	count, err := executeDownload(context.Background(), runner, "mangadex:title-id", "", downloader.FormatCBZ)
	if err == nil {
		t.Fatal("expected the drain error to propagate")
	}
	if count != 1 {
		t.Fatalf("count = %d, want only the completed chapter", count)
	}
}
