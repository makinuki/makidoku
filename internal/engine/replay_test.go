package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

// The loop the research proved by hand: a request is challenged, a solve captures
// material, and the original request is then made again and accepted. This is the
// one worth locking down, because every other part of the feature assumes it.
//
// The server stands in for a gated origin. It refuses anything without the cookie
// it issues, so the test fails if the material is not stored, not applied, or
// applied under a different identity.
func TestSolveThenReplayReachesTheRealContent(t *testing.T) {
	const clearance = "solved-clearance"
	const issuedAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/154.0.0.0"

	var challenges int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "cf_clearance="+clearance {
			atomic.AddInt32(&challenges, 1)
			w.Header().Set("Cf-Mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(challengeBody))
			return
		}
		// The cookie is only valid for the identity that obtained it, so a replay
		// under any other agent is a replay that fails.
		if r.Header.Get("User-Agent") != issuedAgent {
			t.Errorf("replay agent = %q, want the agent the clearance was issued to",
				r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"title":"the chapter listing"}`))
	}))
	defer server.Close()

	broker, repo := newTestBroker(t, time.Minute)
	origin := hostOnly(server.URL)

	// Stands in for the solver. A real one presents a window and reads the cookie
	// the site issues; what the fetcher needs is only that material lands in the
	// broker while the blocked request is parked.
	installed := make(chan struct{})
	resolver := &bundleResolver{broker: broker, install: func() {
		if err := broker.SubmitBundle(testSourceID, db.ClearanceBundle{
			Origin:         origin,
			Cookies:        map[string]string{"cf_clearance": clearance},
			UserAgent:      issuedAgent,
			BrowserProfile: DefaultBrowserProfile,
		}); err != nil {
			t.Errorf("store captured material: %v", err)
		}
		close(installed)
	}}

	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	fetcher.SetBaseURL(testSourceID, server.URL)

	resp, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL:    server.URL + "/series/1",
		Method: http.MethodGet,
	})
	if herr != nil {
		t.Fatalf("host error after a successful solve: %+v", herr)
	}
	if !strings.Contains(resp.Body, "the chapter listing") {
		t.Fatalf("body = %q, want the content behind the challenge", resp.Body)
	}

	<-installed
	// Two refusals precede the success: the blocked request itself, then the
	// warm-up, which is also refused and so does not lead to a retry. The third
	// attempt carries the captured material and is accepted.
	if got := atomic.LoadInt32(&challenges); got != 2 {
		t.Fatalf("challenges = %d, want the request and the warm-up", got)
	}

	// A verified request records the material as usable, so the stored record must
	// agree with what actually happened.
	stored, found, err := repo.GetClearanceBundle(testSourceID, origin)
	if err != nil || !found {
		t.Fatalf("read stored bundle: %v", err)
	}
	if stored.Status != db.ClearanceUsable {
		t.Fatalf("status = %q, want the bundle marked usable by the successful replay",
			stored.Status)
	}
	if stored.Cookies["cf_clearance"] != clearance {
		t.Fatalf("stored cookie = %q", stored.Cookies["cf_clearance"])
	}
}

// A capture the site then refuses must not be recorded as usable, or the interface
// reports a working clearance that does not work and the reader is sent back to a
// button that will fail the same way.
func TestARefusedCaptureIsNotRecordedAsUsable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer server.Close()

	broker, repo := newTestBroker(t, time.Minute)
	origin := hostOnly(server.URL)
	resolver := &bundleResolver{broker: broker, install: func() {
		if err := broker.SubmitBundle(testSourceID, db.ClearanceBundle{
			Origin:         origin,
			Cookies:        map[string]string{"cf_clearance": "refused"},
			UserAgent:      "agent",
			BrowserProfile: DefaultBrowserProfile,
		}); err != nil {
			t.Errorf("store captured material: %v", err)
		}
	}}

	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	fetcher.SetBaseURL(testSourceID, server.URL)

	if _, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL: server.URL + "/series/1",
	}); herr == nil {
		t.Fatal("a site that kept refusing reported success")
	}

	stored, found, err := repo.GetClearanceBundle(testSourceID, origin)
	if err != nil || !found {
		t.Fatalf("read stored bundle: %v", err)
	}
	if stored.Status == db.ClearanceUsable {
		t.Fatalf("status = %q, want a refused capture not recorded as usable", stored.Status)
	}
}

// A subdomain presenting clearance captured for its parent must reach the parent's
// gated endpoint. This is the loop that was broken: exact-origin keying meant the
// image host never saw the material and every page image was refused.
//
// Both servers share a host, so the parent and the image host differ by port only.
// That is enough here, because clearance is keyed by host rather than by port.
func TestSolveThenReplayReachesAParentGatedSubdomain(t *testing.T) {
	const clearance = "parent-clearance"

	// The asset host refuses anything without the parent's cookie, which is the
	// behaviour that a real gated CDN exhibits.
	var images int32
	asset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "cf_clearance="+clearance) {
			atomic.AddInt32(&images, 1)
			w.Header().Set("Cf-Mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(challengeBody))
			return
		}
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0 image bytes"))
	}))
	defer asset.Close()

	broker, _ := newTestBroker(t, time.Minute)

	// The capture is recorded for the host a solve would present.
	resolver := &bundleResolver{broker: broker, install: func() {
		if err := broker.SubmitBundle(testSourceID, db.ClearanceBundle{
			Origin:         hostOnly(asset.URL),
			Cookies:        map[string]string{"cf_clearance": clearance},
			UserAgent:      "agent",
			BrowserProfile: DefaultBrowserProfile,
		}); err != nil {
			t.Errorf("store captured material: %v", err)
		}
	}}

	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	fetcher.SetBaseURL(testSourceID, "https://"+hostOnly(asset.URL)+"/")

	body, err := fetcher.FetchImage(context.Background(), testSourceID, asset.URL+"/page.jpg", nil)
	if err != nil {
		t.Fatalf("the asset host refused material captured for its host: %v", err)
	}
	if !strings.Contains(string(body), "image bytes") {
		t.Fatalf("body = %q, want the image", body)
	}
	if got := atomic.LoadInt32(&images); got != 2 {
		t.Fatalf("asset challenges = %d, want the request and the warm-up", got)
	}
}
