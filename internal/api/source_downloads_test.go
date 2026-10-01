package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/settings"
)

// downloadsPrefsFixture builds a server over one installed source whose plugin
// cannot load, so the hint layer of the response stays silent and the test
// covers the graceful degradation. The returned recorder captures queue-side
// calls such as policy invalidation.
func downloadsPrefsFixture(t *testing.T) (chi.Router, *fakeDownloads, *db.Repository) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "source-downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'multi','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	downloads := newFakeDownloads()
	server := NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()}), downloads)
	server.SetSettings(settings.New(repo))
	router := chi.NewRouter()
	server.Mount(router)
	return router, downloads, repo
}

func doJSON(t *testing.T, handler http.Handler, method, target, body string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, target, strings.NewReader(body)))
	return recorder.Code, recorder.Body.String()
}

func TestSourceDownloadsReadAndOverride(t *testing.T) {
	router, downloads, repo := downloadsPrefsFixture(t)

	// Without an override the response carries silence for the user layer
	// and the global policy the fake queue reports.
	code, body := doJSON(t, router, http.MethodGet, "/api/sources/s/downloads", "")
	if code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", code, body)
	}
	var view sourceDownloadsResponse
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	if view.Override.IntervalMs != nil || view.Override.MaxAttempts != nil || view.Override.BackoffMs != nil {
		t.Fatalf("override before storing = %+v", view.Override)
	}
	if view.Hint.IntervalMs != nil || view.Hint.MaxAttempts != nil || view.Hint.BackoffMs != nil || view.Hint.Burst != nil {
		t.Fatalf("hint from an unloadable source should stay silent, got %+v", view.Hint)
	}
	if view.Defaults.IntervalMs == nil || *view.Defaults.IntervalMs != 500 ||
		view.Defaults.MaxAttempts == nil || *view.Defaults.MaxAttempts != 3 ||
		view.Defaults.BackoffMs == nil || *view.Defaults.BackoffMs != 1000 ||
		view.Defaults.Burst == nil || *view.Defaults.Burst != 3 {
		t.Fatalf("defaults = %+v", view.Defaults)
	}

	// Storing an override persists it, echoes it back and drops the cached
	// queue policy so the next chapter uses the new pacing.
	code, body = doJSON(t, router, http.MethodPut, "/api/sources/s/downloads", `{"intervalMs":120,"maxAttempts":5,"burst":4}`)
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	if view.Override.IntervalMs == nil || *view.Override.IntervalMs != 120 || view.Override.MaxAttempts == nil || *view.Override.MaxAttempts != 5 ||
		view.Override.Burst == nil || *view.Override.Burst != 4 {
		t.Fatalf("stored override = %+v", view.Override)
	}
	if downloads.invalidated != "s" {
		t.Fatalf("policy invalidation recorded = %q", downloads.invalidated)
	}
	stored, err := repo.GetSourceDownloadPrefs("s")
	if err != nil || stored == nil || stored.IntervalMs == nil || *stored.IntervalMs != 120 {
		t.Fatalf("stored prefs = %+v, %v", stored, err)
	}

	// A field the client explicitly nulls returns to following the source.
	code, body = doJSON(t, router, http.MethodPut, "/api/sources/s/downloads", `{"maxAttempts":5}`)
	if code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body = %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	if view.Override.IntervalMs != nil || view.Override.MaxAttempts == nil || *view.Override.MaxAttempts != 5 || view.Override.Burst != nil {
		t.Fatalf("replaced override = %+v", view.Override)
	}

	// An entirely empty override removes the row.
	code, body = doJSON(t, router, http.MethodPut, "/api/sources/s/downloads", `{}`)
	if code != http.StatusOK {
		t.Fatalf("clearing PUT status = %d, body = %s", code, body)
	}
	if prefs, err := repo.GetSourceDownloadPrefs("s"); err != nil || prefs != nil {
		t.Fatalf("prefs after clearing = %+v, %v", prefs, err)
	}
}

func TestSourceDownloadsRejectsInvalidOverrides(t *testing.T) {
	router, _, repo := downloadsPrefsFixture(t)

	for _, body := range []string{
		`{"intervalMs":-1}`,
		`{"intervalMs":3600001}`,
		`{"maxAttempts":-1}`,
		`{"maxAttempts":11}`,
		`{"backoffMs":-5}`,
		`{"backoffMs":60001}`,
		`{"burst":0}`,
		`{"burst":17}`,
	} {
		code, _ := doJSON(t, router, http.MethodPut, "/api/sources/s/downloads", body)
		if code != http.StatusBadRequest {
			t.Fatalf("PUT %s status = %d, want 400", body, code)
		}
	}
	if prefs, err := repo.GetSourceDownloadPrefs("s"); err != nil || prefs != nil {
		t.Fatalf("rejected writes stored prefs = %+v, %v", prefs, err)
	}

	// An unknown source cannot receive an override.
	code, _ := doJSON(t, router, http.MethodPut, "/api/sources/ghost/downloads", `{"intervalMs":500}`)
	if code == http.StatusOK || code == http.StatusInternalServerError {
		t.Fatalf("unknown source PUT status = %d, want a client-visible error", code)
	}
}

func TestRetryFailedDownloads(t *testing.T) {
	downloads := newFakeDownloads()
	downloads.retryFailed = 2
	handler := downloadRouter(downloads)

	code, body := doJSON(t, handler, http.MethodPost, "/api/download/retry-failed", `{"sourceId":"mangadex"}`)
	if code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", code, body)
	}
	var result struct {
		Retried int `json:"retried"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if result.Retried != 2 || downloads.retryFailedSource != "mangadex" {
		t.Fatalf("result = %+v, source = %q", result, downloads.retryFailedSource)
	}

	if code, _ := doJSON(t, handler, http.MethodPost, "/api/download/retry-failed", `{}`); code != http.StatusBadRequest {
		t.Fatalf("missing sourceId status = %d, want 400", code)
	}
}

// Pausing one source answers with the updated snapshot whose paused list
// names the source, so open clients flip their per-source control without a
// refetch; a resume clears it again.
func TestPauseAndResumeSourceDownloads(t *testing.T) {
	downloads := newFakeDownloads()
	handler := downloadRouter(downloads)

	code, body := doJSON(t, handler, http.MethodPost, "/api/download/sources/mangadex/pause", "")
	if code != http.StatusOK {
		t.Fatalf("pause status = %d, body = %s", code, body)
	}
	if downloads.pausedSource != "mangadex" {
		t.Fatalf("paused source = %q", downloads.pausedSource)
	}
	var snapshot downloadSnapshot
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PausedSources) != 1 || snapshot.PausedSources[0] != "mangadex" {
		t.Fatalf("snapshot paused sources = %v", snapshot.PausedSources)
	}

	code, body = doJSON(t, handler, http.MethodPost, "/api/download/sources/mangadex/resume", "")
	if code != http.StatusOK {
		t.Fatalf("resume status = %d, body = %s", code, body)
	}
	if downloads.resumedSource != "mangadex" {
		t.Fatalf("resumed source = %q", downloads.resumedSource)
	}
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PausedSources) != 0 {
		t.Fatalf("snapshot after resume = %v", snapshot.PausedSources)
	}
}
