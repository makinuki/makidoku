//go:build windows

package solver

import (
	"context"
	"os"
	"testing"
	"time"
)

// The self check drives a real embedded browser against a reserved invalid host,
// which cannot resolve. It exercises the COM layout described in solver.go without
// touching the network or any third-party site, so it is the only guard that the
// hand-written layout still works after a runtime or library change.
//
// It needs an interactive session and the WebView2 runtime, so it is gated on an
// environment variable and skipped otherwise. A CI runner on Windows satisfies both.
func TestSelfCheckReportsTheBrowserIsUsable(t *testing.T) {
	requireSolverIntegration(t)

	s := New(t.TempDir())
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout+30*time.Second)
	defer cancel()

	if err := s.Available(ctx); err != nil {
		t.Fatalf("the solver reported itself unavailable on a machine with a runtime: %v", err)
	}
	if err := s.SelfCheck(ctx); err != nil {
		t.Fatalf("self check failed: %v", err)
	}
}

// requireSolverIntegration skips unless the integration gate is set. The variable
// is named for what it enables rather than for the platform, because the platform
// is already pinned by the build tag.
func requireSolverIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("MAKIDOKU_SOLVER_TEST") != "1" {
		t.Skip("set MAKIDOKU_SOLVER_TEST=1 to run the embedded browser tests")
	}
}
