package engine

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/makinuki/makidoku/internal/settings"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

// challengeBody imitates a Cloudflare interstitial.
const challengeBody = `<!DOCTYPE html><title>Just a moment...</title><div id="cf-challenge-platform"></div>`

// resolverFunc adapts a function to ChallengeResolver.
type resolverFunc func(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool

func (f resolverFunc) Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
	return f(ctx, sourceID, usedCookie, challenge)
}

// Bundle reports no stored material unless the test installs one, so the
// resolver seam can be driven from a plain function.
func (f resolverFunc) Bundle(sourceID, origin string) *db.ClearanceBundle { return nil }

// MarkUsable and MarkChallenged are no-ops for the function seam.
func (f resolverFunc) MarkUsable(sourceID, origin string) error     { return nil }
func (f resolverFunc) MarkChallenged(sourceID, origin string) error { return nil }

// bundleResolver drives the real broker and installs material when a challenge
// is reported, so the replay path runs end to end instead of through a stubbed
// cookie.
type bundleResolver struct {
	broker  *ClearanceBroker
	test    *testing.T
	install func()
}

func (r *bundleResolver) Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
	if r.install != nil {
		r.install()
	}
	return r.broker.Resolve(ctx, sourceID, usedCookie, challenge)
}

func (r *bundleResolver) Bundle(sourceID, origin string) *db.ClearanceBundle {
	return r.broker.Bundle(sourceID, origin)
}

func (r *bundleResolver) MarkUsable(sourceID, origin string) error {
	return r.broker.MarkUsable(sourceID, origin)
}

func (r *bundleResolver) MarkChallenged(sourceID, origin string) error {
	return r.broker.MarkChallenged(sourceID, origin)
}

func TestFetchPassesUpstreamStatusThrough(t *testing.T) {
	// Status mapping belongs to the plugin, so a throttled response must reach
	// it as a response rather than as a host error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	resp, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL})
	if herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if resp.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", resp.Status, http.StatusTooManyRequests)
	}
	if resp.Headers["retry-after"] != "30" {
		t.Fatalf("headers = %v, want a lowercased retry-after", resp.Headers)
	}
	if resp.Body != "slow down" {
		t.Fatalf("body = %q", resp.Body)
	}
}

func TestFetchReportsChallengeWithoutResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	resp, herr := fetcher.Do(context.Background(), "asurascans", HttpRequest{URL: server.URL})
	if resp != nil {
		t.Fatal("a challenged request must not yield a response")
	}
	if herr == nil || herr.Error != CodeCloudflareBlocked {
		t.Fatalf("error = %+v, want %s", herr, CodeCloudflareBlocked)
	}
}

func TestFetchReplaysGetAfterClearance(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&requests, 1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(challengeBody))
			return
		}
		if !strings.Contains(r.Header.Get("Cookie"), "cf_clearance=solved") {
			t.Errorf("replay is missing the clearance cookie: %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("User-Agent") != "cleared-agent" {
			t.Errorf("replay must carry the agent the clearance was issued to, got %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	broker, _ := newTestBroker(t, 0)
	resolver := &bundleResolver{broker: broker, install: func() {
		if err := broker.Submit(testSourceID, hostOnly(server.URL), "solved", "cleared-agent"); err != nil {
			t.Fatalf("submit clearance: %v", err)
		}
	}}

	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	resp, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{URL: server.URL})
	if herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if resp.Body != "content" {
		t.Fatalf("body = %q, want %q", resp.Body, "content")
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("requests = %d, want exactly one replay", got)
	}
}

func TestFetchDoesNotReplayPost(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer server.Close()

	storage := NewMemoryStorage()
	resolved := false
	resolver := resolverFunc(func(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
		resolved = true
		return true
	})

	body := `{"login":"user"}`
	fetcher := NewFetcher(storage, resolver)
	_, herr := fetcher.Do(context.Background(), "asurascans", HttpRequest{
		URL:    server.URL,
		Method: "POST",
		Body:   &body,
	})
	if herr == nil || herr.Error != CodeCloudflareBlocked {
		t.Fatalf("error = %+v, want %s", herr, CodeCloudflareBlocked)
	}
	// A write must never be replayed, so the resolver is not even consulted.
	if resolved {
		t.Fatal("clearance resolution must not run for a POST")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestFetchRejectsRelativeURL(t *testing.T) {
	fetcher := NewFetcher(NewMemoryStorage(), nil)
	_, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: "/api/manga"})
	if herr == nil || herr.Error != CodeParsingError {
		t.Fatalf("error = %+v, want %s", herr, CodeParsingError)
	}
}

func TestFetchCapsResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("a", 1<<20)
		for i := 0; i < 17; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	_, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL})
	if herr == nil || herr.Error != CodeMemoryLimitExceeded {
		t.Fatalf("error = %+v, want %s", herr, CodeMemoryLimitExceeded)
	}
}

