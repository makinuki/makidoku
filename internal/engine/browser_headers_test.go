package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/settings"
)

func TestClassifyRequest(t *testing.T) {
	cases := []struct {
		name   string
		accept string
		want   requestKind
	}{
		{"absent accept is a document", "", kindDocument},
		{"document set", defaultAccept, kindDocument},
		{"html fragment", "text/html,*/*;q=0.8", kindDocument},
		{"image set", "image/avif,image/webp,image/apng,*/*;q=0.8", kindImage},
		{"wildcard image", "image/*", kindImage},
		{"json api", "application/json", kindFetch},
		{"wildcard", "*/*", kindFetch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRequest(tc.accept); got != tc.want {
				t.Fatalf("kind = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestClientHintsFollowTheAgent(t *testing.T) {
	cases := []struct {
		name     string
		agent    string
		version  string
		platform string
		mobile   string
	}{
		{"default", settings.DefaultUserAgent, "154", "Windows", "?0"},
		{
			"macos",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36",
			"154", "macOS", "?0",
		},
		{
			"android",
			"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Mobile Safari/537.36",
			"154", "Android", "?1",
		},
		{
			"chrome os",
			"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36",
			"154", "Chrome OS", "?0",
		},
		{
			"linux",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36",
			"154", "Linux", "?0",
		},
		{
			"a non chromium agent gets none",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:130.0) Gecko/20100101 Firefox/130.0",
			"", "", "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hints := clientHints(tc.agent)
			if tc.version == "" {
				if len(hints) != 0 {
					t.Fatalf("hints = %v, want none for a non Chromium agent", hints)
				}
				return
			}
			want := `"Chromium";v="` + tc.version + `", "Not A(Brand";v="99", "Google Chrome";v="` + tc.version + `"`
			if got := hints["Sec-Ch-Ua"]; got != want {
				t.Errorf("Sec-Ch-Ua = %q, want %q", got, want)
			}
			if got, want := hints["Sec-Ch-Ua-Platform"], `"`+tc.platform+`"`; got != want {
				t.Errorf("Sec-Ch-Ua-Platform = %q, want %q", got, want)
			}
			if got := hints["Sec-Ch-Ua-Mobile"]; got != tc.mobile {
				t.Errorf("Sec-Ch-Ua-Mobile = %q, want %q", got, tc.mobile)
			}
		})
	}
}

// A plugin's Accept decides the shape of the request, so a page, an image, and
// an API call are each presented the way a browser presents them.
func TestFetchPresentsHeadersForTheRequestKind(t *testing.T) {
	seen := make(chan http.Header, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	// A base address on another host makes every request cross-site, which is
	// the shape an image CDN and an API host have.
	fetcher.SetBaseURL(testSourceID, "https://origin.test/")

	cases := []struct {
		name         string
		headers      map[string]string
		wantDest     string
		wantMode     string
		wantPriority string
		navigation   bool
	}{
		{name: "document", wantDest: "document", wantMode: "navigate", wantPriority: "u=0, i", navigation: true},
		{
			name:         "image",
			headers:      map[string]string{"Accept": "image/avif,image/webp,image/apng,*/*;q=0.8"},
			wantDest:     "image",
			wantMode:     "no-cors",
			wantPriority: "u=2, i",
		},
		{
			name:         "api",
			headers:      map[string]string{"Accept": "application/json"},
			wantDest:     "empty",
			wantMode:     "cors",
			wantPriority: "u=1, i",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
				URL:     server.URL + "/thing",
				Headers: tc.headers,
			}); herr != nil {
				t.Fatalf("host error: %+v", herr)
			}
			got := <-seen
			if tc.headers["Accept"] == "" && got.Get("Accept") != defaultAccept {
				t.Errorf("Accept = %q, want the document set", got.Get("Accept"))
			}
			if v := got.Get("Sec-Fetch-Dest"); v != tc.wantDest {
				t.Errorf("Sec-Fetch-Dest = %q, want %q", v, tc.wantDest)
			}
			if v := got.Get("Sec-Fetch-Mode"); v != tc.wantMode {
				t.Errorf("Sec-Fetch-Mode = %q, want %q", v, tc.wantMode)
			}
			if v := got.Get("Sec-Fetch-Site"); v != "cross-site" {
				t.Errorf("Sec-Fetch-Site = %q, want cross-site", v)
			}
			if v := got.Get("Priority"); v != tc.wantPriority {
				t.Errorf("Priority = %q, want %q", v, tc.wantPriority)
			}
			if tc.navigation {
				if v := got.Get("Sec-Fetch-User"); v != "?1" {
					t.Errorf("Sec-Fetch-User = %q, want ?1", v)
				}
				if v := got.Get("Upgrade-Insecure-Requests"); v != "1" {
					t.Errorf("Upgrade-Insecure-Requests = %q, want 1", v)
				}
				if v := got.Get("Cache-Control"); v != defaultCacheControl {
					t.Errorf("Cache-Control = %q, want %q", v, defaultCacheControl)
				}
			} else {
				for _, name := range []string{"Sec-Fetch-User", "Upgrade-Insecure-Requests", "Cache-Control"} {
					if v := got.Get(name); v != "" {
						t.Errorf("%s = %q, want it absent outside a navigation", name, v)
					}
				}
			}
			if v := got.Get("User-Agent"); v != settings.DefaultUserAgent {
				t.Errorf("User-Agent = %q, want the configured identity", v)
			}
			if v := got.Get("Sec-Ch-Ua"); !strings.Contains(v, `"Chromium";v="154"`) {
				t.Errorf("Sec-Ch-Ua = %q, want the hints derived from the agent", v)
			}
		})
	}
}

