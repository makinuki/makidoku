package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"

	"github.com/makinuki/makidoku/internal/app"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/web"
)

var (
	servePort int
	serveBind string
	serveTray bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the daemon and serve the web UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Open DB (creates file + runs migrations).
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer database.Close()
		repo := db.NewRepository(database)
		_ = repo // used by API handlers

		// Router.
		r := chi.NewRouter()
		r.Use(middleware.RequestID)
		// RealIP intentionally omitted: loopback-bound daemon must not trust
		// client-supplied headers (chi GHSA-3fxj-6jh8-hvhx and related).
		r.Use(middleware.Logger)
		r.Use(middleware.Recoverer)

		// Health + API placeholders.
		app.Mount(r, database)

		// Embedded frontend (or fallback notice when web/dist is empty).
		web.Mount(r)

		addr := net.JoinHostPort(serveBind, fmt.Sprintf("%d", servePort))
		srv := &http.Server{
			Addr:    addr,
			Handler: r,
		}

		// Tray is stubbed; guarded so headless/server usage does not
		// require cgo at build time when -tags notray is used.
		if serveTray {
			if err := runTrayStub(); err != nil {
				log.Printf("tray: %v (continuing without tray)", err)
			}
		}

		// Graceful shutdown on SIGINT/SIGTERM.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		go func() {
			log.Printf("makidoku listening on http://%s (data: %s)", addr, cfg.DataDir)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("listen: %v", err)
			}
		}()

		<-ctx.Done()
		log.Printf("shutting down...")
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shCtx)
	},
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "HTTP port")
	serveCmd.Flags().StringVar(&serveBind, "bind", "127.0.0.1", "bind address")
	serveCmd.Flags().BoolVar(&serveTray, "tray", false, "run with system tray (requires tray build tag)")
	rootCmd.AddCommand(serveCmd)
}

// runTrayStub is replaced by a real systray implementation behind
// `//go:build tray`. Keeping it here avoids a cgo dependency.
func runTrayStub() error { return nil }
