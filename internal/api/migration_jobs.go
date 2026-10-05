package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/migration"
)

// migrationSearcher is the engine surface a migration job needs. It is an
// interface rather than the concrete engine so a job can be exercised without
// a compiled plugin.
type migrationSearcher interface {
	Installed() ([]engine.InstalledSource, error)
	Search(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error)
	Details(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error)
}

// migrationRepository is the stored-state surface a migration job needs.
type migrationRepository interface {
	ListLibraryBySource(sourceID string) ([]db.Manga, error)
	GetManga(id string) (db.Manga, error)
	UpsertMangaStub(manga db.Manga) (db.Manga, error)
}

// migrationTitleConcurrency caps the titles searched at once. A migration can
// hold many titles and each search fans out to several sources, so the cap is
// what keeps a large run from opening an unbounded number of transfers.
const migrationTitleConcurrency = 4

// migrationJobRetention is how long a finished job is kept so a client that
// reconnects can read what already resolved.
const migrationJobRetention = 10 * time.Minute

// Title states reported by the migration stream. The frontend shares these
// strings; searching is set on creation and every other value is terminal.
const (
	migrationStatusSearching = "searching"
	migrationStatusSuccess   = "success"
	migrationStatusNotFound  = "notFound"
	migrationStatusFailed    = "failed"
	migrationStatusCancelled = "cancelled"
)

// migrationTitleEvent is the state of one title in a migration job, streamed
// as it changes. Manga carries the materialized replacement once a match is
// found; Source names the plugin the match came from.
type migrationTitleEvent struct {
	MangaID       string                  `json:"mangaId"`
	Title         string                  `json:"title"`
	Status        string                  `json:"status"`
	Source        *engine.InstalledSource `json:"source,omitempty"`
	Manga         *db.Manga               `json:"manga,omitempty"`
	Score         float64                 `json:"score,omitempty"`
	ChapterCount  int                     `json:"chapterCount,omitempty"`
	LatestChapter *float64                `json:"latestChapter,omitempty"`
	Error         string                  `json:"error,omitempty"`
}

// migrationFrame is one message on the migration websocket. A snapshot frame
// carries every title's current state so a reconnecting client can resync
// without replaying events; title frames carry one update; complete marks the
// end of the job.
type migrationFrame struct {
	Type   string                `json:"type"`
	JobID  string                `json:"jobId"`
	Done   bool                  `json:"done,omitempty"`
	Title  *migrationTitleEvent  `json:"title,omitempty"`
	Titles []migrationTitleEvent `json:"titles,omitempty"`
}

// migrationJobSpec is the immutable input to a job. The mutable state lives on
// migrationJob.
type migrationJobSpec struct {
	searcher migrationSearcher
	repo     migrationRepository
	parent   context.Context

	sourceID             string
	query                string
	additionalQuery      string
	deep                 bool
	prioritizeByChapters bool
	targets              []engine.InstalledSource
	titles               []db.Manga

	// searchTimeout overrides the per-request bound. Tests set it small; the
	// default is migrationSearchTimeout.
	searchTimeout time.Duration
}

func (s migrationJobSpec) timeout() time.Duration {
	if s.searchTimeout > 0 {
		return s.searchTimeout
	}
	return migrationSearchTimeout
}

// migrationTitle is one mutable row of a job.
type migrationTitle struct {
	manga  db.Manga
	state  migrationTitleEvent
	cancel context.CancelFunc
	// cancelRequested records a cancellation that arrived before the worker
	// stored its cancel function, so the worker cancels as soon as it can.
	cancelRequested bool
}

// migrationJob tracks one migration run. Work starts when the first subscriber
// attaches and stops when the last one leaves or the job is cancelled; the
// resolved state is kept so a reconnecting client can be told what finished.
type migrationJob struct {
	id   string
	spec migrationJobSpec

	mu          sync.Mutex
	titles      map[string]*migrationTitle
	order       []string
	subscribers map[int]chan migrationFrame
	nextSub     int
	started     bool
	closed      bool
	closedAt    time.Time
	ctx         context.Context
	cancel      context.CancelFunc
}

// migrationManager owns the jobs the daemon has started, keyed by id.
type migrationManager struct {
	mu   sync.Mutex
	jobs map[string]*migrationJob
}

