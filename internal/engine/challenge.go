package engine

import (
	"net/http"
	"strings"
)

// Marker matching is separated into strong and weak lists. A strong marker
// identifies an interstitial on its own. A weak marker is a phrase an ordinary
// page may legitimately contain, so it only counts alongside a refusal status.
//
// The substring "challenge-platform" also occurs in Cloudflare's JavaScript
// detection script, which every zone loads from /cdn-cgi/challenge-platform/,
// and "just a moment" occurs in one origin's own interface copy. Neither
// identifies a challenge by itself, so neither is a strong marker.

// strongMarkers identify an interstitial by themselves.
var strongMarkers = []string{
	"cf-browser-verification",
	// The element attribute, not the script path every zone loads.
	"id=\"cf-challenge-platform\"",
	"id='cf-challenge-platform'",
	"cf_chl_opt",
	"__cf_chl",
	"cf-challenge-running",
	"cf-turnstile",
	"attention required",
	"ddos protection",
}

// weakMarkers are phrases that mean nothing without corroboration.
var weakMarkers = []string{
	"just a moment",
	"checking your browser",
	"enable javascript and cookies",
	"please stand by",
	"verify you are human",
	"verifying you are human",
}

// terminalMarkers identify a refusal that a challenge cannot clear.
var terminalMarkers = []string{
	"you have been blocked",
	"error 1010",
	"error 1020",
	"access denied",
	"not available in your country",
	"unavailable in your region",
	"blocked by",
	"forbidden by",
	"your access has been restricted",
	"permission to access",
}

// markerScanLimit caps how much of a body is inspected. Markers appear in the
// document head, so a large prefix is more than enough and it keeps an
// eight megabyte image from being scanned end to end.
const markerScanLimit = 64 << 10

// Classify decides what a response means to the host.
//
// The order is deliberate. A rate limit is checked first because Cloudflare
// answers a mitigated request with 429 carrying cf-mitigated, and treating that
// as a solvable puzzle would open a window for something no puzzle fixes. A
// terminal refusal is checked next, before any challenge test, so a geo-block
// can never be mistaken for something worth retrying.
func Classify(status int, headers map[string]string, body []byte) ChallengeClass {
	mitigated := headerValue(headers, "cf-mitigated") == "challenge"
	scan := body
	if len(scan) > markerScanLimit {
		scan = scan[:markerScanLimit]
	}
	lower := strings.ToLower(string(scan))

	if status == http.StatusTooManyRequests {
		return ClassRateLimited
	}
	if containsAny(lower, terminalMarkers) {
		return ClassTerminal
	}

	strong := containsAny(lower, strongMarkers)
	weak := containsAny(lower, weakMarkers)
	// A refusal corroborates a weak marker. A 404 is not a refusal: a listing
	// endpoint may legitimately answer that way, so it must not upgrade a page
	// that merely contains the words.
	refused := status == http.StatusForbidden || status == http.StatusServiceUnavailable

	switch {
	case strong || mitigated:
		return ClassSolvable
	case weak && refused:
		return ClassSolvable
	}

	switch status {
	case http.StatusOK, http.StatusNotFound, http.StatusBadRequest, http.StatusUnauthorized:
		return ClassNone
	case http.StatusForbidden, http.StatusServiceUnavailable:
		// A refusal carrying no marker at all is terminal, not a challenge.
		return ClassTerminal
	}
	return ClassNone
}

// ClassifyResponse classifies a completed request.
func ClassifyResponse(status int, headers http.Header, body []byte) ChallengeClass {
	flat := make(map[string]string, len(headers))
	for name, values := range headers {
		flat[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	return Classify(status, flat, body)
}

func headerValue(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// needsChallengeResponse keeps the previous boolean helper working for callers
// that only care whether a replay is worth attempting.
func needsChallengeResponse(status int, headers map[string]string, body []byte) bool {
	return Classify(status, headers, body) == ClassSolvable
}
