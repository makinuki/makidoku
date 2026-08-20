package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var downloadCmd = &cobra.Command{
	Use:   "download <source:manga-id> [--chapters 1-50] [--format cbz|folder]",
	Short: "Headless batch download",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("download: not yet implemented; got %q", args[0])
	},
}

func init() {
	downloadCmd.Flags().String("chapters", "", "chapter range, e.g. 1-50")
	downloadCmd.Flags().String("format", "cbz", "archive format: cbz or folder")
	rootCmd.AddCommand(downloadCmd)
}
