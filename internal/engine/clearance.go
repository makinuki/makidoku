package engine

import (
	"context"
	"log"
	"sync"
	"time"
)

// ClearanceBroker records the anti-bot clearance material a challenged source
// needs and wakes requests waiting on it. The daemon has no interactive
// browser of its own, so clearance arrives through the local API: the operator
// solves the challenge in a normal browser and submits the cf_clearance cookie
// together with the matching user agent.
//
// With a zero wait the broker never blocks and a challenged request fails
// immediately with CLOUDFLARE_BLOCKED. With a positive wait the blocked GET or
// HEAD is held until clearance is submitted or the wait elapses, then replayed
// once.
type ClearanceBroker struct {
	storage Storage
	wait    time.Duration

	mu      sync.Mutex
	waiters map[string]chan struct{}
}

func NewClearanceBroker(storage Storage, wait time.Duration) *ClearanceBroker {
	return &ClearanceBroker{
		storage: storage,
		wait:    wait,
		waiters: map[string]chan struct{}{},
	}
}

// Submit persists clearance material for sourceID and releases any request
// waiting on it. An empty cookie clears the stored material.
func (b *ClearanceBroker) Submit(sourceID, cookie, userAgent string) error {
	if err := b.storage.Set(sourceID, ClearanceCookieKey, cookie); err != nil {
		return err
	}
	if err := b.storage.Set(sourceID, ClearanceUserAgentKey, userAgent); err != nil {
		return err
	}
	b.notify(sourceID)
	return nil
}

// HasClearance reports whether usable clearance material is stored. The
// cookie value itself is never exposed through the API.
func (b *ClearanceBroker) HasClearance(sourceID string) bool {
	cookie, ok, err := b.storage.Get(sourceID, ClearanceCookieKey)
	return err == nil && ok && cookie != ""
}

// Resolve implements ChallengeResolver.
func (b *ClearanceBroker) Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
	if b.fresh(sourceID, usedCookie) {
		return true
	}
	if b.wait <= 0 {
		log.Printf("engine: %s blocked by an anti-bot challenge at %s; submit clearance to POST /api/sources/%s/clearance",
			sourceID, challenge.URL, sourceID)
		return false
	}

	log.Printf("engine: %s blocked by an anti-bot challenge at %s; waiting up to %s for clearance on POST /api/sources/%s/clearance",
		sourceID, challenge.URL, b.wait, sourceID)
	deadline := time.NewTimer(b.wait)
	defer deadline.Stop()

	for {
		// Subscribe before re-reading so a submission landing in between is
		// not missed.
		updated := b.subscribe(sourceID)
		if b.fresh(sourceID, usedCookie) {
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

// fresh reports whether stored clearance differs from what the blocked
// attempt already used.
func (b *ClearanceBroker) fresh(sourceID, usedCookie string) bool {
	cookie, ok, err := b.storage.Get(sourceID, ClearanceCookieKey)
	if err != nil || !ok {
		return false
	}
	return cookie != "" && cookie != usedCookie
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
