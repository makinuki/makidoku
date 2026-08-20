package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// challengeBody imitates a Cloudflare interstitial.
const challengeBody = `<!DOCTYPE html><title>Just a moment...</title><div id="cf-challenge-platform"></div>`

// resolverFunc adapts a function to ChallengeResolver.
type resolverFunc func(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool

func (f resolverFunc) Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
	return f(ctx, sourceID, usedCookie, challenge)
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

	storage := NewMemoryStorage()
	resolver := resolverFunc(func(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool {
		if usedCookie != "" {
			t.Errorf("the blocked attempt used cookie %q, want none", usedCookie)
		}
		if err := storage.Set(sourceID, ClearanceCookieKey, "solved"); err != nil {
			t.Error(err)
		}
		if err := storage.Set(sourceID, ClearanceUserAgentKey, "cleared-agent"); err != nil {
			t.Error(err)
		}
		return true
	})

	fetcher := NewFetcher(storage, resolver)
	resp, herr := fetcher.Do(context.Background(), "asurascans", HttpRequest{URL: server.URL})
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

func TestFetchAppliesDefaultUserAgent(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("User-Agent")
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if got := <-seen; got != DefaultUserAgent {
		t.Fatalf("user agent = %q, want %q", got, DefaultUserAgent)
	}

	// A plugin supplied agent is preserved when no clearance is stored.
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
	if isChallenge(&HttpResponse{Status: http.StatusServiceUnavailable, Body: "upstream is down"}) {
		t.Fatal("a plain 503 must reach the plugin as a response")
	}
	if !isChallenge(&HttpResponse{Status: http.StatusServiceUnavailable, Body: challengeBody}) {
		t.Fatal("a 503 carrying challenge markers is an interstitial")
	}
	if !isChallenge(&HttpResponse{
		Status:  http.StatusServiceUnavailable,
		Headers: map[string]string{"cf-mitigated": "challenge"},
	}) {
		t.Fatal("a mitigation header marks an interstitial")
	}
	if !isChallenge(&HttpResponse{Status: http.StatusForbidden}) {
		t.Fatal("a 403 is treated as a challenge")
	}
}
