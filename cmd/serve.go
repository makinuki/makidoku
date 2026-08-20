package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/app"
	"github.com/makinuki/makidoku/internal/config"
)

var serveTray bool

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the daemon and serve the web UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		server, err := app.New(cfg)
		if err != nil {
			return err
		}

		// The tray is built behind a tag so headless use does not require cgo.
		if serveTray {
			if err := runTrayStub(); err != nil {
				log.Printf("tray: %v (continuing without tray)", err)
			}
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return server.Run(ctx)
	},
}

func init() {
	serveCmd.Flags().IntVar(&cfg.Port, "port", 8080, "HTTP port")
	serveCmd.Flags().StringVar(&cfg.Bind, "bind", "127.0.0.1", "bind address")
	serveCmd.Flags().BoolVar(&serveTray, "tray", false, "run with system tray (requires tray build tag)")
	serveCmd.Flags().StringVar(&cfg.RegistryURL, "registry", config.DefaultRegistryURL(),
		"source catalog: an index.json URL, or a path to a local mirror (default: the public registry)")
	serveCmd.Flags().DurationVar(&cfg.ChallengeWait, "challenge-wait", config.DefaultChallengeWait(),
		"how long a request blocked by an anti-bot challenge waits for clearance to be submitted")
	rootCmd.AddCommand(serveCmd)
}

// runTrayStub is replaced by a real systray implementation behind
// `//go:build tray`.
func runTrayStub() error { return nil }