func newMigrationManager() *migrationManager {
	return &migrationManager{jobs: map[string]*migrationJob{}}
}

func (m *migrationManager) start(spec migrationJobSpec) *migrationJob {
	job := &migrationJob{
		id:          newMigrationJobID(),
		spec:        spec,
		titles:      make(map[string]*migrationTitle, len(spec.titles)),
		order:       make([]string, 0, len(spec.titles)),
		subscribers: map[int]chan migrationFrame{},
	}
	for i := range spec.titles {
		manga := spec.titles[i]
		job.titles[manga.ID] = &migrationTitle{
			manga: manga,
			state: migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusSearching},
		}
		job.order = append(job.order, manga.ID)
	}
	m.mu.Lock()
	m.pruneLocked(time.Now())
	m.jobs[job.id] = job
	m.mu.Unlock()
	return job
}

func (m *migrationManager) get(id string) *migrationJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

// pruneLocked drops jobs that finished long enough ago that no client can
// still be attached to them.
func (m *migrationManager) pruneLocked(now time.Time) {
	for id, job := range m.jobs {
		if job.expired(now) {
			delete(m.jobs, id)
		}
	}
}

func (j *migrationJob) expired(now time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.closed && now.Sub(j.closedAt) > migrationJobRetention
}

// subscribe registers a listener and starts the job's work on the first call.
// It returns the current snapshot, a channel of subsequent frames, and an
// unsubscribe function.
func (j *migrationJob) subscribe() (migrationFrame, <-chan migrationFrame, func()) {
	j.mu.Lock()
	snapshot := migrationFrame{Type: "snapshot", JobID: j.id, Done: j.closed, Titles: j.snapshotLocked()}
	if j.closed {
		j.mu.Unlock()
		channel := make(chan migrationFrame)
		close(channel)
		return snapshot, channel, func() {}
	}
	if !j.started {
		j.started = true
		j.ctx, j.cancel = context.WithCancel(j.spec.parent)
		go j.run()
	}
	id := j.nextSub
	j.nextSub++
	channel := make(chan migrationFrame, len(j.order)+16)
	j.subscribers[id] = channel
	j.mu.Unlock()
	return snapshot, channel, func() { j.unsubscribe(id) }
}

// unsubscribe removes a listener. When the last listener leaves, the job is
// cancelled: a page that navigated away must not keep issuing source requests.
func (j *migrationJob) unsubscribe(id int) {
	j.mu.Lock()
	if channel, ok := j.subscribers[id]; ok {
		delete(j.subscribers, id)
		close(channel)
	}
	cancel := j.cancel
	stop := len(j.subscribers) == 0 && j.started && !j.closed
	j.mu.Unlock()
	if stop && cancel != nil {
		cancel()
	}
}

func (j *migrationJob) snapshotLocked() []migrationTitleEvent {
	out := make([]migrationTitleEvent, 0, len(j.order))
	for _, id := range j.order {
		if title, ok := j.titles[id]; ok {
			out = append(out, title.state)
		}
	}
	return out
}

// publish delivers a frame to every listener without blocking. A listener
// whose buffer is full is skipped; it resyncs from the snapshot on reconnect.
func (j *migrationJob) publish(frame migrationFrame) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, channel := range j.subscribers {
		select {
		case channel <- frame:
		default:
		}
	}
}

