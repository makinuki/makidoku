package cmd

import (
	"context"
	"testing"
)

type fakeSyncRunner struct {
	tracker string
	count   int
}

func (f *fakeSyncRunner) DrainDue(_ context.Context, tracker string) (int, error) {
	f.tracker = tracker
	return f.count, nil
}

func TestExecuteSyncFiltersAndDrainsDueJobs(t *testing.T) {
	runner := &fakeSyncRunner{count: 3}
	count, err := executeSync(context.Background(), runner, "anilist")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 || runner.tracker != "anilist" {
		t.Fatalf("count=%d tracker=%q", count, runner.tracker)
	}
}

func TestExecuteSyncAcceptsMALAlias(t *testing.T) {
	runner := &fakeSyncRunner{}
	if _, err := executeSync(context.Background(), runner, "mal"); err != nil {
		t.Fatal(err)
	}
	if runner.tracker != "myanimelist" {
		t.Fatalf("tracker = %q", runner.tracker)
	}
}
