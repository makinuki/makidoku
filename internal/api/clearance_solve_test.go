package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/settings"
	"github.com/makinuki/makidoku/internal/solver"
)

// fakeChallenger stands in for the embedded browser so the route is exercised
// without a window or a network.
type fakeChallenger struct {
	// availableErr is returned by Available. A non-nil value means no browser can
	// be presented at all.
	availableErr error
	// result is what Solve returns.
	result *solver.Result
	// solveErr is returned alongside a nil result.
	solveErr error
	// hold blocks Solve until it is closed, which lets a test observe a second
	// request arriving while the first is still open.
	hold chan struct{}

	mu      sync.Mutex
	targets []string
}

func (f *fakeChallenger) Available(context.Context) error {
	return f.availableErr
}

func (f *fakeChallenger) Solve(_ context.Context, target string) (*solver.Result, error) {
	f.mu.Lock()
	f.targets = append(f.targets, target)
	f.mu.Unlock()
	if f.hold != nil {
		<-f.hold
	}
	return f.result, f.solveErr
}

func (f *fakeChallenger) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.targets...)
}

// solveFixture builds a server over one source whose base address is baseURL, so
// the route has an origin to present and a site to probe.
func solveFixture(t *testing.T, baseURL string) (chi.Router, *Server) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "solve.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })
	repo := db.NewRepository(handle)
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'multi',?,'s.wasm',1)`, baseURL); err != nil {
		t.Fatal(err)
	}
	server := NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()}), newFakeDownloads())
	server.SetSettings(settings.New(repo))
	router := chi.NewRouter()
	server.Mount(router)
	return router, server
}

// postSolve runs one solve request and decodes the answer. A failure is returned
// as a plain error body, so a decode failure is not itself a problem for the
// cases that assert on the status alone.
func postSolve(router chi.Router, body string) (int, solveResponse) {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/sources/s/clearance/solve", reader))
	var payload solveResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	return recorder.Code, payload
}

// plainPage serves an ordinary document, which is what a site that is not
// challenging returns.
func plainPage(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>Source</h1></body></html>"))
	}))
	t.Cleanup(server.Close)
	return server
}

// challengingPage serves an interstitial, which is what a site that is still
// holding a request back returns.
func challengingPage(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Just a moment..."))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSolveReportsUnavailableWhenNoChallengerIsAttached(t *testing.T) {
	router, _ := solveFixture(t, "https://source.test")

	status, _ := postSolve(router, "")

	if status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", status, http.StatusServiceUnavailable)
	}
}

func TestSolveReportsUnavailableWhenTheBrowserIsMissing(t *testing.T) {
	router, server := solveFixture(t, "https://source.test")
	server.SetChallenger(&fakeChallenger{availableErr: errors.New("no browser runtime")})

	status, _ := postSolve(router, "")

	if status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", status, http.StatusServiceUnavailable)
	}
}

func TestSolveTakesTheOriginFromTheSourceBaseAddress(t *testing.T) {
	router, server := solveFixture(t, "https://Source.Test/list")
	challenger := &fakeChallenger{result: &solver.Result{}}
	server.SetChallenger(challenger)

	postSolve(router, "")

	calls := challenger.calls()
	if len(calls) != 1 {
		t.Fatalf("Solve called %d times, want 1", len(calls))
	}
	if calls[0] != "https://source.test/" {
		t.Fatalf("Solve target=%q, want the bare origin of the base address", calls[0])
	}
}

func TestSolvePrefersTheOriginInTheBody(t *testing.T) {
	router, server := solveFixture(t, "https://source.test")
	challenger := &fakeChallenger{result: &solver.Result{}}
	server.SetChallenger(challenger)

	postSolve(router, `{"origin":"other.test"}`)

	calls := challenger.calls()
	if len(calls) != 1 || calls[0] != "https://other.test/" {
		t.Fatalf("Solve targets=%v, want the origin from the body", calls)
	}
}

func TestSolveReportsASiteThatDidNotChallenge(t *testing.T) {
	router, server := solveFixture(t, "https://source.test")
	server.SetChallenger(&fakeChallenger{result: &solver.Result{Captured: false}})

	status, payload := postSolve(router, "")

	if status != http.StatusOK {
		t.Fatalf("status=%d, want %d", status, http.StatusOK)
	}
	if payload.Captured || payload.Verified {
		t.Fatalf("captured=%v verified=%v, want neither", payload.Captured, payload.Verified)
	}
	if payload.NeedsInteraction {
		t.Fatal("needsInteraction is set for a site that served a document")
	}
	if !strings.Contains(payload.Message, "without challenging") {
		t.Fatalf("message=%q, want the site-served wording", payload.Message)
	}
}

func TestSolveReportsAnUnansweredChallenge(t *testing.T) {
	router, server := solveFixture(t, "https://source.test")
	server.SetChallenger(&fakeChallenger{
		result: &solver.Result{Captured: false, NeedsInteraction: true},
	})

	status, payload := postSolve(router, "")

	if status != http.StatusOK {
		t.Fatalf("status=%d, want %d", status, http.StatusOK)
	}
	if !payload.NeedsInteraction {
		t.Fatal("needsInteraction is not set for a challenge that went unanswered")
	}
	if payload.Captured || payload.Verified {
		t.Fatalf("captured=%v verified=%v, want neither", payload.Captured, payload.Verified)
	}
}

func TestSolveVerifiesThroughTheEngine(t *testing.T) {
	// The probe reaches the source base address, so a server answering with an
	// ordinary document is what makes verification succeed.
	site := plainPage(t)
	router, server := solveFixture(t, site.URL)
	server.SetChallenger(&fakeChallenger{result: &solver.Result{
		Captured: true,
		Capture: solver.Capture{
			Cookies:   map[string]string{"cf_clearance": "material", "session": "material"},
			UserAgent: "agent",
		},
	}})

	status, payload := postSolve(router, "")

	if status != http.StatusOK {
		t.Fatalf("status=%d, want %d", status, http.StatusOK)
	}
	if !payload.Captured {
		t.Fatal("captured is false after a capture")
	}
	if !payload.Verified {
		t.Fatal("verified is false after a capture the site accepted")
	}
	want := []string{"cf_clearance", "session"}
	if !reflect.DeepEqual(payload.Cookies, want) {
		t.Fatalf("cookies=%v, want %v in sorted order", payload.Cookies, want)
	}
}

func TestSolveDoesNotReportCaptureAloneAsSuccess(t *testing.T) {
	// The site keeps challenging after the cookie is stored, so the material was
	// read and the site is still refusing requests. Reporting that as a cleared
	// site would be wrong.
	site := challengingPage(t)
	router, server := solveFixture(t, site.URL)
	server.SetChallenger(&fakeChallenger{result: &solver.Result{
		Captured: true,
		Capture:  solver.Capture{Cookies: map[string]string{"cf_clearance": "material"}},
	}})

	status, payload := postSolve(router, "")

	if status != http.StatusOK {
		t.Fatalf("status=%d, want %d", status, http.StatusOK)
	}
	if !payload.Captured {
		t.Fatal("captured is false after a capture")
	}
	if payload.Verified {
		t.Fatal("verified is true for a site that is still challenging")
	}
	if !strings.Contains(payload.Message, "still refusing") {
		t.Fatalf("message=%q, want the not-yet-cleared wording", payload.Message)
	}
}

func TestSolveStoresTheCapturedBundle(t *testing.T) {
	site := plainPage(t)
	router, server := solveFixture(t, site.URL)
	server.SetChallenger(&fakeChallenger{result: &solver.Result{
		Captured: true,
		Capture: solver.Capture{
			Cookies:   map[string]string{"cf_clearance": "material"},
			UserAgent: "captured-agent",
		},
	}})

	postSolve(router, "")

	bundles, err := server.engine.ClearanceBundles("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 {
		t.Fatalf("stored %d bundles, want 1", len(bundles))
	}
	if !bundles[0].HasClearance {
		t.Fatal("hasClearance is false after a capture")
	}
	if bundles[0].Status != "usable" {
		t.Fatalf("status=%q, want usable after the site accepted the material", bundles[0].Status)
	}
	if want := []string{"cf_clearance"}; !reflect.DeepEqual(bundles[0].Cookies, want) {
		t.Fatalf("stored cookies=%v, want %v", bundles[0].Cookies, want)
	}
	if bundles[0].UserAgent != "captured-agent" {
		t.Fatalf("userAgent=%q, want the identity the solve ran under", bundles[0].UserAgent)
	}
}

func TestSolveNeverReturnsCookieValues(t *testing.T) {
	site := plainPage(t)
	router, server := solveFixture(t, site.URL)
	server.SetChallenger(&fakeChallenger{result: &solver.Result{
		Captured: true,
		Capture:  solver.Capture{Cookies: map[string]string{"cf_clearance": "secret-value"}},
	}})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/sources/s/clearance/solve", nil))

	if strings.Contains(recorder.Body.String(), "secret-value") {
		t.Fatal("the response body carries a cookie value")
	}
}

func TestSolveRefusesASecondSolveForAnOpenWindow(t *testing.T) {
	router, server := solveFixture(t, "https://source.test")
	challenger := &fakeChallenger{result: &solver.Result{}, hold: make(chan struct{})}
	server.SetChallenger(challenger)

	const callers = 4
	codes := make([]int, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for slot := 0; slot < callers; slot++ {
		go func(index int) {
			defer wg.Done()
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/sources/s/clearance/solve", nil))
			codes[index] = recorder.Code
		}(slot)
	}

	// The first caller is parked inside Solve. The others must be refused rather
	// than queueing a second window for the same origin.
	waitForCalls(t, challenger, 1)
	time.Sleep(50 * time.Millisecond)
	close(challenger.hold)
	wg.Wait()

	accepted, refused := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			accepted++
		case http.StatusConflict:
			refused++
		default:
			t.Fatalf("status=%d, want %d or %d", code, http.StatusOK, http.StatusConflict)
		}
	}
	if accepted != 1 {
		t.Fatalf("%d solves were accepted, want 1 while a window is open", accepted)
	}
	if refused != callers-1 {
		t.Fatalf("%d solves were refused, want %d", refused, callers-1)
	}
	if calls := len(challenger.calls()); calls != 1 {
		t.Fatalf("Solve called %d times, want 1 while a window is open", calls)
	}
}

// waitForCalls blocks until the challenger has recorded at least want calls.
func waitForCalls(t *testing.T, challenger *fakeChallenger, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(challenger.calls()) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Solve was called %d times, want at least %d", len(challenger.calls()), want)
}

func TestTargetForKeepsSchemeAndPort(t *testing.T) {
	cases := []struct {
		raw        string
		wantOrigin string
		wantURL    string
	}{
		{"https://source.test/list", "source.test", "https://source.test/"},
		{"Source.Test", "source.test", "https://source.test/"},
		{"http://127.0.0.1:8080", "127.0.0.1:8080", "http://127.0.0.1:8080/"},
	}
	for _, c := range cases {
		target, err := targetFor(c.raw)
		if err != nil {
			t.Fatalf("%q: %v", c.raw, err)
		}
		if target.origin != c.wantOrigin || target.url != c.wantURL {
			t.Fatalf("%q gave origin=%q url=%q, want origin=%q url=%q",
				c.raw, target.origin, target.url, c.wantOrigin, c.wantURL)
		}
	}
}

func TestTargetForRejectsAnAddressWithNoHost(t *testing.T) {
	if _, err := targetFor("   "); err == nil {
		t.Fatal("a blank origin was accepted")
	}
	if _, err := targetFor("https://"); err == nil {
		t.Fatal("a scheme with no host was accepted")
	}
}
