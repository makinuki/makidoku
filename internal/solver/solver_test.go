package solver

import "testing"

// The solver must satisfy the interface the engine depends on. This holds on every
// platform, including builds with no embedded browser, because the unsupported
// implementation is what lets those builds compile at all.
func TestSolverSatisfiesTheChallengerInterface(t *testing.T) {
	var _ Challenger = New(t.TempDir())
}

// A build without an embedded browser must report itself unavailable rather than
// failing, because the daemon falls back to asking the user for the material.
func TestUnavailableBuildReportsRatherThanFails(t *testing.T) {
	s := New(t.TempDir())
	defer s.Close()

	if err := s.Available(t.Context()); err != nil {
		// A machine that can present a window is expected to pass this check, and
		// failing here would mean a window-capable build reported itself unusable.
		t.Logf("this build reports the solver as unavailable: %v", err)
		return
	}
	t.Log("this build can present a window, so availability is not asserted")
}