// An asset host that refuses a request without a referrer is a form of hotlink
// protection. The source's base address is sent as Referer and Origin, and a
// value the plugin set for itself is left alone.
func TestFetchSendsTheSourceBaseAsReferrerAndOrigin(t *testing.T) {
	type seenHeaders struct {
		referer string
		origin  string
	}
	got := make(chan seenHeaders, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seenHeaders{referer: r.Header.Get("Referer"), origin: r.Header.Get("Origin")}
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	fetcher.SetBaseURL("source", "https://gate.test/")

	if _, herr := fetcher.Do(context.Background(), "source", HttpRequest{
		URL: server.URL + "/img/1/2/3.jpg",
	}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}

	first := <-got
	if first.referer != "https://gate.test" {
		t.Fatalf("referer = %q, want the source base address", first.referer)
	}
	if first.origin != "https://gate.test" {
		t.Fatalf("origin = %q, want the scheme and host of the base address", first.origin)
	}

	if _, herr := fetcher.Do(context.Background(), "source", HttpRequest{
		URL:     server.URL + "/img/1/2/4.jpg",
		Headers: map[string]string{"Referer": "https://elsewhere.test/page"},
	}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if second := <-got; second.referer != "https://elsewhere.test/page" {
		t.Fatalf("referer = %q, want the plugin supplied value left alone", second.referer)
	}
}

// With no recorded base the request's own address is used, so the header is
// present rather than sometimes absent.
func TestFetchFallsBackToTheRequestAddressWithoutABase(t *testing.T) {
	referer := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		referer <- r.Header.Get("Referer")
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), "source", HttpRequest{URL: server.URL}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}

	if got := <-referer; got != server.URL {
		t.Fatalf("referer = %q, want the request address %q", got, server.URL)
	}
}

func TestFetchPresentsTheConfiguredBrowserIdentity(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("User-Agent")
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	// The host supplies no agent of its own, so the configured browser identity
	// is presented rather than the transport naming itself as Go-http-client.
	// The value is editable because a constant drifts out of date; the reader
	// changes it rather than the build being frozen at the wrong one.
	if got := <-seen; got != settings.DefaultUserAgent {
		t.Fatalf("user agent = %q, want the configured browser identity %q",
			got, settings.DefaultUserAgent)
	}

	// A configured agent replaces the default for later requests.
	fetcher.SetUserAgent("chosen-agent")
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if got := <-seen; got != "chosen-agent" {
		t.Fatalf("user agent = %q, want the configured agent", got)
	}

	// A plugin supplied agent is passed through unchanged.
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{
		URL:     server.URL,
		Headers: map[string]string{"User-Agent": "plugin-agent"},
	}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if got := <-seen; got != "plugin-agent" {
		t.Fatalf("user agent = %q, want %q", got, "plugin-agent")
	}
}

func TestUnwrapEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    ErrorCode
		data    string
	}{
		{name: "success", payload: `{"ok":true,"data":{"page":1}}`, data: `{"page":1}`},
		{name: "failure", payload: `{"ok":false,"error":{"code":"NOT_FOUND","message":"gone"}}`, want: CodeNotFound},
		{name: "unknown code", payload: `{"ok":false,"error":{"code":"KABOOM","message":"x"}}`, want: CodeParsingError},
		{name: "missing ok", payload: `{"data":{"page":1}}`, want: CodeParsingError},
		{name: "success without data", payload: `{"ok":true}`, want: CodeParsingError},
		{name: "failure without error", payload: `{"ok":false}`, want: CodeParsingError},
		{name: "not json", payload: `<html>blocked</html>`, want: CodeParsingError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := unwrapEnvelope("mangadex", ExportSearch, []byte(tc.payload))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if string(data) != tc.data {
					t.Fatalf("data = %s, want %s", data, tc.data)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := CodeOf(err); got != tc.want {
				t.Fatalf("code = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestStorageKeyAcceptsBothWireForms(t *testing.T) {
	if got := storageKey(`"session_token"`); got != "session_token" {
		t.Fatalf("JSON encoded key = %q", got)
	}
	if got := storageKey(`session_token`); got != "session_token" {
		t.Fatalf("bare key = %q", got)
	}
}

func TestIsChallengeDistinguishesOutageFromInterstitial(t *testing.T) {
	if needsChallengeResponse(http.StatusServiceUnavailable, nil, []byte("upstream is down")) {
		t.Fatal("an ordinary outage must not count as a challenge")
	}
	if !needsChallengeResponse(http.StatusServiceUnavailable, nil, []byte(challengeBody)) {
		t.Fatal("a 503 carrying challenge markers is an interstitial")
	}
	if !needsChallengeResponse(http.StatusServiceUnavailable, map[string]string{"cf-mitigated": "challenge"}, nil) {
		t.Fatal("a mitigation header marks an interstitial")
	}
	// A bare refusal carries no evidence of a challenge, so it is terminal
	// rather than solvable.
	if got := Classify(http.StatusForbidden, nil, nil); got != ClassTerminal {
		t.Fatalf("bare 403 class = %q, want %q", got, ClassTerminal)
	}
}

// TestWarmUpClearsChallengeWithoutResolve covers the warm-up remedy: an origin
// that challenges once and serves normally on the next visit is answered by the
// root request alone, with no clearance and no resolver involved.
func TestWarmUpClearsChallengeWithoutResolve(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(challengeBody))
			return
		}
		_, _ = w.Write([]byte("<title>ok</title>"))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	fetcher.SetBaseURL("mangafire", server.URL)

	resp, herr := fetcher.Do(context.Background(), "mangafire", HttpRequest{
		URL:    server.URL + "/series/1",
		Method: http.MethodGet,
	})
	if herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if resp.Status != http.StatusOK || !strings.Contains(resp.Body, "ok") {
		t.Fatalf("status = %d body = %q", resp.Status, resp.Body)
	}
	if states := fetcher.ChallengeStates(); len(states) != 0 {
		t.Fatalf("no challenge should be recorded, got %+v", states)
	}
}

// The warm-up settles a session; it does not answer the request. A source that
// serves documents from one host and images from another is challenged on the
// image host, and the warm-up must not hand the document host's page back as
// though it were the image.
func TestWarmUpDoesNotAnswerTheRequestFromAnotherHost(t *testing.T) {
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>document host home page</body></html>"))
	}))
	defer base.Close()

	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer images.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	fetcher.SetBaseURL("source", base.URL+"/")

	body, err := fetcher.FetchImage(context.Background(), "source", images.URL+"/img/1.jpg", nil)
	if err == nil {
		t.Fatalf("a challenged image must not resolve to a %d byte body from another host", len(body))
	}
	if strings.Contains(string(body), "document host home page") {
		t.Fatal("the document host's page was returned in place of the image")
	}
}

// The warm-up reaches the host that issued the challenge. Warming the source's
// base address instead touches a host that never answered with a challenge.
func TestWarmUpWarmsTheChallengedHost(t *testing.T) {
	warmed := make(chan string, 4)
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		warmed <- r.URL.Path
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte("<title>ok</title>"))
			return
		}
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer images.Close()

	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the document host must not be warmed for an image host challenge")
		_, _ = w.Write([]byte("<title>wrong host</title>"))
	}))
	defer base.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	fetcher.SetBaseURL("source", base.URL+"/")

	if _, err := fetcher.FetchImage(context.Background(), "source", images.URL+"/img/1.jpg", nil); err == nil {
		t.Fatal("the image was expected to stay blocked, the warm-up only proves it is challenged")
	}

	sawRoot := false
	for len(warmed) > 0 {
		if <-warmed == "/" {
			sawRoot = true
		}
	}
	if !sawRoot {
		t.Fatal("the challenged host was never warmed")
	}
}

// A warm origin answers the retry with the requested resource, not with the
// origin root.
func TestWarmUpSettlesTheSessionAndTheRequestIsMadeAgain(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Cf-Mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(challengeBody))
			return
		}
		if r.URL.Path == "/series/1" {
			_, _ = w.Write([]byte(`{"title":"the requested listing"}`))
			return
		}
		_, _ = w.Write([]byte("<title>origin root</title>"))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	fetcher.SetBaseURL("source", server.URL)

	resp, herr := fetcher.Do(context.Background(), "source", HttpRequest{
		URL:    server.URL + "/series/1",
		Method: http.MethodGet,
	})
	if herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if !strings.Contains(resp.Body, "the requested listing") {
		t.Fatalf("body = %q, want the requested listing rather than the origin root", resp.Body)
	}
}

// TestChallengeIsRecordedAndResolvedOnce checks that a challenge is surfaced
// and that repeated requests on one origin resolve it a single time.
func TestChallengeIsRecordedAndResolvedOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer server.Close()

	var resolves int32
	resolver := resolverFunc(func(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
		atomic.AddInt32(&resolves, 1)
		return false
	})

	// No base URL is recorded, so the warm-up is skipped and the resolver path
	// is exercised on its own.
	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	_, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL:    server.URL + "/series/1",
		Method: http.MethodGet,
	})
	if herr == nil || herr.Error != CodeCloudflareBlocked {
		t.Fatalf("error = %+v, want CLOUDFLARE_BLOCKED", herr)
	}

	states := fetcher.ChallengeStates()
	state, ok := states[hostKey(testSourceID, server.URL)]
	if !ok {
		t.Fatalf("no state recorded, got %+v", states)
	}
	if state.Hits != 1 {
		t.Fatalf("hits = %d, want 1", state.Hits)
	}

	_, _ = fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL:    server.URL + "/series/2",
		Method: http.MethodGet,
	})
	if got := atomic.LoadInt32(&resolves); got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
}

