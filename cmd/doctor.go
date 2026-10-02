package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/solver"
)

// doctorTimeout bounds the whole command. The self check has its own bound, and
// this one stops a stuck run from hanging a diagnostic.
const doctorTimeout = 60 * time.Second

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that the local environment can run makidoku",
	Long: `Runs local checks that do not need a network.

The browser check exercises the embedded view used to answer anti-bot
challenges. It needs no network and touches no source site, so it is safe to run
before trusting a download. A failure here means challenges cannot be solved on
this machine and the manual route is the only option.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), doctorTimeout)
		defer cancel()

		profile := filepath.Join(cfg.DataDir, "solver", "profile")
		s := solver.New(profile)
		defer s.Close()

		fmt.Println("checking the challenge solver")

		if err := s.Available(ctx); err != nil {
			if errors.Is(err, solver.ErrUnavailable) {
				// An absent browser is a supported configuration, not a fault, so
				// it is reported plainly and the command succeeds.
				fmt.Printf("  solver:     unavailable (%v)\n", err)
				fmt.Println("\nChallenges will have to be answered by hand and the cookie pasted in.")
				return nil
			}
			return err
		}
		fmt.Println("  solver:     available")

		if err := s.SelfCheck(ctx); err != nil {
			fmt.Printf("  self check: failed (%v)\n", err)
			return err
		}
		fmt.Println("  self check: passed")
		fmt.Println("\nThe embedded browser works. Anti-bot challenges can be answered on this machine.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
