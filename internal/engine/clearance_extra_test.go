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
	if err := legacy.Set("source", ClearanceCookieKey, "old-cookie"); err != nil {
		t.Fatalf("seed legacy cookie: %v", err)
	}
	if err := legacy.Set("source", ClearanceUserAgentKey, "old-agent"); err != nil {
		t.Fatalf("seed legacy agent: %v", err)
	}
	broker.legacy = legacy

	got := broker.Bundle("source", fallbackOrigin)
	if got == nil {
		t.Fatal("legacy material was not picked up")
	}
	if got.Cookies["cf_clearance"] != "old-cookie" {
		t.Fatalf("legacy cookie = %q", got.Cookies["cf_clearance"])
	}
	if _, found, err := repo.GetClearanceBundle("source", fallbackOrigin); err != nil {
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
	if err := broker.Submit(testSourceID, "gate.test", "solved", "agent"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	challenge := HttpError{Error: CodeCloudflareBlocked, URL: "https://gate.test/series/1"}
	if !broker.Resolve(context.Background(), testSourceID, "", challenge) {
		t.Fatal("fresh clearance for the challenged origin should resolve")
	}
	if broker.Resolve(context.Background(), testSourceID, "solved", challenge) {
		t.Fatal("the cookie the blocked attempt used must not count as fresh")
	}
	other := HttpError{Error: CodeCloudflareBlocked, URL: "https://img.other.test/a.jpg"}
	if broker.Resolve(context.Background(), testSourceID, "", other) {
		t.Fatal("material for one origin must not clear another")
	}
}

// TestBrokerIgnoresBundleFromAnotherProfile checks that material captured under
// a different transport profile is not replayed, because a cookie is only valid
// for the identity that obtained it.
func TestBrokerIgnoresBundleFromAnotherProfile(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.SubmitBundle("source", db.ClearanceBundle{
		Origin:         "gate.test",
		Cookies:        map[string]string{"cf_clearance": "solved"},
		BrowserProfile: "chrome_154",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	challenge := HttpError{Error: CodeCloudflareBlocked, URL: "https://gate.test/series/1"}
	if broker.Resolve(context.Background(), "source", "", challenge) {
		t.Fatal("a bundle from another browser profile must not be replayed")
	}
}

// A source that serves documents from one host and images from another is gated
// once for the parent domain. The subdomain presents the material captured for
// the parent rather than reporting no clearance at all.
func TestBrokerOffersParentMaterialToASubdomain(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.SubmitBundle("source", db.ClearanceBundle{
		Origin:  "gate.test",
		Cookies: map[string]string{"cf_clearance": "solved", "__guard_trust": "t"},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	got := broker.Bundle("source", "img.gate.test")
	if got == nil {
		t.Fatal("a subdomain must inherit the material stored for its parent")
	}
	if got.Cookies["cf_clearance"] != "solved" || got.Cookies["__guard_trust"] != "t" {
		t.Fatalf("cookies = %v, want the whole parent jar", got.Cookies)
	}
	// The inherited record keeps the origin it is stored under, so nothing
	// reports it as belonging to the subdomain.
	if got.Origin != "gate.test" {
		t.Fatalf("origin = %q, want the storing origin", got.Origin)
	}

	challenge := HttpError{Error: CodeCloudflareBlocked, URL: "https://img.gate.test/img/1.jpg"}
	if !broker.Resolve(context.Background(), "source", "", challenge) {
		t.Fatal("inherited clearance should resolve the subdomain challenge")
	}
}

// The fallback widens within one domain only. An unrelated domain, and a host that
// merely ends with the same letters, are never offered another domain's material.
func TestBrokerDoesNotOfferMaterialAcrossDomains(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.Submit("source", "gate.test", "solved", "agent"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	for _, host := range []string{"notgate.test", "evil.test", "gate.test.evil.test"} {
		if got := broker.Bundle("source", host); got != nil {
			t.Fatalf("host %q was offered material from another domain", host)
		}
	}
}

// A subdomain holding its own bundle is not displaced by the parent's, so a
// second solve for the image host is never silently discarded.
func TestBrokerPrefersTheMostSpecificBundle(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.Submit("source", "gate.test", "parent", "agent"); err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	if err := broker.Submit("source", "img.gate.test", "image", "agent"); err != nil {
		t.Fatalf("submit image: %v", err)
	}
	got := broker.Bundle("source", "img.gate.test")
	if got == nil || got.Cookies["cf_clearance"] != "image" {
		t.Fatalf("bundle = %+v, want the subdomain's own material", got)
	}
}

// The nearest ancestor wins over a more distant one, so material held for a
// subdomain is preferred over material held for the registrable domain.
func TestBrokerPrefersTheNearestAncestor(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.Submit("source", "gate.test", "root", "agent"); err != nil {
		t.Fatalf("submit root: %v", err)
	}
	if err := broker.Submit("source", "cdn.gate.test", "cdn", "agent"); err != nil {
		t.Fatalf("submit cdn: %v", err)
	}
	got := broker.Bundle("source", "img.cdn.gate.test")
	if got == nil || got.Cookies["cf_clearance"] != "cdn" {
		t.Fatalf("bundle = %+v, want the nearest ancestor", got)
	}
}

// A subdomain presenting inherited material and being refused marks the bundle
// that stores it, rather than addressing a row the subdomain does not have.
func TestBrokerMarksTheBundleThatStoredInheritedMaterial(t *testing.T) {
	broker, repo := newTestBroker(t, 0)
	if err := broker.SubmitBundle("source", db.ClearanceBundle{
		Origin:  "gate.test",
		Cookies: map[string]string{"cf_clearance": "solved"},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if err := broker.MarkChallenged("source", "img.gate.test"); err != nil {
		t.Fatalf("mark challenged: %v", err)
	}
	stored, found, err := repo.GetClearanceBundle("source", "gate.test")
	if err != nil || !found {
		t.Fatalf("read parent bundle: %v", err)
	}
	if stored.Status != db.ClearanceChallenged {
		t.Fatalf("status = %q, want the storing bundle marked challenged", stored.Status)
	}
}

// The reserved fallback origin names no domain, so it inherits nothing and is
// never inherited from.
func TestBrokerFallbackOriginIsNotInherited(t *testing.T) {
	broker, _ := newTestBroker(t, 0)
	if err := broker.Submit("source", fallbackOrigin, "loose", "agent"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got := broker.Bundle("source", "img.gate.test"); got != nil {
		t.Fatal("material under the reserved origin must not reach a real domain")
	}
}

// Ancestor matching is on label boundaries, so a suffix that is not a whole
// label is not a parent.
func TestAncestorDomainRequiresALabelBoundary(t *testing.T) {
	cases := []struct {
		ancestor, host string
		want           bool
	}{
		{"gate.test", "img.gate.test", true},
		{"gate.test", "a.b.gate.test", true},
		{"gate.test", "gate.test", false},
		{"gate.test", "notgate.test", false},
		{"gate.te", "img.gate.test", false},
		{"", "img.gate.test", false},
		{"gate.test", "", false},
	}
	for _, tc := range cases {
		if got := isAncestorDomain(tc.ancestor, tc.host); got != tc.want {
			t.Fatalf("isAncestorDomain(%q, %q) = %v, want %v", tc.ancestor, tc.host, got, tc.want)
		}
	}
}