// cancelAll stops the whole job. Before the first subscriber it simply marks
// the job closed so nothing starts later.
func (j *migrationJob) cancelAll() {
	j.mu.Lock()
	if !j.started {
		j.closed = true
		j.closedAt = time.Now()
		j.mu.Unlock()
		return
	}
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// cancelTitle stops one title's search, leaving the rest of the job running.
func (j *migrationJob) cancelTitle(mangaID string) bool {
	j.mu.Lock()
	title, ok := j.titles[mangaID]
	if !ok {
		j.mu.Unlock()
		return false
	}
	title.cancelRequested = true
	cancel := title.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

// run searches every title, bounded by migrationTitleConcurrency, and marks
// the job complete when they all settle.
func (j *migrationJob) run() {
	semaphore := make(chan struct{}, migrationTitleConcurrency)
	var group sync.WaitGroup
	for _, id := range j.order {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			j.work(id, semaphore)
		}(id)
	}
	group.Wait()
	j.complete()
}

func (j *migrationJob) work(id string, semaphore chan struct{}) {
	j.mu.Lock()
	title, ok := j.titles[id]
	var manga db.Manga
	if ok {
		manga = title.manga
	}
	j.mu.Unlock()
	if !ok {
		return
	}

	select {
	case semaphore <- struct{}{}:
	case <-j.ctx.Done():
		j.finish(title, cancelledTitle(manga))
		return
	}
	defer func() { <-semaphore }()

	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	j.mu.Lock()
	title.cancel = cancel
	pending := title.cancelRequested
	j.mu.Unlock()
	if pending {
		cancel()
	}

	event := j.searchTitle(ctx, manga)
	j.mu.Lock()
	title.cancel = nil
	j.mu.Unlock()
	j.finish(title, event)
}

func (j *migrationJob) finish(title *migrationTitle, event migrationTitleEvent) {
	if event.MangaID == "" {
		event.MangaID = title.manga.ID
	}
	if event.Title == "" {
		event.Title = title.manga.Title
	}
	j.mu.Lock()
	title.state = event
	j.mu.Unlock()
	j.publish(migrationFrame{Type: "title", JobID: j.id, Title: &event})
}

func (j *migrationJob) complete() {
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return
	}
	j.closed = true
	j.closedAt = time.Now()
	for id, channel := range j.subscribers {
		delete(j.subscribers, id)
		select {
		case channel <- migrationFrame{Type: "complete", JobID: j.id, Done: true}:
		default:
		}
		close(channel)
	}
	j.mu.Unlock()
}

// searchTitle resolves one title to a replacement. It reports notFound when no
// source produced a candidate over the threshold, and cancelled when the title
// or the job was stopped.
func (j *migrationJob) searchTitle(ctx context.Context, manga db.Manga) migrationTitleEvent {
	if ctx.Err() != nil {
		return cancelledTitle(manga)
	}
	base, queries := j.queries(manga)
	if len(queries) == 0 {
		return migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusNotFound}
	}
	if j.spec.prioritizeByChapters {
		return j.searchPrioritized(ctx, manga, base, queries)
	}
	return j.searchInOrder(ctx, manga, base, queries)
}

// queries builds the search terms. Without deep search it is a single query;
// with it the cleaned title is expanded into several, and additionalQuery is
// appended to each so a disambiguator applies to every search.
func (j *migrationJob) queries(manga db.Manga) (string, []string) {
	base := strings.TrimSpace(j.spec.query)
	if base == "" {
		base = strings.TrimSpace(manga.Title)
	}
	if base == "" {
		return "", nil
	}
	queries := []string{base}
	if j.spec.deep {
		if expanded := migration.DeepQueries(migration.CleanTitle(base)); len(expanded) > 0 {
			queries = expanded
		}
	}
	if extra := strings.TrimSpace(j.spec.additionalQuery); extra != "" {
		for i := range queries {
			queries[i] = strings.TrimSpace(queries[i] + " " + extra)
		}
	}
	return base, dedupeStrings(queries)
}

// searchInOrder walks the selected sources in order and keeps the first
// candidate that clears the threshold, which fetches chapters for one match
// per title.
func (j *migrationJob) searchInOrder(ctx context.Context, manga db.Manga, base string, queries []string) migrationTitleEvent {
	answered := false
	var firstErr error
	for _, source := range j.spec.targets {
		if ctx.Err() != nil {
			return cancelledTitle(manga)
		}
		items, err := j.searchSource(ctx, source, queries)
		if err != nil {
			if ctx.Err() != nil {
				return cancelledTitle(manga)
			}
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("migration source search failed", "source", source.Name, "err", err)
			continue
		}
		answered = true
		index, score, found := migration.Match(candidateTitles(items), base, len(queries), j.spec.deep)
		if !found {
			continue
		}
		event, ok := j.materialize(ctx, manga, source, items[index], score)
		if !ok {
			if ctx.Err() != nil {
				return cancelledTitle(manga)
			}
			continue
		}
		return event
	}
	if ctx.Err() != nil {
		return cancelledTitle(manga)
	}
	if !answered {
		// Every source failed. Reporting this as notFound would hide an outage
		// behind an empty result, which is the failure a migration must not
		// make invisible.
		return migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusFailed, Error: errorText(firstErr)}
	}
	return migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusNotFound}
}

