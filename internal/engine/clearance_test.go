package engine

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

// newTestBroker builds a broker over a temporary database and a memory storage
// standing in for the legacy reserved keys.
func newTestBroker(t *testing.T, wait time.Duration) (*ClearanceBroker, *db.Repository) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	if _, err := handle.Exec(`INSERT INTO sources(
		id, name, version, abi_version, lang, base_url, wasm_path, installed_at
	) VALUES('kagane', 'Kagane', '1.0.0', 1, 'en', 'https://kagane.to', 'kagane.wasm', ?)`,
		time.Now().Unix()); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	repo := db.NewRepository(handle)
	return NewClearanceBroker(repo, NewMemoryStorage(), wait), repo
}

// hostOnly strips the port so it matches the origin key the fetcher derives.
func hostOnly(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return parsed.Hostname()
}

// TestBrokerStoresWholeJarAcrossOrigins checks that material is kept per origin
// and that companion cookies survive, since dropping them causes an immediate
// re-challenge.
func TestBrokerStoresWholeJarAcrossOrigins(t *testing.T) {
	broker, _ := newTestBroker(t, 0)

	if err := broker.SubmitBundle("kagane", db.ClearanceBundle{
		Origin:    "kagane.to",
		Cookies:   map[string]string{"cf_clearance": "page", "cf_chl_rc_ni": "aux"},
		UserAgent: "agent-page",
	}); err != nil {
		t.Fatalf("submit page bundle: %v", err)
	}
	if err := broker.SubmitBundle("kagane", db.ClearanceBundle{
		Origin:    "img.example",
		Cookies:   map[string]string{"cf_clearance": "image"},
		UserAgent: "agent-image",
	}); err != nil {
		t.Fatalf("submit image bundle: %v", err)
	}

	page := broker.Bundle("kagane", "kagane.to")
	if page == nil {
		t.Fatal("page bundle missing")
	}
	if page.Cookies["cf_chl_rc_ni"] != "aux" {
		t.Fatalf("companion cookie lost: %+v", page.Cookies)
	}
	if page.UserAgent != "agent-page" {
		t.Fatalf("user agent = %q", page.UserAgent)
	}

	image := broker.Bundle("kagane", "img.example")
	if image == nil {
		t.Fatal("image bundle missing")
	}
	if image.Cookies["cf_clearance"] != "image" {
		t.Fatalf("origins overwrote each other: %+v", image.Cookies)
	}

	// An origin with no bundle must not borrow another origin's material.
	if got := broker.Bundle("kagane", "other.example"); got != nil {
		t.Fatalf("unrelated origin returned %+v", got)
	}
}
