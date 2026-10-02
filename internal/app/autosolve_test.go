package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// brokerStoreFixture opens a repository for the broker to store clearance in.
func brokerStoreFixture(t *testing.T) (*db.Repository, *sqlx.DB) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "broker.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Stored clearance references its source, so the fixture needs one.
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at)
		VALUES('s','Source','1',1,'multi','https://source.test','s.wasm',1)`); err != nil {
		handle.Close()
		t.Fatal(err)
	}
	return db.NewRepository(handle), handle
}

// challengeBrokerFixture returns a broker that waits, plus a channel a test can
// close so no request is left parked behind it.
func challengeBrokerFixture(t *testing.T) (*engine.ClearanceBroker, chan struct{}) {
	t.Helper()
	store, handle := brokerStoreFixture(t)
	t.Cleanup(func() { handle.Close() })
	broker := engine.NewClearanceBroker(store, nil, 3*time.Second)

	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	return broker, release
}

func TestAttemptBudgetAllowsUpToTheLimit(t *testing.T) {
	budget := NewAttemptBudget(3)

	for attempt := 1; attempt <= 3; attempt++ {
		if !budget.Take("kagane.to") {
			t.Fatalf("attempt %d was refused before the limit was reached", attempt)
		}
		if got := budget.Spent("kagane.to"); got != attempt {
			t.Fatalf("spent=%d after %d attempts, want %d", got, attempt, attempt)
		}
	}

	if budget.Take("kagane.to") {
		t.Fatal("a fourth attempt was allowed")
	}
}

// Origins are counted apart, because one site being stubborn is not a reason to
// stop helping with another.
func TestAttemptBudgetCountsOriginsApart(t *testing.T) {
	budget := NewAttemptBudget(1)

	if !budget.Take("kagane.to") {
		t.Fatal("the first attempt on one origin was refused")
	}
	if !budget.Take("mangadot.net") {
		t.Fatal("a different origin was refused because another had spent its budget")
	}
	if budget.Take("kagane.to") {
		t.Fatal("the exhausted origin was allowed again")
	}
}

// A verified solve clears the count, so an origin that recovers gets fresh
// attempts rather than being written off for the session.
func TestAttemptBudgetClearsOnVerifiedSolve(t *testing.T) {
	budget := NewAttemptBudget(2)
	budget.Take("mangadot.net")
	budget.Take("mangadot.net")

	if budget.Take("mangadot.net") {
		t.Fatal("the limit was not enforced")
	}

	budget.Clear("mangadot.net")

	if got := budget.Spent("mangadot.net"); got != 0 {
		t.Fatalf("spent=%d after a verified solve, want 0", got)
	}
	if !budget.Take("mangadot.net") {
		t.Fatal("an origin was not given fresh attempts after a verified solve")
	}
}

// An attempt reserved but never opened must not be spent, or a failure to start
// a window would quietly cost the reader their allowance.
func TestAttemptBudgetReleasesUnusedAttempts(t *testing.T) {
	budget := NewAttemptBudget(1)

	budget.Take("weebcentral.com")
	budget.Release("weebcentral.com")

	if !budget.Take("weebcentral.com") {
		t.Fatal("a released attempt was not returned")
	}
}

func TestAttemptBudgetDisabledWhenLimitIsNotPositive(t *testing.T) {
	for _, limit := range []int{0, -1} {
		budget := NewAttemptBudget(limit)
		if budget.Take("kagane.to") {
			t.Fatalf("limit %d allowed an attempt", limit)
		}
	}
}

// A nil budget is the disabled state, so callers do not need a special case when
// auto-solve is off.
// A classified challenge announces itself once the request has committed to
// waiting, and only then, so an automatic solver starts while the request is
// still parked rather than after it has already failed.
func TestChallengeHookIsCalledWhileTheRequestWaits(t *testing.T) {
	broker, release := challengeBrokerFixture(t)

	announced := make(chan string, 1)
	broker.SetChallengeHook(func(sourceID, origin, blockedURL string) {
		announced <- sourceID + "|" + origin
	})

	waited := make(chan bool, 1)
	go func() {
		waited <- broker.Resolve(context.Background(), "s", "", engine.HttpError{
			URL: "https://kagane.to/search?q=x", Status: 403,
		})
	}()

	select {
	case got := <-announced:
		if got != "s|kagane.to" {
			t.Fatalf("announced %q, want the source and origin of the blocked request", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the hook was not called while a request was waiting")
	}
	close(release)
	<-waited
}

// A request that fails at once because the daemon does not wait has nothing to
// solve for, so nothing is announced. Opening a window then would be an intrusion
// with no request behind it.
func TestChallengeHookIsNotCalledWhenTheDaemonDoesNotWait(t *testing.T) {
	store, handle := brokerStoreFixture(t)
	t.Cleanup(func() { handle.Close() })
	broker := engine.NewClearanceBroker(store, nil, 0)

	announced := false
	broker.SetChallengeHook(func(string, string, string) { announced = true })

	if broker.Resolve(context.Background(), "s", "", engine.HttpError{
		URL: "https://kagane.to/search?q=x", Status: 403,
	}) {
		t.Fatal("a request reported success with no clearance and no wait")
	}
	if announced {
		t.Fatal("a challenge was announced even though the request failed at once")
	}
}

// Material that already exists answers the request without waiting, so there is
// nothing for a solver to do.
func TestChallengeHookIsNotCalledWhenClearanceAlreadyExists(t *testing.T) {
	store, handle := brokerStoreFixture(t)
	t.Cleanup(func() { handle.Close() })
	broker := engine.NewClearanceBroker(store, nil, time.Minute)
	if err := broker.Submit("s", "kagane.to", "value", "agent"); err != nil {
		t.Fatal(err)
	}

	announced := false
	broker.SetChallengeHook(func(string, string, string) { announced = true })

	if !broker.Resolve(context.Background(), "s", "", engine.HttpError{
		URL: "https://kagane.to/search?q=x", Status: 403,
	}) {
		t.Fatal("stored clearance did not answer the request")
	}
	if announced {
		t.Fatal("a challenge was announced although clearance already existed")
	}
}

func TestNilAttemptBudgetRefuses(t *testing.T) {
	var budget *AttemptBudget
	if budget.Take("kagane.to") {
		t.Fatal("a nil budget allowed an attempt")
	}
	budget.Release("kagane.to")
	budget.Clear("kagane.to")
	if budget.Spent("kagane.to") != 0 || budget.Limit() != 0 {
		t.Fatal("a nil budget reported a non-zero state")
	}
}