// searchPrioritized consults every source, then keeps the match with the
// highest chapter number. This fetches chapters for one match per source and
// is what the prioritizeByChapters flag buys.
func (j *migrationJob) searchPrioritized(ctx context.Context, manga db.Manga, base string, queries []string) migrationTitleEvent {
	type match struct {
		source engine.InstalledSource
		item   engine.MangaItem
		score  float64
	}
	matches := make([]*match, len(j.spec.targets))
	failures := make([]error, len(j.spec.targets))
	var group sync.WaitGroup
	for i, source := range j.spec.targets {
		group.Add(1)
		go func(i int, source engine.InstalledSource) {
			defer group.Done()
			items, err := j.searchSource(ctx, source, queries)
			if err != nil {
				failures[i] = err
				return
			}
			index, score, found := migration.Match(candidateTitles(items), base, len(queries), j.spec.deep)
			if !found {
				return
			}
			matches[i] = &match{source: source, item: items[index], score: score}
		}(i, source)
	}
	group.Wait()
	if ctx.Err() != nil {
		return cancelledTitle(manga)
	}
	answered := false
	var firstErr error
	for i := range failures {
		if failures[i] == nil {
			answered = true
		} else if firstErr == nil {
			firstErr = failures[i]
		}
	}
	if !answered {
		return migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusFailed, Error: errorText(firstErr)}
	}

	var chosen *migrationTitleEvent
	for _, candidate := range matches {
		if candidate == nil {
			continue
		}
		event, ok := j.materialize(ctx, manga, candidate.source, candidate.item, candidate.score)
		if !ok {
			if ctx.Err() != nil {
				return cancelledTitle(manga)
			}
			continue
		}
		if chosen == nil || betterChapter(event, *chosen) {
			copied := event
			chosen = &copied
		}
	}
	if chosen == nil {
		return migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusNotFound}
	}
	return *chosen
}

// searchSource runs every query against one source concurrently and pools the
// results, dropping duplicates. It fails only when every query failed.
func (j *migrationJob) searchSource(ctx context.Context, source engine.InstalledSource, queries []string) ([]engine.MangaItem, error) {
	results := make([][]engine.MangaItem, len(queries))
	failures := make([]error, len(queries))
	var group sync.WaitGroup
	for i, query := range queries {
		group.Add(1)
		go func(i int, query string) {
			defer group.Done()
			searchCtx, cancel := context.WithTimeout(ctx, j.spec.timeout())
			defer cancel()
			page, err := j.spec.searcher.Search(searchCtx, source.ID, engine.SearchQuery{Query: query, Page: 1})
			if err != nil {
				failures[i] = err
				return
			}
			results[i] = page.Items
		}(i, query)
	}
	group.Wait()

	pool := make([]engine.MangaItem, 0)
	seen := make(map[string]struct{})
	succeeded := 0
	var firstErr error
	for i := range results {
		if failures[i] != nil {
			if firstErr == nil {
				firstErr = failures[i]
			}
			continue
		}
		succeeded++
		for _, item := range results[i] {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			pool = append(pool, item)
		}
	}
	if succeeded == 0 {
		if firstErr == nil {
			firstErr = errors.New("search returned no result")
		}
		return nil, firstErr
	}
	return pool, nil
}

