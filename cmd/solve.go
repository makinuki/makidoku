package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/solver"
)

var solveCmd = &cobra.Command{
	Use:   "solve <url>",
	Short: "Present a site in an embedded browser and capture its clearance",
	Long: `Opens the site in an embedded browser window and captures the clearance
cookie the site issues once its challenge is answered.

The window is shown, and the challenge may clear on its own or wait for a click.
Cookie values are never printed; only their names are, so the material can be
confirmed without putting a secret on the screen.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		profile := filepath.Join(cfg.DataDir, "solver", "profile")
		s := solver.New(profile)
		defer s.Close()

		if err := s.Available(ctx); err != nil {
			return err
		}
		fmt.Printf("opening %s in a browser window\n", args[0])
		fmt.Println("answer the challenge if one appears")

		result, err := s.Solve(ctx, args[0])
		if err != nil && !errors.Is(err, solver.ErrSolveAbandoned) {
			return err
		}
		if result == nil {
			return fmt.Errorf("the solve produced no result")
		}

		if !result.Captured {
			fmt.Println("\nno clearance cookie was issued")
			if result.NeedsInteraction {
				fmt.Println("the site was still showing a challenge when the attempt ended")
			} else {
				fmt.Println("the site served the request without challenging it")
			}
			if errors.Is(err, solver.ErrSolveAbandoned) {
				return err
			}
			return nil
		}

		names := make([]string, 0, len(result.Capture.Cookies))
		for name := range result.Capture.Cookies {
			names = append(names, name)
		}
		sort.Strings(names)

		fmt.Printf("\ncaptured:   %d cookie(s): %s\n", len(names), strings.Join(names, ", "))
		fmt.Printf("agent:      %s\n", result.Capture.UserAgent)
		fmt.Printf("hints:      %d value(s)\n", len(result.Capture.SecChUa))
		fmt.Println("\nvalues are not shown and are not stored by this command")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(solveCmd)
}
