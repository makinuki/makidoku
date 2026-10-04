package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// requestIDHeader is echoed to the caller so a request observed in the frontend
// log can be quoted in a report and found here under the same name.
const requestIDHeader = "X-Request-Id"

// The identifier itself comes from chi's existing RequestID middleware rather than
// a second mechanism of its own. Two identifiers on one request would mean a
// record carrying whichever one its author happened to read, and the failure this
// serves is already ambiguous enough.
//
// LogValue renders the identifier as a record field, so the field name is written
// in one place.
func LogValue(ctx context.Context) slog.Attr {
	if id := middleware.GetReqID(ctx); id != "" {
		return slog.String("request_id", id)
	}
	return slog.Attr{}
}

// Correlate records each inbound request and its outcome once, carrying the
// request identifier from chi so the records beneath it can be tied together. A
// refresh that spans several upstream calls otherwise leaves several records with
// nothing to link them to one operation.
//
// It must be installed after middleware.RequestID.
func Correlate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.GetReqID(r.Context())
		ctx := r.Context()
		if id != "" {
			w.Header().Set(requestIDHeader, id)
		}

		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		slog.InfoContext(ctx, "api request",
			"request_id", id, "method", r.Method, "path", r.URL.Path)

		next.ServeHTTP(recorder, r.WithContext(ctx))

		slog.InfoContext(ctx, "api response",
			"request_id", id, "method", r.Method, "path", r.URL.Path,
			"status", recorder.status, "elapsed_ms", time.Since(started).Milliseconds())
	})
}

// statusRecorder remembers the status a handler wrote, which is otherwise not
// observable without wrapping the writer.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(status int) {
	if !s.written {
		s.status = status
		s.written = true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(p)
}

// Unwrap lets the middleware chain below this one reach the original writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