// materialize stores the matched candidate as a stub so it has a local id, and
// fetches its chapter list for the count and latest chapter. A failed chapter
// fetch keeps the match; the count is then zero and the client shows none.
func (j *migrationJob) materialize(ctx context.Context, manga db.Manga, source engine.InstalledSource, item engine.MangaItem, score float64) (migrationTitleEvent, bool) {
	stub, err := j.spec.repo.UpsertMangaStub(db.Manga{
		SourceID:      source.ID,
		SourceMangaID: item.ID,
		SourcePageURL: item.URL,
		Title:         item.Title,
		CoverURL:      engine.SelectCover(item.CoverURL, item.Covers, engine.PreferredCoverWidth),
		Status:        "unknown",
	})
	if err != nil {
		slog.Warn("migration materializing candidate failed", "source", source.Name, "err", err)
		return migrationTitleEvent{}, false
	}

	var count int
	var latest *float64
	details, detailsErr := j.details(ctx, source.ID, item.ID)
	if detailsErr != nil {
		if ctx.Err() != nil {
			return migrationTitleEvent{}, false
		}
		slog.Warn("migration chapter fetch failed", "source", source.Name, "err", detailsErr)
		latest = latestChapterFromLabel(item.LatestChapter)
	} else {
		count, latest = chapterSummary(details, item)
	}

	entry := source
	return migrationTitleEvent{
		MangaID:       manga.ID,
		Title:         manga.Title,
		Status:        migrationStatusSuccess,
		Source:        &entry,
		Manga:         &stub,
		Score:         score,
		ChapterCount:  count,
		LatestChapter: latest,
	}, true
}

func (j *migrationJob) details(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error) {
	detailsCtx, cancel := context.WithTimeout(ctx, j.spec.timeout())
	defer cancel()
	return j.spec.searcher.Details(detailsCtx, sourceID, mangaID)
}

// betterChapter reports whether a is a better replacement than b, ranking by
// the highest chapter number and falling back to chapter count and score.
func betterChapter(a, b migrationTitleEvent) bool {
	if !sameChapterNumber(a.LatestChapter, b.LatestChapter) {
		return chapterNumber(a.LatestChapter) > chapterNumber(b.LatestChapter)
	}
	if a.ChapterCount != b.ChapterCount {
		return a.ChapterCount > b.ChapterCount
	}
	return a.Score > b.Score
}

func chapterNumber(value *float64) float64 {
	if value == nil {
		return -1
	}
	return *value
}

func sameChapterNumber(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// chapterSummary reports how many chapters the replacement has and the highest
// numbered one. Unnumbered entries do not count toward the latest number.
func chapterSummary(details engine.MangaDetails, item engine.MangaItem) (int, *float64) {
	count := len(details.Chapters)
	var latest *float64
	for i := range details.Chapters {
		number := details.Chapters[i].Number
		if number == nil {
			continue
		}
		if latest == nil || *number > *latest {
			value := *number
			latest = &value
		}
	}
	if latest == nil {
		latest = latestChapterFromLabel(item.LatestChapter)
	}
	return count, latest
}

// trailingNumber finds the last number in a label such as Ch. 120.5.
var trailingNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)

func latestChapterFromLabel(label string) *float64 {
	matches := trailingNumber.FindAllString(label, -1)
	if len(matches) == 0 {
		return nil
	}
	value, err := strconv.ParseFloat(matches[len(matches)-1], 64)
	if err != nil {
		return nil
	}
	return &value
}

func cancelledTitle(manga db.Manga) migrationTitleEvent {
	return migrationTitleEvent{MangaID: manga.ID, Title: manga.Title, Status: migrationStatusCancelled}
}

func errorText(err error) string {
	if err == nil {
		return "every source search failed"
	}
	return err.Error()
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// candidateTitles adapts source search results to the matcher's input.
func candidateTitles(items []engine.MangaItem) []migration.Candidate {
	candidates := make([]migration.Candidate, len(items))
	for i := range items {
		candidates[i] = migration.Candidate{ID: items[i].ID, Title: items[i].Title}
	}
	return candidates
}

func newMigrationJobID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer[:])
}

// selectTargets lists the sources a title can migrate to: every installed
// source except its own, narrowed to wanted when the caller named any.
func selectTargets(sources []engine.InstalledSource, own string, wanted []string) []engine.InstalledSource {
	allow := make(map[string]struct{}, len(wanted))
	for _, id := range wanted {
		if id = strings.TrimSpace(id); id != "" {
			allow[id] = struct{}{}
		}
	}
	out := make([]engine.InstalledSource, 0, len(sources))
	for _, source := range sources {
		if source.ID == own {
			continue
		}
		if len(allow) > 0 {
			if _, ok := allow[source.ID]; !ok {
				continue
			}
		}
		out = append(out, source)
	}
	return out
}

