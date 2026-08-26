package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/app"
	"github.com/makinuki/makidoku/internal/config"
	"github.com/makinuki/makidoku/internal/tray"
)

var serveTray bool
var serveNoTray bool

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the daemon and serve the web UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		server, err := app.New(cfg)
		if err != nil {
			return err
		}

		enableTray := serveTray && !serveNoTray

		if enableTray {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			addr := net.JoinHostPort(cfg.Bind, fmt.Sprint(cfg.Port))
			serverErr := make(chan error, 1)
			go func() {
				serverErr <- server.Run(ctx)
			}()

			tray.Run(ctx, addr)

			stop()
			select {
			case err := <-serverErr:
				return err
			default:
				<-serverErr
				return nil
			}
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return server.Run(ctx)
	},
}

func init() {
	serveCmd.Flags().IntVar(&cfg.Port, "port", config.DefaultPort(), "HTTP port")
	serveCmd.Flags().StringVar(&cfg.Bind, "bind", "127.0.0.1", "bind address")
	serveCmd.Flags().BoolVar(&serveTray, "tray", false, "run with system tray (requires tray build tag)")
	serveCmd.Flags().BoolVar(&serveNoTray, "no-tray", false, "disable system tray even when built with tray tag")
	rootCmd.AddCommand(serveCmd)
}
