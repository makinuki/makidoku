package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/makinuki/makidoku/internal/config"
)

var (
	cfg     config.Config
	dataDir string
)

var rootCmd = &cobra.Command{
	Use:   "makidoku",
	Short: "MakiDoku - portable manga library, reader, and downloader",
	Long: `MakiDoku is a single-binary host for MakiNuki WASM plugins.
It serves an embedded React reader and a local REST API.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dir, err := config.ResolveDataDir(dataDir)
		if err != nil {
			return fmt.Errorf("resolve data dir: %w", err)
		}
		cfg.DataDir = dir
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "data directory for makidoku.db and downloads (default: ./data or $MAKIDOKU_DATA_DIR)")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "verbose logging")
	// Subcommands are registered in their respective files via init().
}
