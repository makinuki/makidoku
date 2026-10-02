package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

// DefaultBrowserProfile names the transport profile the daemon fetches under.
// A stored bundle captured under a different profile is not replayed, because a
// cookie is only valid for the client identity that obtained it.
const DefaultBrowserProfile = "default"

// fallbackOrigin records material whose origin could not be determined, which
// happens when a caller submits clearance without a challenge to attribute it
// to.
const fallbackOrigin = "default"

// ClearanceBroker reads and writes the stored clearance material for a source.
// The daemon has no interactive browser of its own, so clearance arrives through
// the local API: the operator solves the challenge in a normal browser and
// submits the cookie together with the matching user agent.
//
// Material is kept per source and origin rather than per source, because
// clearance for one registrable domain is never presented to another.
type ClearanceBroker struct {
	store  *db.Repository
	legacy Storage
	wait   time.Duration

	mu      sync.Mutex
	waiters map[string]chan struct{}

	// hook is told when a request starts waiting. It is guarded separately from
	// the waiters because it is set once at start-up and read on every blocked
	// request.
	hookMu sync.Mutex
	hook   func(sourceID, origin string)
}

func NewClearanceBroker(store *db.Repository, legacy Storage, wait time.Duration) *ClearanceBroker {
	return &ClearanceBroker{
		store:   store,
		legacy:  legacy,
		wait:    wait,
		waiters: map[string]chan struct{}{},
	}
}

// Submit stores a single clearance cookie for a source and origin, and releases
// any request waiting on it. An empty cookie removes the stored material.
func (b *ClearanceBroker) Submit(sourceID, origin, cookie, userAgent string) error {
	if strings.TrimSpace(cookie) == "" {
		return b.Delete(sourceID, origin)
	}
	return b.SubmitBundle(sourceID, db.ClearanceBundle{
		Origin:    normalizeOrigin(origin),
		Cookies:   map[string]string{"cf_clearance": cookie},
		UserAgent: userAgent,
	})
}

// SubmitBundle stores a full captured bundle, preserving the companion cookies
// and the identity the solve ran under.
func (b *ClearanceBroker) SubmitBundle(sourceID string, bundle db.ClearanceBundle) error {
	bundle.SourceID = sourceID
	bundle.Origin = normalizeOrigin(bundle.Origin)
	if bundle.BrowserProfile == "" {
		bundle.BrowserProfile = DefaultBrowserProfile
	}
	if bundle.ObtainedAt == 0 {
		bundle.ObtainedAt = time.Now().Unix()
	}
	if bundle.Generation <= 0 {
		bundle.Generation = 1
	}
	if bundle.Status == "" {
		bundle.Status = db.ClearanceUsable
	}
	if err := b.store.PutClearanceBundle(bundle); err != nil {
		return err
	}
	b.notify(sourceID)
	return nil
}

// Delete removes the stored material for one source and origin. A missing row is
// not an error.
func (b *ClearanceBroker) Delete(sourceID, origin string) error {
	if err := b.store.DeleteClearanceBundle(sourceID, normalizeOrigin(origin)); err != nil {
		return err
	}
	b.notify(sourceID)
	return nil
}

// Bundles returns every bundle stored for a source.
func (b *ClearanceBroker) Bundles(sourceID string) ([]db.ClearanceBundle, error) {
	return b.store.ListClearanceBundles(sourceID)
}

// Bundle returns the material for one source and origin. When no bundle exists,
// the reserved plugin_storage keys are consulted so installs that predate the
// bundle table keep working, and the result is written back.
func (b *ClearanceBroker) Bundle(sourceID, origin string) *db.ClearanceBundle {
	origin = normalizeOrigin(origin)
	bundle, found, err := b.store.GetClearanceBundle(sourceID, origin)
	if err != nil {
		slog.Warn("read clearance bundle", "source", sourceID, "origin", origin, "error", err)
		return nil
	}
	if found {
		return bundle
	}
	migrated := b.legacyBundle(sourceID)
	if migrated == nil {
		return nil
	}
	if err := b.store.PutClearanceBundle(*migrated); err != nil {
		slog.Warn("write through legacy clearance", "source", sourceID, "error", err)
	}
	return migrated
}

// legacyBundle reads the reserved plugin_storage keys written by earlier
// versions, which held only a single cookie and a user agent.
func (b *ClearanceBroker) legacyBundle(sourceID string) *db.ClearanceBundle {
	if b.legacy == nil {
		return nil
	}
	cookie, ok, err := b.legacy.Get(sourceID, ClearanceCookieKey)
	if err != nil || !ok || cookie == "" {
		return nil
	}
	userAgent, _, err := b.legacy.Get(sourceID, ClearanceUserAgentKey)
	if err != nil {
		userAgent = ""
	}
	return &db.ClearanceBundle{
		SourceID:       sourceID,
		Origin:         fallbackOrigin,
		Cookies:        map[string]string{"cf_clearance": cookie},
		UserAgent:      userAgent,
		BrowserProfile: DefaultBrowserProfile,
		ObtainedAt:     time.Now().Unix(),
		Generation:     1,
		Status:         db.ClearanceUsable,
	}
}

