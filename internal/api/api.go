package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/imagecache"
	"github.com/makinuki/makidoku/internal/settings"
	"github.com/makinuki/makidoku/internal/solver"
	"github.com/makinuki/makidoku/internal/tracker"
	"github.com/makinuki/makidoku/internal/updater"
	"github.com/makinuki/makidoku/internal/version"
)

type downloadQueue interface {
	List() ([]db.DownloadQueueItem, error)
	Stats() downloader.Stats
	EnqueueManga(context.Context, string, downloader.ChapterSelection, string) ([]db.DownloadQueueItem, error)
	Pause(int64) error
	Resume(int64) error
	Cancel(int64) error
	Retry(int64) error
	RetryFailedItems(sourceID string) (int, error)
	InvalidateSourcePolicy(sourceID string)
	Defaults() (interval time.Duration, maxAttempts int, backoff time.Duration)
	ConcurrencyDefaults() (maxActiveSources, chaptersPerSource int)
	PauseSource(sourceID string)
	ResumeSource(sourceID string)
	PausedSources() []string
	PauseAll()
	ResumeAll()
	Paused() bool
	CancelAll() (int64, error)
	Reorder([]int64) error
	Subscribe() (<-chan downloader.Event, func())
}

// Server holds the dependencies shared by the REST handlers.
type Server struct {
	repo          *db.Repository
	engine        *engine.Engine
	downloads     downloadQueue
	trackers      *tracker.Registry
	syncer        *tracker.SyncWorker
	imageCache    *imagecache.Cache
	trackerEvents *trackerBroker
	settings      *settings.Service
	updater       *updater.Service
	// challenger presents anti-bot challenges in a browser. It is attached after
	// construction because it owns a thread and a window, and a build without an
	// embedded browser leaves it nil.
	challenger solver.Challenger
	// A solve already in flight, keyed by origin. A second window for one origin
	// would show the reader two windows answering a single challenge, so a repeat
	// request for the same origin is refused while the first is open.
	solving  sync.Mutex
	inFlight map[string]struct{}
	lifetime atomic.Pointer[context.Context]
	// incognito is the runtime no-trace flag: while set, reading progress is
	// not recorded and no automatic tracker activity is produced.
	incognito atomic.Bool
	// seriesURLLookups marks the titles whose series page URL lookup already
	// ran, so a title the source cannot match is not searched on every read.
	seriesURLLookups sync.Map
	// dataDirOverride redirects locally staged artifacts away from the
	// engine's data directory, which tests use to keep the working tree clean.
	dataDirOverride string
	// imageSlots bounds concurrent upstream image transfers. A grid of covers
	// can otherwise open far more transfers than the link or the source can
	// serve, and each one holds a browser connection for its whole duration.
	imageSlots     chan struct{}
	imageSlotsOnce sync.Once
}

// imageFetchConcurrency caps simultaneous upstream image transfers.
const imageFetchConcurrency = 4

// imageGate returns the image transfer semaphore, building it on first use so
// a Server constructed without NewServer still behaves.
func (s *Server) imageGate() chan struct{} {
	s.imageSlotsOnce.Do(func() {
		if s.imageSlots == nil {
			s.imageSlots = make(chan struct{}, imageFetchConcurrency)
		}
	})
	return s.imageSlots
}

