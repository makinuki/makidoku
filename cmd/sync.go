package cmd

import (
	"context"
	"fmt"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/tracker"
	"github.com/spf13/cobra"
)

type syncRunner interface {
	DrainDue(context.Context, string) (int, error)
}

type trackerSyncRunner struct {
	worker *tracker.SyncWorker
}

func (r trackerSyncRunner) DrainDue(ctx context.Context, trackerType string) (int, error) {
	if err := r.worker.Prepare(); err != nil {
		return 0, err
	}
	count := 0
	for {
		processed, err := r.worker.ProcessOne(ctx, trackerType)
		if err != nil {
			return count, err
		}
		if !processed {
			return count, nil
		}
		count++
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
	}
}

var syncCmd = &cobra.Command{
	Use:   "sync [--tracker anilist|mal|...]",
	Short: "Sync tracker progress",
	RunE: func(cmd *cobra.Command, args []string) error {
		trackerType, _ := cmd.Flags().GetString("tracker")
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return err
		}
		defer database.Close()
		repo := db.NewRepository(database)
		runner := trackerSyncRunner{worker: &tracker.SyncWorker{Repo: repo, Registry: tracker.NewRegistry(repo)}}
		count, err := executeSync(cmd.Context(), runner, trackerType)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "synchronized %d job(s)\n", count)
		return nil
	},
}

func executeSync(ctx context.Context, runner syncRunner, trackerType string) (int, error) {
	if trackerType == "mal" {
		trackerType = "myanimelist"
	}
	if trackerType != "" {
		switch trackerType {
		case "anilist", "myanimelist", "mangaupdates", "mangabaka", "kitsu":
		default:
			return 0, fmt.Errorf("unknown tracker %q", trackerType)
		}
	}
	return runner.DrainDue(ctx, trackerType)
}

func init() {
	syncCmd.Flags().String("tracker", "", "tracker to sync (anilist, mal, mangaupdates, mangabaka, kitsu)")
	rootCmd.AddCommand(syncCmd)
}
