package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockChallengeServer serves a scripted sequence of responses for one path and
// records every request it received. It stands in for a real gated origin so
// remedy ordering can be asserted without network access or a browser.
type mockChallengeServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []recordedRequest
	script   []mockResponse
	// served counts how many scripted responses have been handed out. Once the
	// script is exhausted the final entry repeats, which models an origin that
	// keeps challenging until the right material is presented.
	served int
}

type recordedRequest struct {
	Path   string
	Cookie string
	Header http.Header
}

type mockResponse struct {
	status  int
	headers map[string]string
	body    string
}

func newMockChallengeServer(t *testing.T, script ...mockResponse) *mockChallengeServer {
	t.Helper()
	s := &mockChallengeServer{script: script}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{
			Path:   r.URL.Path,
			Cookie: r.Header.Get("Cookie"),
			Header: r.Header.Clone(),
		})
		idx := s.served
		if idx >= len(s.script) {
			idx = len(s.script) - 1
		}
		resp := s.script[idx]
		s.served++
		s.mu.Unlock()

		for name, value := range resp.headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(s.Close)
	return s
}

// count reports how many requests the server received.
func (s *mockChallengeServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// requestAt returns the recorded request at index i.
func (s *mockChallengeServer) requestAt(i int) recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[i]
}

// challengeScript returns a script that challenges twice and then serves
// content, which is the sequence a warmup-then-retry remedy must clear.
func challengeScript(t *testing.T) []mockResponse {
	t.Helper()
	challenge := mockResponse{
		status:  403,
		headers: map[string]string{"cf-mitigated": "challenge", "server": "cloudflare"},
		body:    readFixture(t, "interstitial.html"),
	}
	ok := mockResponse{
		status:  200,
		headers: map[string]string{"content-type": "text/html"},
		body:    readFixture(t, "ok_page.html"),
	}
	return []mockResponse{challenge, challenge, ok}
}

// fakeChallenger stands in for the platform WebView solver. Phase 4 supplies
// the real implementation; this fake is what lets the remedy chain be tested
// today and keeps the interface shape honest.
type fakeChallenger struct {
	available bool
	bundle    ClearanceBundle
	cleared   bool

	mu     sync.Mutex
	calls  []string
	closed bool
}

func newFakeChallenger(bundle ClearanceBundle) *fakeChallenger {
	return &fakeChallenger{available: true, bundle: bundle, cleared: true}
}

func (f *fakeChallenger) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available
}

func (f *fakeChallenger) Solve(_ context.Context, origin string) (ClearanceBundle, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, origin)
	if !f.available || !f.cleared {
		return ClearanceBundle{}, false
	}
	bundle := f.bundle
	bundle.Origin = origin
	return bundle, true
}

func (f *fakeChallenger) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// solvedOrigins lists the origins the fake was asked to solve.
func (f *fakeChallenger) solvedOrigins() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// TestBundleCarriesWholeJar checks that a bundle is more than a single cookie
// and that the user agent travels with it. Losing a companion cookie such as
// __cf_bm while keeping cf_clearance causes an immediate re-challenge on many
// zones, so the bundle shape is the thing under test.
func TestBundleCarriesWholeJar(t *testing.T) {
	bundle := ClearanceBundle{
		Origin:    "example.test",
		UserAgent: "Mozilla/5.0 test",
		Cookies: map[string]string{
			"cf_clearance": "abc",
			"__cf_bm":      "def",
			"_cfuvid":      "ghi",
		},
	}
	if !bundle.Has() {
		t.Fatal("bundle with cf_clearance must report Has")
	}
	header := bundle.CookieHeader()
	for _, want := range []string{"cf_clearance=abc", "__cf_bm=def", "_cfuvid=ghi"} {
		if !strings.Contains(header, want) {
			t.Errorf("cookie header %q missing %q", header, want)
		}
	}
	if bundle.UserAgent != "Mozilla/5.0 test" {
		t.Errorf("user agent = %q", bundle.UserAgent)
	}
	// Ordering must be stable so the emitted request is reproducible.
	if again := bundle.CookieHeader(); again != header {
		t.Errorf("cookie header not stable: %q then %q", header, again)
	}
}

// TestBundleWithoutClearance pins the negative case, since Has gates whether a
// bundle is worth persisting.
func TestBundleWithoutClearance(t *testing.T) {
	bundle := ClearanceBundle{Cookies: map[string]string{"__cf_bm": "def"}}
	if bundle.Has() {
		t.Fatal("bundle without cf_clearance must not report Has")
	}
	if bundle.CookieHeader() != "__cf_bm=def" {
		t.Errorf("expected companion cookie to still render, got %q", bundle.CookieHeader())
	}
	empty := ClearanceBundle{}
	if empty.CookieHeader() != "" {
		t.Errorf("empty bundle rendered %q", empty.CookieHeader())
	}
}

// TestFakeChallengerStampsOrigin checks the fake matches the contract Phase 4
// must implement: the caller supplies the origin and the solver echoes it back
// so the caller can key the stored material without re-parsing the URL.
func TestFakeChallengerStampsOrigin(t *testing.T) {
	fake := newFakeChallenger(ClearanceBundle{
		Cookies: map[string]string{"cf_clearance": "solved"},
	})
	bundle, ok := fake.Solve(context.Background(), "mangafire.to")
	if !ok {
		t.Fatal("fake solver should report success")
	}
	if bundle.Origin != "mangafire.to" {
		t.Errorf("origin = %q, want mangafire.to", bundle.Origin)
	}
	if !fake.Available() {
		t.Error("fake solver should report available")
	}
}

// TestFakeChallengerUnavailable covers the headless machine, where no window
// can be presented and the chain must fall through to its fallback.
func TestFakeChallengerUnavailable(t *testing.T) {
	fake := newFakeChallenger(ClearanceBundle{Cookies: map[string]string{"cf_clearance": "x"}})
	fake.available = false
	if _, ok := fake.Solve(context.Background(), "example.test"); ok {
		t.Error("an unavailable solver must not report success")
	}
	if err := fake.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// TestMockChallengeServerChallengesTwice verifies the mock actually scripts a
// challenge, since every remedy-ordering test depends on it.
func TestMockChallengeServerChallengesTwice(t *testing.T) {
	server := newMockChallengeServer(t, challengeScript(t)...)
	for i := 0; i < 3; i++ {
		resp, err := http.Get(server.URL + "/series/1")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}
	if server.count() != 3 {
		t.Fatalf("recorded %d requests, want 3", server.count())
	}
	if got := server.requestAt(0).Path; got != "/series/1" {
		t.Errorf("recorded path = %q", got)
	}
}
