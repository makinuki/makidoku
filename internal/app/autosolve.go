package app

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/makinuki/makidoku/internal/config"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/solver"
)

// autoSolver answers a classified challenge without being asked, when the
// auto-solve toggle is on.
//
// It is the same window, the same capture path and the same outcome handling as
// the browse grid button; only who initiates differs. The reader's manual press
// goes through the API and is never rationed by the budget here.
type autoSolver struct {
	challenger solver.Challenger
	engine     *engine.Engine
	budget     *AttemptBudget

	// inFlight keeps one window per origin at a time. Without it two requests
	// blocked on the same origin would each open a window.
	mu       sync.Mutex
	inFlight map[string]bool
	// on is read on the request path and written when the reader flips the
	// preference, so it is read atomically. The hook is registered whatever this
	// says, because a toggle that took effect only on the next start would be
	// useless to someone who just answered a challenge.
	on atomic.Bool
}

// SetEnabled turns the automatic path on or off while the daemon is running.
func (a *autoSolver) SetEnabled(enabled bool) {
	if a == nil {
		return
	}
	a.on.Store(enabled)
	if enabled {
		slog.Info("auto-solve is on, a challenge will open a window without a button press",
			"attemptsPerOrigin", autoSolveAttemptsPerOrigin)
		return
	}
	slog.Info("auto-solve is off, a challenge will wait for the button again")
}

// startAutoSolve registers the challenge hook when a browser is available, and
// returns nil when there is none. Whether the automatic path acts is decided per
// challenge by the current setting, so the toggle applies without a restart.
func startAutoSolve(cfg config.Config, eng *engine.Engine, challenger solver.Challenger, broker *engine.ClearanceBroker) *autoSolver {
	if challenger == nil {
		return nil
	}
	auto := &autoSolver{
		challenger: challenger,
		engine:     eng,
		budget:     NewAttemptBudget(autoSolveAttemptsPerOrigin),
		inFlight:   make(map[string]bool),
	}
	broker.SetChallengeHook(auto.onChallenged)
	auto.on.Store(cfg.AutoSolve)
	if cfg.AutoSolve {
		slog.Info("auto-solve is on, a challenge will open a window without a button press",
			"attemptsPerOrigin", autoSolveAttemptsPerOrigin)
	}
	return auto
}

// autoSolveAttemptsPerOrigin is the allowance the automatic path has per origin
// for the life of the daemon. Three tolerates a site that is merely fussy today
// without letting a hopeless one interrupt the reader repeatedly.
const autoSolveAttemptsPerOrigin = 3

// autoSolveSettingKey is the stored preference the reader flips. It matches the
// key the settings API writes, so a choice made in the interface survives a
// restart and is applied without one.
const autoSolveSettingKey = "anti_bot.auto_solve"

// onChallenged runs on the request path, so it does the deciding here and the
// solving on its own goroutine. A blocked request is already parked and waiting;
// blocking it further would defeat the point.
func (a *autoSolver) onChallenged(sourceID, origin string) {
	if a == nil || origin == "" || !a.on.Load() {
		return
	}
	if !a.claim(origin) {
		return
	}
	if !a.budget.Take(origin) {
		a.release(origin)
		slog.Info("auto-solve has spent its attempts for this origin, leaving it to the reader",
			"origin", origin, "limit", a.budget.Limit())
		return
	}
	go func() {
		defer a.release(origin)
		a.solve(sourceID, origin)
	}()
}

// claim reserves an origin for one solve and reports whether it was free.
func (a *autoSolver) claim(origin string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inFlight[origin] {
		return false
	}
	a.inFlight[origin] = true
	return true
}

func (a *autoSolver) release(origin string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.inFlight, origin)
}

// solve opens the window, stores what it captured, and verifies it. A verified
// result clears the origin's allowance because the origin is demonstrably
// working again; a capture the site refused leaves the allowance spent.
func (a *autoSolver) solve(sourceID, origin string) {
	if err := a.challenger.Available(context.Background()); err != nil {
		slog.Warn("auto-solve skipped, the browser cannot be presented", "origin", origin, "err", err)
		return
	}

	slog.Info("auto-solve opening a window", "source", sourceID, "origin", origin)
	result, err := a.challenger.Solve(context.Background(), "https://"+origin+"/")
	if err != nil || result == nil {
		slog.Warn("auto-solve did not produce a result", "origin", origin, "err", err)
		return
	}
	if !result.Captured {
		slog.Info("auto-solve captured nothing", "origin", origin, "needsInteraction", result.NeedsInteraction)
		return
	}

	if err := a.engine.SubmitClearanceBundle(sourceID, db.ClearanceBundle{
		Origin:    origin,
		Cookies:   result.Capture.Cookies,
		UserAgent: result.Capture.UserAgent,
		SecChUa:   result.Capture.SecChUa,
	}); err != nil {
		slog.Warn("auto-solve could not store what it captured", "origin", origin, "err", err)
		return
	}

	verified, err := a.engine.ProbeClearance(context.Background(), sourceID, origin)
	if err != nil {
		slog.Warn("auto-solve could not verify what it captured", "origin", origin, "err", err)
		return
	}
	if verified {
		a.budget.Clear(origin)
		slog.Info("auto-solve cleared the challenge", "origin", origin, "cookies", len(result.Capture.Cookies))
		return
	}
	slog.Info("auto-solve stored material the site has not accepted yet", "origin", origin)
}

// AttemptBudget bounds how many times the daemon opens a solve window on its own
// for one origin.
//
// It exists because auto-solve opens a window without being asked, and a site
// that never clears would otherwise interrupt the reader repeatedly for no gain:
// each window fails, the next request tries again, and nothing improves.
//
// Two rules keep that bounded while leaving the reader in charge:
//
//   - Only automatic attempts are counted. A press of the browse grid button is
//     the reader deciding to try, and the manual path is the floor, so it is
//     never rationed by what the daemon has already spent.
//   - A verified solve clears the count. Verification means a request carrying
//     the material got through, so the origin is working again and a later
//     re-challenge deserves fresh attempts. A solve that captured material the
//     site refused does not clear it, because that failure should cost something.
type AttemptBudget struct {
	limit int

	mu    sync.Mutex
	spent map[string]int
}

// NewAttemptBudget returns a budget allowing limit automatic attempts per origin.
// A limit of zero or less disables automatic attempts entirely.
func NewAttemptBudget(limit int) *AttemptBudget {
	return &AttemptBudget{limit: limit, spent: make(map[string]int)}
}

// Take reserves one attempt for an origin and reports whether it was allowed.
//
// It returns false once the origin has spent its limit, so a caller must treat
// that as "do not open a window" rather than as an error.
func (b *AttemptBudget) Take(origin string) bool {
	if b == nil || b.limit <= 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent[origin] >= b.limit {
		return false
	}
	b.spent[origin]++
	return true
}

// Release returns an attempt to the budget. It is used when an attempt was
// reserved but never opened a window, so a failure to start does not spend one.
func (b *AttemptBudget) Release(origin string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent[origin] > 0 {
		b.spent[origin]--
	}
}

// Clear forgets what an origin has spent, which a verified solve does.
func (b *AttemptBudget) Clear(origin string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.spent, origin)
}

// Spent reports how many attempts an origin has used.
func (b *AttemptBudget) Spent(origin string) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent[origin]
}

// Limit reports the per-origin allowance.
func (b *AttemptBudget) Limit() int {
	if b == nil {
		return 0
	}
	return b.limit
}