// HasClearance reports whether any origin for the source holds a clearance
// cookie. The cookie value itself is never exposed.
func (b *ClearanceBroker) HasClearance(sourceID string) bool {
	if has, err := b.store.SourceHasClearance(sourceID); err == nil {
		return has
	}
	return b.legacyBundle(sourceID) != nil
}

// normalizeOrigin applies the fallback when an origin is unknown.
func normalizeOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return fallbackOrigin
	}
	return strings.ToLower(origin)
}

// originOf reduces a URL to the domain the clearance belongs to.
func originOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return fallbackOrigin
	}
	return strings.ToLower(parsed.Hostname())
}

// Resolve implements ChallengeResolver.
//
// The challenge is first attributed to an origin so the stored material for that
// domain can be consulted. A bundle recorded under a different transport profile
// is ignored, because a cookie is only valid for the client identity that
// obtained it.
// SetChallengeHook registers a function told when a request has begun waiting for
// clearance, so a host that can answer the challenge itself may do so.
//
// It is optional and set after construction, because the engine has no business
// knowing whether an embedded browser exists. The hook must not block: it runs
// on the request path, and the wait that follows is what the reader is waiting
// on.
func (b *ClearanceBroker) SetChallengeHook(hook func(sourceID, origin string)) {
	b.hookMu.Lock()
	b.hook = hook
	b.hookMu.Unlock()
}

// challengeHook returns the registered hook, or nil.
func (b *ClearanceBroker) challengeHook() func(sourceID, origin string) {
	b.hookMu.Lock()
	defer b.hookMu.Unlock()
	return b.hook
}

func (b *ClearanceBroker) Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
	origin := originOf(challenge.URL)
	if b.fresh(sourceID, origin, usedCookie) {
		return true
	}
	if b.wait <= 0 {
		slog.Warn("engine blocked by anti-bot challenge", "source", sourceID, "origin", origin, "url", challenge.URL)
		return false
	}

	// Announced once per wait, before it begins, so a host that can answer the
	// challenge starts doing so while this request is still parked rather than
	// after it has already given up.
	if hook := b.challengeHook(); hook != nil {
		hook(sourceID, origin)
	}

	slog.Info("engine blocked by anti-bot challenge, waiting for clearance",
		"source", sourceID, "origin", origin, "url", challenge.URL, "wait", b.wait)
	deadline := time.NewTimer(b.wait)
	defer deadline.Stop()

	for {
		// Subscribe before re-reading so a submission landing in between is
		// not missed.
		updated := b.subscribe(sourceID)
		if b.fresh(sourceID, origin, usedCookie) {
			return true
		}
		select {
		case <-updated:
		case <-deadline.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// fresh reports whether usable clearance exists for the origin and differs from
// what the blocked attempt already used.
func (b *ClearanceBroker) fresh(sourceID, origin, usedCookie string) bool {
	bundle := b.Bundle(sourceID, origin)
	if bundle == nil || !bundle.HasClearance() {
		return false
	}
	if bundle.BrowserProfile != "" && bundle.BrowserProfile != DefaultBrowserProfile {
		return false
	}
	return bundle.Cookies["cf_clearance"] != usedCookie
}

// MarkChallenged records that material for an origin met a challenge, advancing
// the generation so a burst of parallel failures counts once.
func (b *ClearanceBroker) MarkChallenged(sourceID, origin string) error {
	origin = normalizeOrigin(origin)
	bundle, found, err := b.store.GetClearanceBundle(sourceID, origin)
	if err != nil {
		return fmt.Errorf("read clearance bundle: %w", err)
	}
	if !found {
		return nil
	}
	return b.store.TouchClearanceChallenge(sourceID, origin, bundle.Generation)
}

// MarkUsable records that a request carrying the material succeeded, which is the
// only reliable evidence that it still works.
func (b *ClearanceBroker) MarkUsable(sourceID, origin string) error {
	return b.store.TouchClearanceSuccess(sourceID, normalizeOrigin(origin))
}

func (b *ClearanceBroker) subscribe(sourceID string) <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch, ok := b.waiters[sourceID]
	if !ok {
		ch = make(chan struct{})
		b.waiters[sourceID] = ch
	}
	return ch
}

func (b *ClearanceBroker) notify(sourceID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.waiters[sourceID]; ok {
		close(ch)
		delete(b.waiters, sourceID)
	}
}
