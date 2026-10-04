package engine

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureLogs points the default logger at a buffer and returns it, so a test can
// assert on the records one request produced.
func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// The field set is what a failed refresh is diagnosed from, so every field a
// report needs has to be present on one request. A missing field makes the record
// useless exactly when it is read.
func TestFetchLogsStatusDurationAndClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	logs := captureLogs(t, slog.LevelDebug)
	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL + "/page"}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}

	out := logs.String()
	for _, field := range []string{
		"source=mangadex", "status=200", "elapsed_ms=", "bytes=",
		"classification=none", "method=GET", "url=",
	} {
		if !strings.Contains(out, field) {
			t.Fatalf("the request record is missing %q; record was:\n%s", field, out)
		}
	}
}

// A challenge must be visible in the log without the owner raising the level,
// since that is the case a report has to contain. The level here is info, so a
// debug record would be dropped and the test would fail.
func TestFetchRaisesAChallengeToInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(challengeBody))
	}))
	defer server.Close()

	logs := captureLogs(t, slog.LevelInfo)
	fetcher := NewFetcher(NewMemoryStorage(), nil)
	_, _ = fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL + "/page"})

	out := logs.String()
	if !strings.Contains(out, "source challenged") {
		t.Fatalf("a challenge was not raised to info; record was:\n%s", out)
	}
	if !strings.Contains(out, "status=403") {
		t.Fatalf("the challenge record did not carry the status; record was:\n%s", out)
	}
}

// An ordinary success must stay at debug, because reading a chapter produces a
// great many of them and an info line per image would drown the file.
func TestFetchKeepsAnOrdinarySuccessAtDebug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	logs := captureLogs(t, slog.LevelInfo)
	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL + "/page"}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}

	if strings.Contains(logs.String(), "source request") {
		t.Fatal("an ordinary 200 was recorded above debug")
	}
}

// The end-to-end guarantee: a clearance value reaching a record would be a leak
// in the artefact most likely to be attached to a bug report. This checks the
// whole path rather than the redaction helper alone.
func TestFetchNeverLogsClearanceMaterial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The resolver would normally hand over real clearance. Here the value is
		// planted in the response so that anything writing a response header would
		// carry it into the log.
		w.Header().Set("Set-Cookie", "cf_clearance=LEAKCANDIDATE99887766")
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	logPath := filepath.Join(t.TempDir(), "fetch.log")
	sink, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("creating the log sink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	if _, herr := fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: server.URL + "/page"}); herr != nil {
		t.Fatalf("host error: %+v", herr)
	}
	if err := sink.Sync(); err != nil {
		t.Fatalf("flushing the log sink: %v", err)
	}

	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if bytes.Contains(contents, []byte("LEAKCANDIDATE")) {
		t.Fatalf("clearance material reached the log file:\n%s", contents)
	}
}

// A transport failure is recorded with its elapsed time, because diagnosing a
// fast failure depends on knowing it was fast.
func TestFetchLogsATransportFailure(t *testing.T) {
	logs := captureLogs(t, slog.LevelWarn)
	fetcher := NewFetcher(NewMemoryStorage(), nil)

	// A closed listener refuses at once, which is the local short-circuit shape.
	refused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	refusedURL := refused.URL
	refused.Close()

	_, _ = fetcher.Do(context.Background(), "mangadex", HttpRequest{URL: refusedURL + "/page"})

	out := logs.String()
	if !strings.Contains(out, "source request failed") {
		t.Fatalf("a transport failure was not recorded; record was:\n%s", out)
	}
	if !strings.Contains(out, "elapsed_ms=") {
		t.Fatalf("the failure record did not carry the duration; record was:\n%s", out)
	}
}

// elapsedMs must render a whole millisecond count so a slow request and a fast
// refusal can be compared at a glance.
func TestElapsedMsIsNonNegative(t *testing.T) {
	if got := elapsedMs(time.Now().Add(-5 * time.Millisecond)); got < 0 {
		t.Fatalf("elapsedMs = %d, want a non-negative duration", got)
	}
}
