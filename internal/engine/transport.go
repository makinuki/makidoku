package engine

import (
	"context"
	"sort"
	"strings"
)

// ChallengeClass is the host's verdict on a response that did not succeed.
// The distinction between a solvable challenge and a terminal block is the
// reason this type exists: retrying a terminal block never clears it, so it
// must not reach the solver.
type ChallengeClass string

const (
	// ClassNone means the response is ordinary and reaches the plugin with its
	// status intact. The plugin performs its own status mapping.
	ClassNone ChallengeClass = "none"
	// ClassSolvable means an interactive challenge was detected and the remedy
	// chain may attempt to clear it.
	ClassSolvable ChallengeClass = "solvable"
	// ClassTerminal means the origin refused access outright, for example a
	// geographic restriction or a WAF ban. No puzzle exists to solve.
	ClassTerminal ChallengeClass = "terminal"
	// ClassRateLimited means the origin asked the client to slow down. This is
	// not a challenge and must not be solved.
	ClassRateLimited ChallengeClass = "rate-limited"
)

// ClearanceBundle is the material captured by a single solve: the whole cookie
// jar for one registrable domain together with the user agent and client hints
// that were in effect during the solve. The user agent is part of the bundle
// rather than a separate setting because a cookie is only valid for the client
// identity that obtained it.
type ClearanceBundle struct {
	// Origin is the registrable domain the material belongs to.
	Origin string
	// Cookies maps cookie name to value.
	Cookies map[string]string
	// UserAgent is the agent string used during the solve.
	UserAgent string
	// SecChUa carries the full client hint set from the same solve.
	SecChUa map[string]string
}

// CookieHeader renders the bundle as a Cookie header value. It returns an empty
// string for a bundle with no cookies.
func (b ClearanceBundle) CookieHeader() string {
	if len(b.Cookies) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(b.Cookies))
	for name, value := range b.Cookies {
		pairs = append(pairs, name+"="+value)
	}
	// A stable order keeps request signatures reproducible, which matters
	// because header ordering is itself an observable.
	sort.Strings(pairs)
	return strings.Join(pairs, "; ")
}

// Has reports whether the bundle carries a clearance cookie.
func (b ClearanceBundle) Has() bool {
	return b.Cookies["cf_clearance"] != ""
}

// Challenger obtains a clearance bundle for an origin. It is deliberately
// narrow: warmup and replay are remedies that do not involve a human, so they
// do not belong behind this interface.
type Challenger interface {
	// Available reports whether a challenge can be presented right now, which
	// is false on a machine with no display.
	Available() bool
	// Solve presents the origin and waits for the visitor to clear the
	// challenge. It returns the captured material, or false when the challenge
	// was never cleared.
	Solve(ctx context.Context, origin string) (ClearanceBundle, bool)
	// Close releases any window or browser process the implementation owns.
	Close() error
}

// Transport performs one request for a source, applying the ordered chain of
// remedies when the response is classified as a challenge.
type Transport interface {
	Do(ctx context.Context, sourceID string, req HttpRequest) (*HttpResponse, *HttpError)
}
