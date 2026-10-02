package app

import (
	"testing"
)

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
