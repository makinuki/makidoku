package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"

	"github.com/makinuki/makidoku/internal/api"
	"github.com/makinuki/makidoku/internal/config"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/tracker"
	"github.com/makinuki/makidoku/web"
)

// Server owns the process wide resources: the database, the plugin engine and
// the HTTP listener serving the API and the embedded reader.
type Server struct {
	cfg       config.Config
	db        *sqlx.DB
	engine    *engine.Engine
	downloads *downloader.Queue
	trackers  *tracker.Registry
	syncer    *tracker.SyncWorker
	http      *http.Server
}

func waitForBackground(downloadErrs, syncErrs <-chan error, downloadConsumed, syncConsumed bool) error {
	var first error
	if !downloadConsumed {
		if err := <-downloadErrs; err != nil && first == nil && !errors.Is(err, context.Canceled) {
			first = err
		}
	}
	if !syncConsumed {
		if err := <-syncErrs; err != nil && first == nil && !errors.Is(err, context.Canceled) {
			first = err
		}
	}
	return first
}

// New opens the database, applies migrations and wires the router.
func New(cfg config.Config) (*Server, error) {
	if cfg.DownloadWorkers < 1 {
		cfg.DownloadWorkers = config.DefaultDownloadWorkers()
	}
	if cfg.DownloadDir == "" {
		downloadDir, err := config.ResolveDownloadDir(config.DefaultDownloadDir(), cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("resolve download dir: %w", err)
		}
		cfg.DownloadDir = downloadDir
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	eng := engine.New(database, engine.Options{
		DataDir:       cfg.DataDir,
		RegistryURL:   cfg.RegistryURL,
		ChallengeWait: cfg.ChallengeWait,
	})
	downloads := downloader.NewQueue(db.NewRepository(database), eng, downloader.Options{
		Workers: cfg.DownloadWorkers, PageInterval: cfg.PageInterval,
		DownloadDir: cfg.DownloadDir, MaxRetries: 3,
	})

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	// RealIP is intentionally omitted: the daemon binds to loopback and must
	// not trust client supplied forwarding headers.
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	repo := db.NewRepository(database)
	trackers := tracker.NewRegistry(repo)
	syncer := &tracker.SyncWorker{Repo: repo, Registry: trackers}
	api.NewTrackerServer(repo, eng, downloads, trackers).Mount(router)
	web.Mount(router)

	return &Server{
		cfg:       cfg,
		db:        database,
		engine:    eng,
		downloads: downloads,
		trackers:  trackers,
		syncer:    syncer,
		http: &http.Server{
			Addr:              net.JoinHostPort(cfg.Bind, fmt.Sprint(cfg.Port)),
			Handler:           router,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

// Addr is the address the daemon listens on.
func (s *Server) Addr() string { return s.http.Addr }

// Run serves until ctx is cancelled, then shuts down and releases resources.
func (s *Server) Run(ctx context.Context) error {
	defer s.close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 1)
	go func() {
		log.Printf("makidoku listening on http://%s (data: %s)", s.http.Addr, s.cfg.DataDir)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- err
			return
		}
		errs <- nil
	}()
	downloadErrs := make(chan error, 1)
	go func() { downloadErrs <- s.downloads.Run(runCtx) }()
	syncErrs := make(chan error, 1)
	go func() { syncErrs <- s.syncer.Run(runCtx) }()

	select {
	case err := <-errs:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = s.http.Shutdown(shutdownCtx)
		_ = waitForBackground(downloadErrs, syncErrs, false, false)
		return err
	case err := <-downloadErrs:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		shutdownErr := s.http.Shutdown(shutdownCtx)
		backgroundErr := waitForBackground(downloadErrs, syncErrs, true, false)
		if err != nil {
			return fmt.Errorf("downloader: %w", err)
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return backgroundErr
	case err := <-syncErrs:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		shutdownErr := s.http.Shutdown(shutdownCtx)
		backgroundErr := waitForBackground(downloadErrs, syncErrs, false, true)
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("tracker sync: %w", err)
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return backgroundErr
	case <-ctx.Done():
	}

	log.Printf("shutting down")
	cancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return waitForBackground(downloadErrs, syncErrs, false, false)
}

// close releases the engine and the database. Plugins are released before the
// database because their storage writes go through it.
func (s *Server) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.engine.Close(ctx)
	if err := s.db.Close(); err != nil {
		log.Printf("closing database failed: %v", err)
	}
}
