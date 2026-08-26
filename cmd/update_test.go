package cmd

import (
	"context"
	"testing"
)

type fakeUpdateRunner struct {
	count int
}

func (f fakeUpdateRunner) Run(context.Context) (int, error) { return f.count, nil }

func TestExecuteUpdateReportsNewChapters(t *testing.T) {
	count, err := executeUpdate(context.Background(), fakeUpdateRunner{count: 4})
	if err != nil || count != 4 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
