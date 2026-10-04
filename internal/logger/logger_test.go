package logger

import (
	"net/http"
	"strings"
	"testing"
)

// A clearance cookie is a credential. The log file is the artefact most likely to
// be pasted into a bug report, so a value must never survive redaction in any
// form, while the header name stays available for diagnosis.
func TestSanitiseHeadersRedactsCookieValues(t *testing.T) {
	const secret = "cf_clearance=SUPERSECRETVALUE123"
	header := http.Header{}
	header.Set("Cookie", secret)
	header.Set("Set-Cookie", "cf_clearance=ANOTHERSECRET")
	header.Set("User-Agent", "Mozilla/5.0")

	var joined strings.Builder
	for _, attr := range SanitiseHeaders(header) {
		joined.WriteString(attr.Value.String())
	}
	got := joined.String()

	if strings.Contains(got, "SUPERSECRETVALUE123") {
		t.Fatal("a clearance cookie value reached the loggable attributes")
	}
	if strings.Contains(got, "ANOTHERSECRET") {
		t.Fatal("a set-cookie value reached the loggable attributes")
	}
	if !strings.Contains(got, Redacted) {
		t.Fatal("expected a redaction marker in place of the cookie")
	}
	// An ordinary header is diagnostic and keeps its value.
	if !strings.Contains(got, "Mozilla/5.0") {
		t.Fatal("expected a non-secret header to survive redaction")
	}
}

// A test that checks only secret headers could be satisfied by redacting
// everything, so this one asserts ordinary headers stay readable.
func TestSanitiseHeadersKeepsOrdinaryValues(t *testing.T) {
	header := http.Header{}
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("CF-RAY", "8a1b2c3d4e5f6789")

	for _, attr := range SanitiseHeaders(header) {
		if attr.Value.String() == Redacted {
			t.Fatalf("header %q was redacted but carries no credential", attr.Key)
		}
	}
}

// A truncated value would still be enough to correlate a person with a log file,
// so nothing derived from the cookie may appear at all, not even a prefix.
func TestSanitiseHeadersLeavesNoPrefix(t *testing.T) {
	header := http.Header{}
	header.Set("Cookie", "cf_clearance=AbCdEfGhIjKlMnOpQrStUvWx")

	var joined strings.Builder
	for _, attr := range SanitiseHeaders(header) {
		joined.WriteString(attr.Value.String())
	}
	if strings.Contains(joined.String(), "AbCdEf") {
		t.Fatal("a prefix of the cookie value survived redaction")
	}
}

func TestHasSecretHeader(t *testing.T) {
	withCookie := http.Header{}
	withCookie.Set("Cookie", "cf_clearance=x")
	withoutCookie := http.Header{}
	withoutCookie.Set("Content-Type", "text/html")

	if !HasSecretHeader(withCookie) {
		t.Fatal("expected a cookie header to be reported as secret")
	}
	if HasSecretHeader(withoutCookie) {
		t.Fatal("expected an ordinary header not to be reported as secret")
	}
}

// The log lives beside the database, so it follows whatever the data directory
// resolves to rather than a fixed location.
func TestFilePathIsBesideTheDataDirectory(t *testing.T) {
	got := FilePath(`C:\Users\reader\AppData\Roaming\makidoku`)
	want := `C:\Users\reader\AppData\Roaming\makidoku\makidoku.log`
	if got != want {
		t.Fatalf("FilePath = %q, want %q", got, want)
	}
}
