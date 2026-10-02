package app

import "sync"

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
