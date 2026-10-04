package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// correlated builds the middleware pair the daemon installs, in the same order,
// so a test exercises the real chain rather than a reconstruction of it.
func correlated(t *testing.T, handler http.HandlerFunc) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(Correlate)
	router.Get("/library", handler)
	return router
}

// The identifier has to appear on both the request and the response record, since
// one without the other cannot be matched up.
func TestCorrelateRecordsBothEnds(t *testing.T) {
	logs := captureAPILogs(t)
	handler := correlated(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := logs.String()
	if !strings.Contains(out, "api request") || !strings.Contains(out, "api response") {
		t.Fatalf("expected a record for each end of the request; records were:\n%s", out)
	}
	if !strings.Contains(out, "status=418") {
		t.Fatalf("the response record did not carry the status; records were:\n%s", out)
	}
	if !strings.Contains(out, "path=/library") {
		t.Fatalf("the records did not carry the path; records were:\n%s", out)
	}
}

// Both records must share one identifier, since that is the entire purpose.
func TestCorrelateUsesOneIdentifierForBothRecords(t *testing.T) {
	logs := captureAPILogs(t)
	handler := correlated(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/library", nil))

	var ids []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if i := strings.Index(line, "request_id="); i >= 0 {
			rest := line[i+len("request_id="):]
			if j := strings.IndexAny(rest, " \t"); j >= 0 {
				rest = rest[:j]
			}
			ids = append(ids, rest)
		}
	}
	if len(ids) < 2 {
		t.Fatalf("expected an identifier on both records, found %d; records were:\n%s", len(ids), logs.String())
	}
	for _, id := range ids {
		if id == "" {
			t.Fatal("an empty identifier reached a record")
		}
		if id != ids[0] {
			t.Fatalf("records carried different identifiers: %q and %q", ids[0], id)
		}
	}
}

// The identifier is echoed so a person can quote it from a browser or a frontend
// log and have it match the record here.
func TestCorrelateEchoesTheIdentifier(t *testing.T) {
	handler := correlated(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/library", nil))

	if got := rec.Header().Get(requestIDHeader); got == "" {
		t.Fatal("the identifier was not echoed to the caller")
	}
}

// A handler that writes no explicit status must still be recorded as 200, since
// an absent status in the log is what makes a failure hard to read.
func TestCorrelateRecordsAnImplicitSuccess(t *testing.T) {
	logs := captureAPILogs(t)
	handler := correlated(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/library", nil))

	if !strings.Contains(logs.String(), "status=200") {
		t.Fatalf("an implicit success was not recorded as 200; records were:\n%s", logs.String())
	}
}

// The identifier has to reach the handler, otherwise the records beneath the
// request cannot be tied to it.
func TestCorrelateReachesTheHandler(t *testing.T) {
	var seen string
	handler := correlated(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = middleware.GetReqID(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/library", nil))

	if seen == "" {
		t.Fatal("the identifier did not reach the handler")
	}
	if seen != rec.Header().Get(requestIDHeader) {
		t.Fatalf("the handler saw %q but the response echoed %q", seen, rec.Header().Get(requestIDHeader))
	}
}

// LogValue must render nothing when there is no identifier, rather than an empty
// field that reads as a real value.
func TestLogValueIsEmptyWithoutAnIdentifier(t *testing.T) {
	if got := LogValue(context.Background()); got.Key != "" {
		t.Fatalf("LogValue without a request produced %v", got)
	}
}

func TestLogValueRendersTheIdentifier(t *testing.T) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	var attr slog.Attr
	router.Get("/library", func(w http.ResponseWriter, r *http.Request) {
		attr = LogValue(r.Context())
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/library", nil))

	if attr.Key != "request_id" {
		t.Fatalf("LogValue produced key %q, want request_id", attr.Key)
	}
	if attr.Value.String() == "" {
		t.Fatal("LogValue produced an empty identifier")
	}
}

// captureAPILogs points the default logger at a buffer for the duration of a test.
func captureAPILogs(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}
