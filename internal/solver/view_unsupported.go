//go:build !windows

package solver

import (
	"context"
	"fmt"
)

// This build has no embedded browser, so solving is reported as unavailable
// rather than attempted. The caller falls back to asking the user for the
// material, which is a supported path rather than a degraded one.
//
// Only Windows is implemented. The other desktop platforms need a system web view
// reached through cgo, which cannot be built or tested from the machine this was
// developed on, and a clearance cookie solved on one is bound to that engine's
// transport rather than to the Go replay.

// platformSupport reports that this build cannot present a solve window.
func platformSupport() error {
	return fmt.Errorf("%w: no embedded browser on this platform", ErrUnavailable)
}

// runtimePresent has nothing to inspect on a platform with no browser.
func runtimePresent() error { return platformSupport() }

// initThread has nothing to prepare.
func initThread() error { return nil }

// teardown has nothing to release.
func teardown() error { return nil }

// openView is never reached, because Availability reports unsupported first.
func (s *Solver) openView(ctx context.Context, show bool) error { return platformSupport() }

// closeView is a no-op.
func (s *Solver) closeView(ctx context.Context) {}

// readCookies is never reached.
func (s *Solver) readCookies(ctx context.Context, origin string) (cookieSnapshot, error) {
	return cookieSnapshot{}, platformSupport()
}

// writeCheckCookie is never reached.
func (s *Solver) writeCheckCookie(ctx context.Context) error { return platformSupport() }

// checkScriptInjection is never reached.
func (s *Solver) checkScriptInjection(ctx context.Context) error { return platformSupport() }

// cookieSnapshot is the platform-independent shape of a jar read. The Windows
// build defines the same type with the extra flag the self check inspects, so
// this exists only to keep the unsupported build compiling.
type cookieSnapshot struct {
	jar      map[string]string
	httpOnly *bool
}

// beginSolve is never reached, because Availability reports unsupported first.
func (s *Solver) beginSolve(origin string, sink *reports) error { return platformSupport() }

// hostWindow is the window the browser is embedded in. This build has no
// browser, so the type exists only to satisfy the field the shared solver holds.
// Nothing constructs one, and the methods below are the whole of its surface.
type hostWindow struct{}

// startThread has nothing to build on a platform with no browser, and reports
// that so the solver thread stops instead of pretending to be ready.
func startThread() (threadHost, error) { return nil, platformSupport() }
