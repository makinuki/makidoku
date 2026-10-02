// Package solver presents an anti-bot challenge in an embedded browser and reads
// the clearance material the site issues once it is answered.
//
// The browser is a solver, not a content path. It opens only in response to a
// classified challenge, and once clearance is read the ordinary fetcher takes
// over. Nothing here is on the request path for a source that is not challenged.
//
// # Threading
//
// The embedded browser is a COM component with a hard affinity requirement: it
// refuses calls on itself and on its interfaces from any thread other than the
// one that created it, and its window receives messages only on that thread. A
// goroutine may be rescheduled onto a different thread at any time, so all work
// happens on one goroutine pinned for the life of the Solver.
//
// Work is submitted to that thread rather than run on it, and the caller waits
// for the result. The distinction matters for the one operation that is
// asynchronous in the interface: the cookie read reports its answer by calling
// back into a handler that the runtime dispatches on the thread that issued the
// call. That call is therefore issued on the solver thread and awaited on the
// caller's goroutine, because a caller that blocked the solver thread would stop
// the thread responsible for producing the answer.
package solver

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ErrUnavailable reports that this build or this machine cannot present a solve
// window. It is a normal condition rather than a fault, and the caller is
// expected to fall back to asking the user for the material.
var ErrUnavailable = errors.New("the challenge solver is not available on this system")

// ClearanceName is the cookie an anti-bot interstitial issues.
const ClearanceName = "cf_clearance"

// Capture is the material read from a solved view.
//
// The whole cookie jar travels rather than only the clearance cookie, because
// companion cookies are commonly required and omitting them causes an immediate
// re-challenge. The agent and client hints must come from the same solve as the
// cookies, since a cookie is only valid for the identity that obtained it.
type Capture struct {
	// Origin is the host the material belongs to. Clearance is never valid
	// across origins, so this is part of the identity of the record.
	Origin string
	// Cookies is the jar as name to value.
	Cookies map[string]string
	// UserAgent is the agent the view reported.
	UserAgent string
	// SecChUa holds the client hint values the view reported, if any.
	SecChUa map[string]string
}

// Challenger presents an anti-bot challenge in a browser and captures the
// clearance the site issues.
//
// The API depends on this rather than on the concrete solver so that a build
// without an embedded browser still satisfies the interface, and reports itself
// unavailable instead of failing to compile.
type Challenger interface {
	// Available reports whether a solve window can be presented at all.
	Available(ctx context.Context) error
	// Solve presents origin in a visible window and returns what was captured.
	//
	// The window is always shown. A site may clear its own challenge or may wait
	// for a click, and both are ordinary outcomes, so whether the caller opened
	// this window by itself or because the user asked for it does not change what
	// happens next.
	Solve(ctx context.Context, origin string) (*Result, error)
}

// Result reports what one solve produced.
type Result struct {
	// Captured reports that clearance material was read. It says nothing about
	// whether the site will now serve the request that was blocked, which is
	// decided by retrying that request.
	Captured bool
	// Capture holds the material when Captured is true.
	Capture Capture
	// NeedsInteraction reports that a challenge is on screen and has not cleared
	// on its own. This is an ordinary state rather than a failure: some challenges
	// wait for a click.
	NeedsInteraction bool
}

// Self-check bounds. The check runs at start-up, so it cannot be slow, and a
// wedged browser has to report rather than hang.
const (
	checkPollInterval = 250 * time.Millisecond
	checkTimeout      = 20 * time.Second
)

// checkOrigin is the origin the self check writes to. The reserved invalid TLD
// cannot resolve, so nothing leaves the machine and no real site is touched.
const checkOrigin = "https://makidoku.invalid"

// threadHost is the platform part of the solver thread: the window whose message
// queue the browser is driven from.
//
// It is an interface so the Solver compiles on platforms with no embedded
// browser. Those build a host that reports nothing is available.
type threadHost interface {
	// run pumps messages until the thread is asked to stop. It occupies the
	// solver thread, which is why work is posted rather than run directly.
	run()
	// runTask runs a closure on the thread and waits for it to finish.
	runTask(func())
	// postClose asks the thread to stop.
	postClose()
}

// Solver owns the thread the embedded browser lives on.
//
// The window and its message pump exist for the life of the solver. The browser
// view inside it is embedded and released per solve, so no window is created and
// destroyed per attempt, which also means the window class is registered once.
type Solver struct {
	profileDir string

	// window is set by the solver thread once it exists. It is read by callers
	// that post work, which is safe because it is only written before the pump
	// starts and never changes afterwards.
	window threadHost

	// viewHost is the window currently holding a browser, when a solve has one
	// open. It is created per solve and destroyed with it, and is only ever
	// touched on the solver thread.
	viewHost *hostWindow

	// stopped is closed once the solver thread has finished.
	stopped chan struct{}
	// ready is closed once the window exists and the pump is about to run. Work
	// submitted before that would otherwise wait on a window that does not exist
	// yet, which looks like a hang rather than a startup race.
	ready chan struct{}
	// start brings the thread up on first use. Constructing a solver does no work
	// by itself, so a daemon that never meets a challenge creates no window and no
	// thread, and a program that only reads configuration pays nothing for the
	// capability.
	start   sync.Once
	started atomic.Bool
	closed  sync.Once

	// initErr records a failure to prepare the thread, so a caller learns about
	// it instead of waiting for a browser that will never arrive.
	initMu  sync.Mutex
	initErr error
}

