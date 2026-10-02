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
	//
	// It is deliberately long. Guard interstitials take seconds to appear and
	// resolve, and the measured time for a site to hand over material runs to
	// several seconds; bailing sooner than the site needs concludes a site needs no
	// check while it is still deciding to issue one.
	settleDelay = 15 * time.Second
	// agentGrace bounds the wait for the page to report the identity the clearance
	// was issued to. It is short because the cookie is usually accompanied by a
	// report already, so the wait is only ever paid when the message is late.
	agentGrace = 3 * time.Second
	// agentPollInterval is how often that wait re-reads the latest report.
	agentPollInterval = 100 * time.Millisecond
	// jarQuietPeriod is how long the jar must be still before it is taken as the
	// whole answer. A guard that sets an identifier and then a granting cookie a
	// moment later would otherwise have the first of the two mistaken for the
	// result.
	jarQuietPeriod = 4 * time.Second
)

// jarChanged reports whether the jar moved since an earlier read. Both a new
// name and a rewritten value count, because a guard may issue a value in stages
// under one name.
//
// Values are compared rather than only names so that a guard which sets its
// material early and rewrites it later is still seen to be working.
func jarChanged(before, after map[string]string) bool {
	if len(before) != len(after) {
		return true
	}
	for name, value := range after {
		if previous, existed := before[name]; !existed || previous != value {
			return true
		}
	}
	return false
}

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

	// The jar is read before the site is asked anything, so a cookie the site
	// issues in response can be told apart from one already on the profile. A
	// clearance cookie left over from an earlier run would otherwise look like a
	// fresh answer and end the attempt before the challenge had been seen.
	baseline, err := s.readCookies(ctx, origin)
	if err != nil {
		return nil, err
	}

	if err := s.beginSolve(origin, sink); err != nil {
		return nil, err
	}

	start := time.Now()
	ticker := time.NewTicker(solvePollInterval)
	defer ticker.Stop()

	// last is the most recent jar, quietSince marks when it last moved, and
	// settled says the jar has been still long enough to be the whole answer.
	last := baseline
	var quietSince time.Time
	settled := false

	for {
		state := sink.current()

		snapshot, err := s.readCookies(ctx, origin)
		if err != nil {
			return nil, err
		}
		// A guard issues material in steps: an identifier first, then the cookie
		// that actually grants access. The first new name is not the answer, it is
		// the answer starting, so polling continues until the jar stops changing
		// and the whole sequence is collected rather than whichever cookie landed
		// first.
		moved := jarChanged(last.jar, snapshot.jar)
		last = snapshot
		if moved {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			settled = time.Since(quietSince) > jarQuietPeriod
		}
		if jarChanged(baseline.jar, last.jar) && settled {
			if state.UserAgent == "" {
				if waited := sink.waitForAgent(ctx, agentGrace); waited != nil {
					state = *waited
				}
			}
			return &Result{
				Captured: true,
				Capture: Capture{
					Origin:    origin,
					Cookies:   last.jar,
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
