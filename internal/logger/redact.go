package logger

import (
	"log/slog"
	"net/http"
	"strings"
)

// Redacted is written in place of a value that must never reach the log. A
// clearance cookie is a credential, and a log file is the artefact most likely to
// be pasted into a bug report.
const Redacted = "[redacted]"

// secretHeaders are never written in full, whatever the level.
var secretHeaders = map[string]bool{
	"cookie":              true,
	"set-cookie":          true,
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
}

// SanitiseHeaders returns loggable header fields. Names are kept, because which
// header was present is part of the diagnosis, and values are kept only for
// headers that cannot carry a credential.
func SanitiseHeaders(header http.Header) []slog.Attr {
	if len(header) == 0 {
		return nil
	}
	out := make([]slog.Attr, 0, len(header))
	for name, values := range header {
		key := strings.ToLower(name)
		if secretHeaders[key] {
			out = append(out, slog.String(key, Redacted))
			continue
		}
		// A repeated header is joined the way the transport would send it.
		out = append(out, slog.String(key, strings.Join(values, ", ")))
	}
	return out
}

// SecretHeaderNames lists the header names that are redacted. The fetcher uses it
// to report that clearance was applied without reporting what it was.
func SecretHeaderNames() []string {
	return []string{"cookie", "set-cookie", "authorization"}
}

// HasSecretHeader reports whether a header set carries anything that must not be
// logged. It lets a record say that clearance was applied without naming the value.
func HasSecretHeader(header http.Header) bool {
	for name := range header {
		if secretHeaders[strings.ToLower(name)] {
			return true
		}
	}
	return false
}
