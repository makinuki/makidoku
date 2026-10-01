package engine

import (
	"context"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

// TestBrokerLegacyFallbackWritesThrough checks that material written by an
// earlier version under the reserved plugin_storage keys is picked up and
// migrated into the bundle table.
func TestBrokerLegacyFallbackWritesThrough(t *testing.T) {
	broker, repo := newTestBroker(t, 0)

	legacy := NewMemoryStorage()
	if err := legacy.Set("kagane", ClearanceCookieKey, "old-cookie"); err != nil {
		t.Fatalf("seed legacy cookie: %v", err)
	}
	if err := legacy.Set("kagane", ClearanceUserAgentKey, "old-agent"); err != nil {
		t.Fatalf("seed legacy agent: %v", err)
	}
	broker.legacy = legacy

	got := broker.Bundle("kagane", fallbackOrigin)
	if got == nil {
		t.Fatal("legacy material was not picked up")
	}
	if got.Cookies["cf_clearance"] != "old-cookie" {
		t.Fatalf("legacy cookie = %q", got.Cookies["cf_clearance"])
	}
	if _, found, err := repo.GetClearanceBundle("kagane", fallbackOrigin); err != nil {
		t.Fatalf("read back migrated bundle: %v", err)
	} else if !found {
		t.Fatal("legacy material was not written through")
	}
}

// TestBrokerResolveUsesOriginScopedBundle checks that material is offered only
// to the origin it was captured for, and only when it differs from what the
// blocked attempt already sent.
func TestBrokerResolveUsesOriginScopedBundle(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.Submit("kagane", "kagane.to", "solved", "agent"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	challenge := HttpError{Error: CodeCloudflareBlocked, URL: "https://kagane.to/series/1"}
	if !broker.Resolve(context.Background(), "kagane", "", challenge) {
		t.Fatal("fresh clearance for the challenged origin should resolve")
	}
	if broker.Resolve(context.Background(), "kagane", "solved", challenge) {
		t.Fatal("the cookie the blocked attempt used must not count as fresh")
	}
	other := HttpError{Error: CodeCloudflareBlocked, URL: "https://img.example/a.jpg"}
	if broker.Resolve(context.Background(), "kagane", "", other) {
		t.Fatal("material for one origin must not clear another")
	}
}

// TestBrokerIgnoresBundleFromAnotherProfile checks that material captured under
// a different transport profile is not replayed, because a cookie is only valid
// for the identity that obtained it.
func TestBrokerIgnoresBundleFromAnotherProfile(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.SubmitBundle("kagane", db.ClearanceBundle{
		Origin:         "kagane.to",
		Cookies:        map[string]string{"cf_clearance": "solved"},
		BrowserProfile: "chrome_154",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	challenge := HttpError{Error: CodeCloudflareBlocked, URL: "https://kagane.to/series/1"}
	if broker.Resolve(context.Background(), "kagane", "", challenge) {
		t.Fatal("a bundle from another browser profile must not be replayed")
	}
}