// A plugin that captured a real session keeps every header it set.
func TestFetchKeepsPluginSuppliedHeaders(t *testing.T) {
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	plugin := map[string]string{
		"Accept":          "application/json",
		"Accept-Language": "de-DE",
		"Sec-Fetch-Dest":  "iframe",
		"Sec-Fetch-Mode":  "nested-navigate",
		"Sec-Fetch-Site":  "cross-site",
		"Priority":        "u=7",
		"Sec-Ch-Ua":       `"Custom";v="1"`,
	}
	if _, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL:     server.URL,
		Headers: plugin,
	}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	got := <-seen
	for name, want := range plugin {
		if v := got.Get(name); v != want {
			t.Errorf("%s = %q, want the plugin value %q", name, v, want)
		}
	}
}

// The hints captured with a clearance belong to the agent that earned it, so
// they replace the hints derived from the current agent.
func TestFetchReplaysCapturedClientHints(t *testing.T) {
	const capturedUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36"
	captured := map[string]string{
		"Sec-Ch-Ua":          `"Chromium";v="154", "Brave";v="154", "Not A(Brand";v="99"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": `"Windows"`,
	}

	var requests int32
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&requests, 1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(challengeBody))
			return
		}
		seen <- r.Header.Clone()
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	broker, _ := newTestBroker(t, 0)
	resolver := &bundleResolver{broker: broker, install: func() {
		if err := broker.SubmitBundle(testSourceID, db.ClearanceBundle{
			Origin:         hostOnly(server.URL),
			Cookies:        map[string]string{"cf_clearance": "solved"},
			UserAgent:      capturedUA,
			SecChUa:        captured,
			BrowserProfile: DefaultBrowserProfile,
		}); err != nil {
			t.Fatalf("submit bundle: %v", err)
		}
	}}

	fetcher := NewFetcher(NewMemoryStorage(), resolver)
	if _, herr := fetcher.Do(context.Background(), testSourceID, HttpRequest{
		URL:     server.URL,
		Headers: map[string]string{"Accept": "application/json"},
	}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}

	got := <-seen
	if v := got.Get("User-Agent"); v != capturedUA {
		t.Errorf("User-Agent = %q, want the agent the clearance was issued to", v)
	}
	if v := got.Get("Sec-Ch-Ua"); v != captured["Sec-Ch-Ua"] {
		t.Errorf("Sec-Ch-Ua = %q, want the captured hint", v)
	}
	if v := got.Get("Sec-Ch-Ua-Platform"); v != captured["Sec-Ch-Ua-Platform"] {
		t.Errorf("Sec-Ch-Ua-Platform = %q, want the captured hint", v)
	}
}
