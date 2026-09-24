package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestManifestServedWithManifestContentType(t *testing.T) {
	rec := get(t, "/manifest.webmanifest")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /manifest.webmanifest: status %d, want 200", rec.Code)
	}
	// .webmanifest is absent from Go's builtin MIME table, so the handler
	// must set the type explicitly for installs to accept the manifest.
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}

func TestServiceWorkerServedNoCache(t *testing.T) {
	rec := get(t, "/sw.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sw.js: status %d, want 200", rec.Code)
	}
	// The worker script must revalidate on every load so a new binary's
	// worker takes effect instead of a stale script from the HTTP cache.
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want a JavaScript type", ct)
	}
}