// createMigrationJob queues a search for every library title of a source and
// returns its id. Results are read from the job's websocket.
func (s *Server) createMigrationJob(w http.ResponseWriter, r *http.Request) {
	if s.searcher == nil || s.migrationRepo == nil || s.migration == nil {
		writeLocalError(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	var body struct {
		SourceID             string   `json:"sourceId"`
		MangaID              string   `json:"mangaId"`
		Query                string   `json:"query"`
		TargetSourceIDs      []string `json:"targetSourceIds"`
		Deep                 bool     `json:"deep"`
		PrioritizeByChapters bool     `json:"prioritizeByChapters"`
		AdditionalQuery      string   `json:"additionalQuery"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	body.SourceID = strings.TrimSpace(body.SourceID)
	body.MangaID = strings.TrimSpace(body.MangaID)
	if body.SourceID == "" && body.MangaID == "" {
		writeBadRequest(w, "sourceId or mangaId is required")
		return
	}
	sourceID := body.SourceID
	var titles []db.Manga
	if body.MangaID != "" {
		// A single title scopes the job to one row and takes its source from
		// the title, so the caller does not need to know it.
		manga, err := s.migrationRepo.GetManga(body.MangaID)
		if err != nil {
			writeLocalError(w, http.StatusNotFound, err)
			return
		}
		titles = []db.Manga{manga}
		sourceID = manga.SourceID
	} else {
		list, err := s.migrationRepo.ListLibraryBySource(body.SourceID)
		if err != nil {
			writeLocalError(w, http.StatusInternalServerError, err)
			return
		}
		titles = list
	}
	sources, err := s.searcher.Installed()
	if err != nil {
		writeError(w, err)
		return
	}
	job := s.migration.start(migrationJobSpec{
		searcher:             s.searcher,
		repo:                 s.migrationRepo,
		parent:               s.Lifetime(),
		sourceID:             sourceID,
		query:                body.Query,
		additionalQuery:      body.AdditionalQuery,
		deep:                 body.Deep,
		prioritizeByChapters: body.PrioritizeByChapters,
		targets:              selectTargets(sources, sourceID, body.TargetSourceIDs),
		titles:               titles,
	})
	writeJSON(w, http.StatusOK, map[string]any{"jobId": job.id, "count": len(titles), "sourceId": sourceID})
}

// migrationJobEvents streams one job's per-title results, starting with a
// snapshot so a client that reconnects can resync.
func (s *Server) migrationJobEvents(w http.ResponseWriter, r *http.Request) {
	job := s.migrationJob(chi.URLParam(r, "jobID"))
	if job == nil {
		writeLocalError(w, http.StatusNotFound, errors.New("migration job not found"))
		return
	}
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()

	ctx := connection.CloseRead(s.Lifetime())
	snapshot, events, unsubscribe := job.subscribe()
	defer unsubscribe()
	if err := wsjson.Write(ctx, connection, snapshot); err != nil {
		return
	}
	for {
		select {
		case frame, ok := <-events:
			if !ok {
				_ = connection.Close(websocket.StatusNormalClosure, "")
				return
			}
			if err := wsjson.Write(ctx, connection, frame); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// cancelMigrationJob stops every remaining search in a job.
func (s *Server) cancelMigrationJob(w http.ResponseWriter, r *http.Request) {
	job := s.migrationJob(chi.URLParam(r, "jobID"))
	if job == nil {
		writeLocalError(w, http.StatusNotFound, errors.New("migration job not found"))
		return
	}
	job.cancelAll()
	w.WriteHeader(http.StatusNoContent)
}

// cancelMigrationTitle stops one title's search and leaves the rest running.
func (s *Server) cancelMigrationTitle(w http.ResponseWriter, r *http.Request) {
	job := s.migrationJob(chi.URLParam(r, "jobID"))
	if job == nil {
		writeLocalError(w, http.StatusNotFound, errors.New("migration job not found"))
		return
	}
	if !job.cancelTitle(chi.URLParam(r, "mangaID")) {
		writeLocalError(w, http.StatusNotFound, errors.New("title is not part of this job"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// migrationJob looks up a job, tolerating a Server built without NewServer.
func (s *Server) migrationJob(id string) *migrationJob {
	if s.migration == nil {
		return nil
	}
	return s.migration.get(id)
}
