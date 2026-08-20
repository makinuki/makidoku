package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [--tracker anilist|mal|...]",
	Short: "Sync tracker progress",
	RunE: func(cmd *cobra.Command, args []string) error {
		tracker, _ := cmd.Flags().GetString("tracker")
		return fmt.Errorf("sync: not yet implemented; tracker=%q", tracker)
	},
}

func init() {
	syncCmd.Flags().String("tracker", "", "tracker to sync (anilist, mal, mangaupdates, mangabaka, kitsu)")
	rootCmd.AddCommand(syncCmd)
}