// New creates a solver. The profile directory holds the browser user data and is
// reused across solves, so clearance survives a restart.
//
// The browser thread starts on first use rather than here.
func New(profileDir string) *Solver {
	return &Solver{
		profileDir: profileDir,
		stopped:    make(chan struct{}),
		ready:      make(chan struct{}),
	}
}

// ensure brings the solver thread up and waits until it is either ready or has
// failed to start.
func (s *Solver) ensure() error {
	s.start.Do(func() {
		s.started.Store(true)
		go s.run()
	})
	select {
	case <-s.ready:
		return nil
	case <-s.stopped:
		if err := s.threadInitError(); err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return ErrUnavailable
	}
}

func (s *Solver) threadInitError() error {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	return s.initErr
}

// run is the solver thread.
//
// It is pinned because window messages and COM both belong to the thread that
// created them, and it then serves as the window message pump for the life of the
// solver. Messages posted to a window are delivered to the queue of the thread
// that owns the window, so the pump must be that thread; a pump on any other
// thread receives nothing and the view stops answering.
//
// Because the pump occupies this thread, work reaches the browser by being posted
// to the window rather than handed over on a channel.
func (s *Solver) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.stopped)

	if err := initThread(); err != nil {
		s.setInitError(err)
		return
	}
	thread, err := startThread()
	if err != nil {
		s.setInitError(err)
		return
	}
	s.window = thread
	// The thread and its apartment exist but the loop has not entered yet. Work is
	// queued as messages, so releasing callers here is safe: the loop will drain
	// them.
	close(s.ready)

	thread.run()
}

func (s *Solver) setInitError(err error) {
	s.initMu.Lock()
	s.initErr = err
	s.initMu.Unlock()
}

// onThread runs a closure on the solver thread and waits for it to finish.
//
// The closure must not wait on the thread's own message pump. Anything that
// completes asynchronously has to be issued here and awaited by the caller, or
// the thread responsible for producing the answer is the thread being blocked.
func (s *Solver) onThread(fn func()) {
	// Waiting for readiness first means a caller that arrives during start-up waits
	// for the thread rather than posting to one that is not running.
	select {
	case <-s.ready:
	case <-s.stopped:
		return
	}
	if s.window == nil {
		return
	}
	s.window.runTask(fn)
}

// onSolver runs a closure on the solver thread and reports its error.
func (s *Solver) onSolver(fn func() error) error {
	var err error
	s.onThread(func() { err = fn() })
	return err
}

// Close stops the solver thread and releases the browser.
//
// A solver that was never used has no thread to stop, and this returns at once
// rather than waiting for one that will never arrive.
func (s *Solver) Close() error {
	var err error
	s.closed.Do(func() {
		if !s.started.Load() {
			return
		}
		if s.window != nil {
			s.onThread(func() { err = teardown() })
			s.window.postClose()
		}
		<-s.stopped
	})
	return err
}

// Available reports whether a solve window can be presented on this machine. A
// missing browser runtime is reported as ErrUnavailable, so a caller can offer
// the manual route rather than reporting a fault.
func (s *Solver) Available(ctx context.Context) error {
	if err := platformSupport(); err != nil {
		return err
	}
	if err := s.ensure(); err != nil {
		return err
	}
	var err error
	s.onThread(func() { err = runtimePresent() })
	return err
}

// SelfCheck exercises the parts of the solver that need no network: the browser
// embeds, the cookie manager is reachable, a cookie written through it reads back
// with its flags intact, and injected script runs.
//
// It exists because the failure mode of this component is a process fault rather
// than an error. A dependency change that alters the interface layout would
// otherwise appear as a crash during normal use instead of a failed check.
func (s *Solver) SelfCheck(ctx context.Context) error {
	if err := s.Available(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	if err := s.openView(ctx, false); err != nil {
		return err
	}
	defer s.closeView(ctx)

	if err := s.writeCheckCookie(ctx); err != nil {
		return err
	}
	snapshot, err := s.awaitCheckCookie(ctx)
	if err != nil {
		return err
	}
	if snapshot.httpOnly == nil {
		return fmt.Errorf("self check: the cookie came back without its flags")
	}
	return s.checkScriptInjection(ctx)
}

// awaitCheckCookie polls until the probe cookie is readable, then returns the
// snapshot. Each read is issued on the solver thread and awaited here, which is
// the only arrangement that does not deadlock against the completion handler.
func (s *Solver) awaitCheckCookie(ctx context.Context) (cookieSnapshot, error) {
	ticker := time.NewTicker(checkPollInterval)
	defer ticker.Stop()
	for {
		snapshot, err := s.readCookies(ctx, checkOrigin)
		if err != nil {
			return cookieSnapshot{}, err
		}
		if _, ok := snapshot.jar[ClearanceName]; ok && snapshot.httpOnly != nil {
			return snapshot, nil
		}
		select {
		case <-ctx.Done():
			return cookieSnapshot{}, fmt.Errorf("self check: a cookie written through the manager was not readable back")
		case <-ticker.C:
		}
	}
}