// beginImageFetch reserves one image transfer slot, giving up when the caller's
// context ends first.
func (s *Server) beginImageFetch(ctx context.Context) error {
	select {
	case s.imageGate() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// endImageFetch releases a slot reserved by beginImageFetch.
func (s *Server) endImageFetch() { <-s.imageGate() }

// imageFetchContext bounds one daemon-owned image transfer. The HTTP request
// context is deliberately not used: a transfer already in flight is allowed to
// finish so its bytes can be cached even when the client navigates away.
func (s *Server) imageFetchContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(s.Lifetime(), engine.ImageFetchTimeout)
}

// Lifetime returns the daemon run context when available, falling back to a
// detached context for tests and direct construction.
func (s *Server) Lifetime() context.Context {
	if ctx := s.lifetime.Load(); ctx != nil {
		return *ctx
	}
	return context.Background()
}

func NewServer(repo *db.Repository, eng *engine.Engine, downloads ...downloadQueue) *Server {
	server := &Server{repo: repo, engine: eng, trackerEvents: newTrackerBroker()}
	if len(downloads) > 0 && downloads[0] != nil {
		server.downloads = downloads[0]
	}
	return server
}

// SetChallenger attaches the component that presents anti-bot challenges in a
// browser. Without it the solve route reports that solving is unavailable, and
// the manual paste route remains the way to supply clearance.
func (s *Server) SetChallenger(c solver.Challenger) {
	s.challenger = c
}

// solveResponse is the outcome of one solve.
//
// Capture and verification are reported separately because they answer different
// questions. Capture says material was read; verification says a request that was
// previously blocked now succeeds. A captured cookie that does not verify is the
// common failure, and collapsing the two would report it as a success.
type solveResponse struct {
	Origin   string `json:"origin"`
	Captured bool   `json:"captured"`
	Verified bool   `json:"verified"`
	// NeedsInteraction reports that a challenge was on screen and unanswered when
	// the attempt ended. This is an expected state, not a fault: some challenges
	// wait for a click.
	NeedsInteraction bool `json:"needsInteraction"`
	// Challenge reports that the site was still challenging after the attempt.
	Challenge bool `json:"challenge"`
	// Cookies lists the names that were stored. Values are never returned.
	Cookies []string `json:"cookies"`
	// Message explains the outcome for a reader of the interface.
	Message string `json:"message"`
}

// solveClearance presents a source's origin in an embedded browser and stores the
// clearance the site issues.
//
// The request is held for the duration rather than answered early and polled. A
// request that was blocked by the challenge is parked by the fetcher for the same
// window, and storing the bundle releases it, so holding here is what lets the
// interrupted operation continue rather than be reissued by the caller.
func (s *Server) solveClearance(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")

	// An absent body is accepted, because the outstanding challenge already
	// names the origin to present. A malformed one is still rejected.
	var body struct {
		Origin string `json:"origin"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}

	target, err := s.resolveTarget(r, sourceID, body.Origin)
	if err != nil {
		writeLocalError(w, http.StatusBadRequest, err)
		return
	}

	if s.challenger == nil {
		writeLocalError(w, http.StatusServiceUnavailable,
			errors.New("the challenge solver is not available on this machine"))
		return
	}
	if err := s.challenger.Available(r.Context()); err != nil {
		writeLocalError(w, http.StatusServiceUnavailable, err)
		return
	}

	if !s.claimOrigin(target.origin) {
		writeLocalError(w, http.StatusConflict, errors.New(
			"a solve is already open for this origin; close that window first"))
		return
	}
	defer s.releaseOrigin(target.origin)

	result, solveErr := s.challenger.Solve(r.Context(), target.url)
	if result == nil {
		if solveErr == nil {
			solveErr = errors.New("the solve produced no result")
		}
		writeLocalError(w, http.StatusBadGateway, solveErr)
		return
	}

	response := solveResponse{
		Origin:           target.origin,
		Captured:         result.Captured,
		NeedsInteraction: result.NeedsInteraction,
		Challenge:        result.NeedsInteraction,
		Cookies:          []string{},
	}

	if result.Captured {
		if err := s.storeCapture(r, sourceID, target.origin, result); err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
		verified, err := s.engine.ProbeClearance(r.Context(), sourceID, target.origin)
		if err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
		response.Verified = verified
		response.Cookies = cookieNames(result.Capture.Cookies)
	}

	switch {
	case response.Verified:
		response.Message = "the challenge is cleared and the source is reachable"
	case response.Captured:
		response.Message = "material was captured but the site is still refusing requests"
	case response.NeedsInteraction:
		response.Message = "the challenge was not answered"
	default:
		response.Message = "the source served the request without challenging it"
	}

	writeJSON(w, http.StatusOK, response)
}

// claimOrigin reserves an origin for one solve and reports whether it was free.
func (s *Server) claimOrigin(origin string) bool {
	s.solving.Lock()
	defer s.solving.Unlock()
	if s.inFlight == nil {
		s.inFlight = make(map[string]struct{})
	}
	if _, held := s.inFlight[origin]; held {
		return false
	}
	s.inFlight[origin] = struct{}{}
	return true
}

// releaseOrigin frees an origin once its solve has finished.
func (s *Server) releaseOrigin(origin string) {
	s.solving.Lock()
	defer s.solving.Unlock()
	delete(s.inFlight, origin)
}

// storeCapture writes a captured bundle and releases any request waiting on it.
func (s *Server) storeCapture(r *http.Request, sourceID, origin string, result *solver.Result) error {
	return s.engine.SubmitClearanceBundle(sourceID, db.ClearanceBundle{
		Origin:    origin,
		Cookies:   result.Capture.Cookies,
		UserAgent: result.Capture.UserAgent,
		SecChUa:   result.Capture.SecChUa,
	})
}

// solveTarget is the address presented to the browser and the identity the
// clearance bundle is stored under.
type solveTarget struct {
	// origin is the bare host. Clearance is keyed by host, so it carries no port
	// and no scheme.
	origin string
	// url is the address to navigate to, using the scheme the source declared.
	url string
}

// resolveTarget returns the address to present, preferring the caller's choice
// and falling back to the source base address.
func (s *Server) resolveTarget(r *http.Request, sourceID, requested string) (solveTarget, error) {
	if requested = strings.TrimSpace(requested); requested != "" {
		return targetFor(requested)
	}
	// A source whose pages and images sit on different domains reports one entry
	// per origin, so the first outstanding one is the origin the reader is being
	// asked about.
	for _, state := range s.engine.ChallengeStates() {
		if state.SourceID == sourceID && state.Origin != "" {
			return targetFor(state.Origin)
		}
	}
	// The base address is read from the source record rather than asked of the
	// plugin, so a solve can be started for a source whose plugin has not been
	// loaded.
	baseURL, err := s.engine.SourceBaseURL(sourceID)
	if err != nil {
		return solveTarget{}, fmt.Errorf("resolve source: %w", err)
	}
	target, err := targetFor(baseURL)
	if err != nil {
		return solveTarget{}, err
	}
	return target, nil
}

// targetFor reduces a URL or bare host to a host and a navigable address. A bare
// host with no scheme is treated as HTTPS, which is what a registry source uses.
func targetFor(raw string) (solveTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return solveTarget{}, errors.New("no origin was given to present")
	}
	if !strings.Contains(raw, "//") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return solveTarget{}, errors.New("the origin could not be read as an address")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	// Host is used rather than Hostname so a non-default port survives. A registry
	// source has no port, so origin is the bare host in every real case.
	host := strings.ToLower(parsed.Host)
	if host == "" {
		return solveTarget{}, errors.New("the origin has no host to present")
	}
	return solveTarget{origin: host, url: scheme + "://" + host + "/"}, nil
}

// cookieNames lists cookie names in a stable order. Values are never returned
// through the interface.
func cookieNames(jar map[string]string) []string {
	names := make([]string, 0, len(jar))
	for name := range jar {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SetImageCache attaches the bounded retention policy for processed page
// images. Without it the cache directory grows without limits.
func (s *Server) SetImageCache(cache *imagecache.Cache) {
	s.imageCache = cache
}

// SetLifetimeContext supplies the daemon's run context so hijacked handlers
// (websockets) end when the daemon shuts down, not only when the client
// disconnects.
func (s *Server) SetLifetimeContext(ctx context.Context) {
	s.lifetime.Store(&ctx)
}

func NewTrackerServer(repo *db.Repository, eng *engine.Engine, downloads downloadQueue, trackers *tracker.Registry) *Server {
	server := NewServer(repo, eng, downloads)
	server.trackers = trackers
	server.syncer = &tracker.SyncWorker{Repo: repo, Registry: trackers}
	return server
}

func (s *Server) SetSettings(service *settings.Service) { s.settings = service }

// SetIncognito seeds the runtime no-trace flag from the persisted preference.
func (s *Server) SetIncognito(enabled bool) { s.incognito.Store(enabled) }

func (s *Server) SetUpdater(service *updater.Service) { s.updater = service }

// Mount registers the local REST API under /api.
func (s *Server) Mount(r chi.Router) {
	r.Route("/api", func(api chi.Router) {
		api.Get("/health", s.health)
		s.mountLibrary(api)
		s.mountMetadata(api)
		s.mountPrivacy(api)
		s.mountTachibackup(api)
		if s.settings != nil {
			s.mountSettings(api)
		}
		if s.updater != nil {
			s.mountUpdates(api)
		}
		s.mountBackup(api)
		s.mountSources(api)
		api.Get("/chapters/{chapterID}/pages", s.materializePages)
		api.Get("/pages/{pageID}/image", s.pageImage)
		s.mountMigration(api)
		if s.downloads != nil {
			s.mountDownloads(api)
		}
		if s.trackers != nil {
			s.mountTrackers(api)
		}
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{"ok": true, "version": version.Version}
	if commit := version.Commit; commit != "" && commit != "unknown" {
		status["commit"] = commit
	}
	if date := version.Date; date != "" && date != "unknown" {
		status["date"] = date
	}
	if err := s.repo.Ping(); err != nil {
		status["ok"] = false
		status["database"] = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, status)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// errorBody mirrors the plugin error envelope so the web client handles source
// failures and local failures with one code path.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    engine.ErrorCode `json:"code"`
	Message string           `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// writeError reports err with the status matching its standardized code.
func writeError(w http.ResponseWriter, err error) {
	code := engine.CodeOf(err)
	message := err.Error()
	var coded *engine.Error
	if errors.As(err, &coded) {
		message = coded.Message
	}
	writeJSON(w, engine.HTTPStatusFor(code), errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeBadRequest reports a malformed client request. Requests from the local
// web client are host side input, so they carry the parsing code rather than a
// source error code.
func writeBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, errorBody{
		Error: errorDetail{Code: engine.CodeParsingError, Message: message},
	})
}

// writeLocalError reports a failure that happened inside the daemon rather than
// in a source, under the status the caller chooses.
func writeLocalError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorBody{
		Error: errorDetail{Code: engine.CodeParsingError, Message: err.Error()},
	})
}

// decodeBody reads a JSON request body with a size limit.
func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeBadRequest(w, "the request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}
