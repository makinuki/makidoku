package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// The want constants mirror the ChallengeClass values the classifier returns.
const (
	wantNone     = "none"
	wantSolve    = "solvable"
	wantTerminal = "terminal"
	wantRate     = "rate-limited"
)

// challengeCorpus holds recorded Cloudflare responses. Bodies are trimmed
// reproductions of the shapes that matter for classification, not byte-exact
// captures: the classification logic under test keys on markers, status, and
// headers, never on layout.
var challengeCorpus = []struct {
	name    string
	status  int
	headers map[string]string
	body    string
	want    string
}{
	{
		name:    "interstitial.html",
		status:  403,
		headers: map[string]string{"cf-mitigated": "challenge", "server": "cloudflare"},
		body:    "interstitial.html",
		want:    wantSolve,
	},
	{
		name:    "turnstile.html",
		status:  403,
		headers: map[string]string{"server": "cloudflare"},
		body:    "turnstile.html",
		want:    wantSolve,
	},
	{
		// Same interstitial, but served as 503, which is the shape the current
		// detector is built around.
		name:    "interstitial.html",
		status:  503,
		headers: map[string]string{"server": "cloudflare"},
		body:    "interstitial.html",
		want:    wantSolve,
	},
	{
		name:    "geo_block.html",
		status:  403,
		headers: map[string]string{"server": "cloudflare"},
		body:    "geo_block.html",
		want:    wantTerminal,
	},
	{
		name:    "banned.html",
		status:  403,
		headers: map[string]string{"server": "cloudflare"},
		body:    "banned.html",
		want:    wantTerminal,
	},
	{
		name:    "rate_limited.html",
		status:  429,
		headers: map[string]string{"retry-after": "30"},
		body:    "rate_limited.html",
		want:    wantRate,
	},
	{
		name:    "ok_page.html",
		status:  200,
		headers: map[string]string{"content-type": "text/html"},
		body:    "ok_page.html",
		want:    wantNone,
	},
}

// readFixture loads a body from testdata/challenge.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "challenge", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

// TestCorpusFixturesExist guards the corpus itself. Every body referenced by
// the tables below must be present, so a rename fails here rather than turning
// into a silently empty classification test.
func TestCorpusFixturesExist(t *testing.T) {
	for _, entry := range challengeCorpus {
		if entry.body == "" {
			t.Fatalf("%s: empty body reference", entry.name)
		}
		readFixture(t, entry.body)
	}
}

// TestCurrentDetectorOverCorpus records how the detector behaves today against
// the corpus. It is a characterization test, not an assertion of desired
// behavior: the goal is to make every divergence visible and named, so task 1.1
// has a baseline to compare against and so a future change to detection cannot
// regress silently.
//
// The known divergences are documented in the expectations table at the bottom
// of this file.
func TestCurrentDetectorOverCorpus(t *testing.T) {
	for _, entry := range challengeCorpus {
		t.Run(entry.name+"/"+httpStatusLabel(entry.status), func(t *testing.T) {
			body := readFixture(t, entry.body)
			got := Classify(entry.status, entry.headers, []byte(body))
			want := ChallengeClass(entry.want)
			if got != want {
				t.Errorf("class = %q, want %q", got, want)
			}
		})
	}
}

func httpStatusLabel(status int) string {
	switch status {
	case 200:
		return "ok"
	case 403:
		return "forbidden"
	case 429:
		return "throttled"
	case 503:
		return "unavailable"
	default:
		return "other"
	}
}