// TestJarSharedBetweenPageAndImage checks that a session cookie set while a
// page is fetched is present on a later image request for the same source.
func TestJarSharedBetweenPageAndImage(t *testing.T) {
	var sawCookie int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			if strings.Contains(r.Header.Get("Cookie"), "session=abc") {
				atomic.StoreInt32(&sawCookie, 1)
			}
		}
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL:    server.URL + "/series/1",
		Method: http.MethodGet,
	}); herr != nil {
		t.Fatalf("page request: %+v", herr)
	}
	if _, err := fetcher.FetchImage(context.Background(), testSourceID, server.URL+"/page.jpg", nil); err != nil {
		t.Fatalf("image request: %v", err)
	}
	if atomic.LoadInt32(&sawCookie) == 0 {
		t.Fatal("image request did not carry the session cookie from the page request")
	}
}

// TestTerminalBlockIsNeverReplayed checks that a refusal no challenge can clear
// reaches the plugin with its status intact and never enters the resolver.
func TestTerminalBlockIsNeverReplayed(t *testing.T) {
	var resolves int32
	resolver := resolverFunc(func(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
		atomic.AddInt32(&resolves, 1)
		return true
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<h1>You have been blocked</h1><p>Error 1020</p>"))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	fetcher.SetBaseURL("blocked", server.URL)

	resp, herr := fetcher.Do(context.Background(), "blocked", HttpRequest{
		URL:    server.URL + "/series/1",
		Method: http.MethodGet,
	})
	if herr != nil {
		t.Fatalf("a terminal refusal must reach the plugin, got %+v", herr)
	}
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.Status)
	}
	if got := atomic.LoadInt32(&resolves); got != 0 {
		t.Fatalf("resolver was called %d times for a terminal refusal", got)
	}
}

// TestDecodeBodyUnpacksEncodings checks that a compressed body reaches the
// plugin as text rather than as compressed bytes.
func TestDecodeBodyUnpacksEncodings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		encoding string
		encode   func(string) []byte
	}{
		{"gzip", "gzip", func(s string) []byte {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			_, _ = zw.Write([]byte(s))
			_ = zw.Close()
			return buf.Bytes()
		}},
		{"deflate", "deflate", func(s string) []byte {
			var buf bytes.Buffer
			zw := zlib.NewWriter(&buf)
			_, _ = zw.Write([]byte(s))
			_ = zw.Close()
			return buf.Bytes()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", tc.encoding)
				_, _ = w.Write(tc.encode("<title>decoded</title>"))
			}))
			defer server.Close()

			fetcher := NewFetcher(NewMemoryStorage(), nil)
			resp, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL})
			if herr != nil {
				t.Fatalf("host error: %+v", herr)
			}
			if !strings.Contains(resp.Body, "<title>decoded</title>") {
				t.Fatalf("body was not decompressed: %q", resp.Body)
			}
		})
	}
}

// hostKey mirrors the key the fetcher builds, which uses the hostname without
// the port so a source stays one entry across local and published addresses.
func hostKey(sourceID, raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return sourceID + "|" + raw
	}
	return sourceID + "|" + parsed.Hostname()
}

// TestClassifySeparatesTerminalFromSolvable pins the distinction that keeps a
// geographic refusal out of the solve path.
func TestClassifySeparatesTerminalFromSolvable(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		want    ChallengeClass
	}{
		{"interstitial", http.StatusForbidden, nil, challengeBody, ClassSolvable},
		{"geo block", http.StatusForbidden, nil, "Error 1020 You have been blocked", ClassTerminal},
		{"access denied", http.StatusForbidden, nil, "Access denied", ClassTerminal},
		{"rate limited", http.StatusTooManyRequests, map[string]string{"retry-after": "30"}, "slow down", ClassRateLimited},
		{"ok page", http.StatusOK, nil, "<title>Example</title>", ClassNone},
		{"ok page naming a challenge", http.StatusOK, nil,
			"<title>Example</title>ads will be disabled for 15 minutes in just a moment", ClassNone},
		{"script path is not a challenge", http.StatusOK, nil,
			"<script src='/cdn-cgi/challenge-platform/scripts/jsd/main.js'></script>", ClassNone},
		{"not found carrying a phrase", http.StatusNotFound, nil, "just a moment", ClassNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.status, tc.headers, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("class = %q, want %q", got, tc.want)
			}
		})
	}
}
