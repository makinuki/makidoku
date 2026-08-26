package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the makidoku version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("makidoku %s\n", version.Version)
		fmt.Printf("commit: %s\n", version.Commit)
		fmt.Printf("date: %s\n", version.Date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
