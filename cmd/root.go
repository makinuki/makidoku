package cmd

import (
	"fmt"
	"os"

	"github.com/makinuki/makidoku/internal/config"
	"github.com/spf13/cobra"
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
		downloadDir, err := config.ResolveDownloadDir(cfg.DownloadDir, cfg.DataDir)
		if err != nil {
			return fmt.Errorf("resolve download dir: %w", err)
		}
		cfg.DownloadDir = downloadDir
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
	rootCmd.PersistentFlags().StringVar(&cfg.DownloadDir, "download-dir", config.DefaultDownloadDir(),
		"download directory (default: <data-dir>/downloads or $MAKIDOKU_DOWNLOAD_DIR)")
	rootCmd.PersistentFlags().IntVar(&cfg.DownloadWorkers, "workers", config.DefaultDownloadWorkers(),
		"concurrent chapter download workers")
	rootCmd.PersistentFlags().DurationVar(&cfg.PageInterval, "page-interval", config.DefaultPageInterval(),
		"minimum interval between image requests to the same domain")
	rootCmd.PersistentFlags().StringVar(&cfg.RegistryURL, "registry", config.DefaultRegistryURL(),
		"source catalog: an index.json URL, or a path to a local mirror")
	rootCmd.PersistentFlags().DurationVar(&cfg.ChallengeWait, "challenge-wait", config.DefaultChallengeWait(),
		"how long a blocked request waits for anti-bot clearance")
	// Subcommands are registered in their respective files via init().
}
