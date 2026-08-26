package cmd

import (
	"context"
	"fmt"

	"github.com/makinuki/makidoku/internal/api"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/updater"
	"github.com/spf13/cobra"
)

type updateRunner interface {
	Run(context.Context) (int, error)
}

func executeUpdate(ctx context.Context, runner updateRunner) (int, error) {
	return runner.Run(ctx)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh library titles and record new chapters",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer database.Close()
		repo := db.NewRepository(database)
		eng := engine.New(database, engine.Options{DataDir: cfg.DataDir, RegistryURL: cfg.RegistryURL, ChallengeWait: cfg.ChallengeWait})
		defer func() {
			eng.Close(context.Background())
		}()
		refreshServer := api.NewServer(repo, eng)
		service := updater.New(repo, refreshServer.RefreshManga)
		count, err := executeUpdate(cmd.Context(), service)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "found %d new chapter(s)\n", count)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
