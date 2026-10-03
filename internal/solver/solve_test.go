package solver

import (
	"context"
	"testing"
	"time"
)

// A guard may issue material in stages, and it may rewrite a value it set earlier.
// Both count as the jar moving, because either means the site is still working.
func TestJarChangedNoticesNewNames(t *testing.T) {
	before := map[string]string{}
	after := map[string]string{"__guard_id": "abc"}
	if !jarChanged(before, after) {
		t.Fatal("a jar that gained a name was reported as still")
	}
}

func TestJarChangedNoticesRewrittenValues(t *testing.T) {
	before := map[string]string{"__guard_id": "abc"}
	after := map[string]string{"__guard_id": "def"}
	if !jarChanged(before, after) {
		t.Fatal("a jar that rewrote a value was reported as still")
	}
}

func TestJarChangedIgnoresAnIdenticalRead(t *testing.T) {
	jar := map[string]string{"__guard_id": "abc", "__guard_trust": "t"}
	if jarChanged(jar, map[string]string{"__guard_id": "abc", "__guard_trust": "t"}) {
		t.Fatal("an unchanged jar was reported as moved")
	}
}

// A solve begins from whatever is already on the profile, so an identical jar must
// not be mistaken for a fresh answer and end the attempt before the challenge.
func TestJarChangedIgnoresAnEmptyRead(t *testing.T) {
	if jarChanged(map[string]string{}, map[string]string{}) {
		t.Fatal("two empty jars were reported as different")
	}
}

// A real page is large and full of links. An interstitial is nearly empty, so
// volume separates the two even when the markers do not match the page.
func TestServedRecognisesRealContent(t *testing.T) {
	if !served(pageState{Links: 40, Text: 4000}) {
		t.Fatal("a full page was reported as an interstitial")
	}
}

func TestServedRejectsAnInterstitial(t *testing.T) {
	cases := map[string]pageState{
		"empty":         {},
		"few links":     {Links: 3, Text: 4000},
		"little text":   {Links: 40, Text: 10},
		"still on show": {Links: 40, Text: 4000, Challenge: true},
	}
	for name, state := range cases {
		if served(state) {
			t.Fatalf("%s: an interstitial was reported as a served page", name)
		}
	}
}

// A site that was never challenged serves the page and issues nothing, which is a
// normal outcome rather than a failure. The marker flag alone is not trusted.
func TestServedRequiresNoChallengeMarker(t *testing.T) {
	if served(pageState{Links: 40, Text: 4000, Challenge: false}) != true {
		t.Fatal("a page without a marker was not served")
	}
	if served(pageState{Links: 40, Text: 4000, Challenge: true}) != false {
		t.Fatal("a page with a marker was reported as served")
	}
}

// The view posts JSON. A malformed message must not take the solve down, because
// the view reports what it saw rather than validating its own replies.
func TestParsePageStateReadsAReport(t *testing.T) {
	state := parsePageState(`{
		"title": "Just a moment...",
		"url": "https://gate.test/",
		"agent": "Mozilla/5.0",
		"hints": {"Sec-Ch-Ua": "\"Chromium\";v=\"154\""},
		"challenge": true,
		"links": 0,
		"text": 12
	}`)
	if state.Title != "Just a moment..." || !state.Challenge {
		t.Fatalf("state = %+v, want the reported challenge", state)
	}
	if state.ClientHints["Sec-Ch-Ua"] == "" {
		t.Fatal("client hints were dropped")
	}
}

func TestParsePageStateSurvivesAMalformedMessage(t *testing.T) {
	for _, message := range []string{"", "not json", "{", "<html>"} {
		if state := parsePageState(message); state.Challenge || state.Links != 0 {
			t.Fatalf("message %q produced %+v, want an empty state", message, state)
		}
	}
}

func TestReportsKeepTheLatestState(t *testing.T) {
	sink := newReports()
	if sink.current().Title != "" {
		t.Fatal("an empty sink reported a title")
	}
	sink.store(pageState{Title: "first"})
	sink.store(pageState{Title: "second"})
	if got := sink.current().Title; got != "second" {
		t.Fatalf("title = %q, want the most recent report", got)
	}
}

// The cookie is written by the network stack while the identity travels back as a
// message, so the two can arrive in either order. Storing a cookie without the
// agent it was issued to is the most common reason a replay fails, so the wait
// exists to close that window.
func TestWaitForAgentReturnsAsSoonAsOneArrives(t *testing.T) {
	sink := newReports()
	go func() {
		time.Sleep(50 * time.Millisecond)
		sink.store(pageState{UserAgent: "Mozilla/5.0", Title: "ready"})
	}()
	state := sink.waitForAgent(context.Background(), 3*time.Second)
	if state == nil || state.UserAgent != "Mozilla/5.0" {
		t.Fatalf("state = %+v, want the reported identity", state)
	}
}

func TestWaitForAgentGivesUpAtTheDeadline(t *testing.T) {
	sink := newReports()
	start := time.Now()
	if state := sink.waitForAgent(context.Background(), 150*time.Millisecond); state != nil {
		t.Fatalf("state = %+v, want none", state)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited %v, want the grace period", elapsed)
	}
}

func TestWaitForAgentHonoursACancelledContext(t *testing.T) {
	sink := newReports()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if state := sink.waitForAgent(ctx, time.Hour); state != nil {
		t.Fatalf("state = %+v, want none after cancellation", state)
	}
}
