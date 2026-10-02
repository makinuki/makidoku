package solver

import (
	"context"
	"errors"
	"time"
)

// DefaultSolveTimeout bounds one attempt. It is generous because a challenge that
// is waiting for a click has to stay open long enough to be answered, and the
// alternative to a long wait is a user staring at a window that closed too early.
const DefaultSolveTimeout = 3 * time.Minute

// Poll pacing for a solve. The jar read is the expensive step, so it is paced
// rather than run flat out, and the page report arrives on its own schedule.
const (
	solvePollInterval = 700 * time.Millisecond
	// settleDelay is how long a page is given before it is believed. A challenge
	// interstitial renders immediately, so substantial content this soon means the
	// site served the request rather than challenging it.
	settleDelay = 4 * time.Second
)

// ErrSolveAbandoned reports that the attempt ended without clearance. It is a
// normal outcome, not a fault: a challenge can need a click that never came, or
// the site can stop challenging and issue nothing at all.
var ErrSolveAbandoned = errors.New("the challenge was not answered")

// Solve presents origin in a visible window and returns the clearance material
// once the site issues it.
//
// The window is always shown. A site may clear the challenge on its own or may
// wait for a click, and both are ordinary outcomes; the caller decides whether
// to open the window on its own or to wait for the user to ask for it.
//
// Success is decided by the caller, not here. This returns the material and the
// state of the page, and whether the blocked request now succeeds is settled by
// retrying that request with the returned material.
func (s *Solver) Solve(ctx context.Context, origin string) (*Result, error) {
	if err := s.Available(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultSolveTimeout)
	defer cancel()

	// The page reports and the jar are both read across the poll loop, and the
	// report sink outlives the view so the caller can read the final state.
	sink := newReports()
	defer s.closeView(context.Background())

	if err := s.beginSolve(origin, sink); err != nil {
		return nil, err
	}

	start := time.Now()
	ticker := time.NewTicker(solvePollInterval)
	defer ticker.Stop()

	for {
		state := sink.current()

		snapshot, err := s.readCookies(ctx, origin)
		if err != nil {
			return nil, err
		}
		if _, ok := snapshot.jar[ClearanceName]; ok {
			return &Result{
				Captured: true,
				Capture: Capture{
					Origin:    origin,
					Cookies:   snapshot.jar,
					UserAgent: state.UserAgent,
					SecChUa:   state.ClientHints,
				},
				NeedsInteraction: state.Challenge,
			}, nil
		}

		// No cookie and no interstitial means the site served the request
		// outright. Some origins are never challenged, and a solver that kept
		// waiting on those would sit there until the timeout for no reason.
		if !state.Challenge && served(state) && time.Since(start) > settleDelay {
			return &Result{Captured: false}, nil
		}

		select {
		case <-ctx.Done():
			return &Result{Captured: false, NeedsInteraction: sink.current().Challenge}, ErrSolveAbandoned
		case <-ticker.C:
		}
	}
}

// served reports whether a document is a real page rather than an interstitial.
// A challenge page carries almost no text and no links, so volume separates the
// two even when the challenge is one the markers do not recognize.
func served(state pageState) bool {
	return !state.Challenge && state.Links >= 25 && state.Text >= 1500
}
