package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"

	"github.com/makinuki/makidoku/internal/api"
	"github.com/makinuki/makidoku/internal/backup"
	"github.com/makinuki/makidoku/internal/config"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/imagecache"
	"github.com/makinuki/makidoku/internal/logger"
	"github.com/makinuki/makidoku/internal/settings"
	"github.com/makinuki/makidoku/internal/solver"
	"github.com/makinuki/makidoku/internal/tracker"
	"github.com/makinuki/makidoku/internal/updater"
	"github.com/makinuki/makidoku/internal/version"
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
	updater   *updater.Service
	settings  *settings.Service
	// challenger presents anti-bot challenges. It owns a thread and a window, so it
	// is created with the server and released with it.
	challenger *solver.Solver
	// autoSolver answers challenges without a button press when the toggle is on.
	// It is nil when the toggle is off. The broker's hook also holds it, and
	// keeping it here makes that explicit.
	autoSolver *autoSolver
	http       *http.Server
	api        *api.Server
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

	// The data directory is resolved before anything else so the log file can be
	// opened next to the database. A failure here is returned rather than swallowed:
	// silently losing the log is the one outcome that would make a later report
	// unexplainable.
	if err := logger.OpenFile(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	eng := engine.New(database, engine.Options{
		DataDir:       cfg.DataDir,
		RegistryURL:   cfg.RegistryURL,
		ChallengeWait: cfg.ChallengeWait,
	})
	repo := db.NewRepository(database)
	preferences := settings.New(repo)
	if rawLevel, err := preferences.Get("advanced.log_level"); err == nil {
		if setErr := logger.SetLevelFromRaw(rawLevel); setErr != nil {
			slog.Warn("daemon log level invalid, using info", "err", setErr)
		}
	}
	// The browser identity is read once here and applied to the fetcher, so every
	// request presents it rather than the transport naming itself as Go. The
	// typed read decodes the stored JSON string, because the stored form carries
	// the quotes and a quoted identity is rejected by an origin.
	if agent, err := preferences.String(settings.UserAgentKey); err == nil {
		eng.SetUserAgent(agent)
	}
	workers := cfg.DownloadWorkers
	if configuredWorkers, settingErr := preferences.Int("downloads.concurrent"); settingErr == nil && configuredWorkers > 0 {
		workers = configuredWorkers
	}
	pageInterval := cfg.PageInterval
	if intervalMs, settingErr := preferences.Int("downloads.page_interval"); settingErr == nil && intervalMs >= 0 {
		pageInterval = time.Duration(intervalMs) * time.Millisecond
	}
	maxRetries := 3
	if attempts, settingErr := preferences.Int("downloads.retry_attempts"); settingErr == nil && attempts >= 0 {
		maxRetries = attempts
	}
	maxActiveSources := downloader.DefaultMaxActiveSources
	if sources, settingErr := preferences.Int("downloads.sources_at_once"); settingErr == nil && sources > 0 {
		maxActiveSources = sources
	}
	chaptersPerSource := downloader.DefaultChaptersPerSource
	if chapters, settingErr := preferences.Int("downloads.chapters_per_source"); settingErr == nil && chapters > 0 {
		chaptersPerSource = chapters
	}
	downloads := downloader.NewQueue(db.NewRepository(database), eng, downloader.Options{
		Workers: workers, PageInterval: pageInterval,
		DownloadDir: cfg.DownloadDir, MaxRetries: maxRetries,
		MaxActiveSources: maxActiveSources, ChaptersPerSource: chaptersPerSource,
	})

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	// RealIP is intentionally omitted: the daemon binds to loopback and must
	// not trust client supplied forwarding headers.
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	// Correlate records the request and its outcome once, under the identifier
	// RequestID assigned, so the records beneath one operation can be found
	// together. It must follow RequestID to have an identifier to use.
	router.Use(api.Correlate)

	trackers := tracker.NewRegistry(repo)
	// Credential storage is unavailable without the encryption secret, so the
	// condition is reported at boot rather than when a user is halfway through
	// a provider authorization.
	if err := trackers.CredentialsReady(); err != nil {
		slog.Warn("tracker credentials disabled", "err", err)
	}
	syncer := &tracker.SyncWorker{Repo: repo, Registry: trackers, Settings: preferences}
	if cfg.ImageCacheMaxBytes == 0 {
		cfg.ImageCacheMaxBytes = config.DefaultImageCacheMaxBytes()
	}
	if configuredDays, settingErr := preferences.Int("advanced.image_cache_days"); settingErr == nil && configuredDays > 0 {
		cfg.ImageCacheMaxAge = time.Duration(configuredDays) * 24 * time.Hour
	} else if cfg.ImageCacheMaxAge == 0 {
		cfg.ImageCacheMaxAge = config.DefaultImageCacheMaxAge()
	}
	imageCache := imagecache.New(filepath.Join(cfg.DataDir, "image-cache"), cfg.ImageCacheMaxBytes, cfg.ImageCacheMaxAge)
	server := api.NewTrackerServer(repo, eng, downloads, trackers)
	server.SetSettings(preferences)
	if incognito, err := preferences.Bool("privacy.incognito"); err == nil && incognito {
		server.SetIncognito(true)
	}
	updateService := updater.New(repo, func(ctx context.Context, mangaID string) ([]string, error) {
		return server.RefreshManga(ctx, mangaID)
	})
	updateService.SetEnqueue(server.EnqueueNewChapters)
	server.SetUpdater(updateService)
	server.SetImageCache(imageCache)

	// The solver owns a pinned thread and a window, so it is created once for the
	// life of the daemon rather than per request. A build without an embedded
	// browser still constructs one; it reports itself unavailable, and the solve
	// route returns that plainly instead of failing.
	solverProfile := filepath.Join(cfg.DataDir, "solver", "profile")
	challenger := solver.New(solverProfile)
	server.SetChallenger(challenger)

	// The automatic path is registered only when the toggle is on, so with it off
	// no hook exists and nothing in the engine is told a challenge happened.
	autoSolver := startAutoSolve(cfg, eng, challenger, eng.ChallengeBroker())
	// The stored preference wins over the flag, so a reader who chose the
	// toggle in the interface keeps that choice across restarts.
	if raw, err := preferences.Get(autoSolveSettingKey); err == nil && raw != "" {
		autoSolver.SetEnabled(raw == "true")
	}
	server.SetAutoSolveObserver(autoSolver.SetEnabled)
	server.SetUserAgentObserver(eng.SetUserAgent)

	sweepOnce(imageCache, repo.ListCachedPaths)
	server.Mount(router)
	web.Mount(router)

	return &Server{
		cfg:        cfg,
		db:         database,
		engine:     eng,
		downloads:  downloads,
		trackers:   trackers,
		syncer:     syncer,
		updater:    updateService,
		settings:   preferences,
		api:        server,
		challenger: challenger,
		autoSolver: autoSolver,
		http: &http.Server{
			Addr:              net.JoinHostPort(cfg.Bind, fmt.Sprint(cfg.Port)),
			Handler:           router,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

// Addr is the address the daemon listens on.
func (s *Server) Addr() string { return s.http.Addr }

// sweepOnce applies image cache retention at startup. Failures are logged
// because a failed sweep must not keep the daemon from serving.
func sweepOnce(cache *imagecache.Cache, keep func() ([]string, error)) {
	paths, err := keep()
	if err != nil {
		slog.Warn("image cache sweep skipped", "err", err)
		return
	}
	keepSet := make(map[string]bool, len(paths))
	for _, path := range paths {
		keepSet[path] = true
	}
	if err := cache.Sweep(keepSet); err != nil {
		slog.Warn("image cache sweep failed", "err", err)
	}
}

// Run serves until ctx is cancelled, then shuts down and releases resources.
func (s *Server) Run(ctx context.Context) error {
	defer s.close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.api.SetLifetimeContext(runCtx)
	interval, err := s.settings.Duration("library.update_interval")
	if err != nil {
		return fmt.Errorf("library update interval: %w", err)
	}
	updateOnLaunch, err := s.settings.Bool("library.update_on_launch")
	if err != nil {
		return fmt.Errorf("library update on launch: %w", err)
	}
	backupInterval, err := s.settings.Duration("backup.auto_interval")
	if err != nil {
		return fmt.Errorf("backup interval: %w", err)
	}
	backupKeep, err := s.settings.Int("backup.auto_keep")
	if err != nil {
		return fmt.Errorf("backup retention: %w", err)
	}

	errs := make(chan error, 1)
	go func() {
		// Without this, there is no way to tell which build answered a question,
		// or whether the solver can run on this machine at all.
		slog.Info("makidoku starting",
			"version", version.Version,
			"commit", version.Commit,
			"build_date", version.Date,
			"data_dir", s.cfg.DataDir,
			"log_file", logger.LogFilePath(),
			"port", s.cfg.Port,
			"log_level", logger.CurrentLevel().String(),
			"challenge_wait", s.cfg.ChallengeWait,
			"solver_available", s.challenger.Available(context.Background()) == nil,
		)
		slog.Info("makidoku listening", "addr", s.http.Addr, "data", s.cfg.DataDir)
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
	if updateOnLaunch {
		go func() {
			if _, err := s.updater.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("library update on launch failed", "err", err)
			}
		}()
	}
	updateErrs := make(chan error, 1)
	go func() { updateErrs <- s.updater.RunTicker(runCtx, interval) }()
	backupErrs := make(chan error, 1)
	go func() { backupErrs <- backup.RunTicker(runCtx, s.db, s.cfg.DataDir, backupInterval, backupKeep) }()

	select {
	case err := <-errs:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = s.http.Shutdown(shutdownCtx)
		_ = waitForBackground(downloadErrs, syncErrs, false, false)
		<-updateErrs
		<-backupErrs
		return err
	case err := <-downloadErrs:
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		shutdownErr := s.http.Shutdown(shutdownCtx)
		backgroundErr := waitForBackground(downloadErrs, syncErrs, true, false)
		<-updateErrs
		<-backupErrs
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
		<-updateErrs
		<-backupErrs
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("tracker sync: %w", err)
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return backgroundErr
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	cancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return err
	}
	backgroundErr := waitForBackground(downloadErrs, syncErrs, false, false)
	<-updateErrs
	<-backupErrs
	return backgroundErr
}

// close releases the engine and the database. Plugins are released before the
// database because their storage writes go through it.
func (s *Server) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The solver goes first: it holds a window and a thread, and releasing it
	// after the database would let a solve finish against a closed handle.
	if s.challenger != nil {
		if err := s.challenger.Close(); err != nil {
			slog.Error("closing the challenge solver failed", "err", err)
		}
	}
	s.engine.Close(ctx)
	if err := s.db.Close(); err != nil {
		slog.Error("closing database failed", "err", err)
	}
	// The log is closed last so every record above reaches the file before the
	// handle is released.
	if err := logger.CloseFile(); err != nil {
		fmt.Fprintf(os.Stderr, "closing the log file failed: %v\n", err)
	}
}
