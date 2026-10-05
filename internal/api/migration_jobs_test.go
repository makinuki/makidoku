package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// fakeSearcher stands in for the engine. Its callbacks let a test decide how
// each source answers, and it records how many searches ran at once.
type fakeSearcher struct {
	sources []engine.InstalledSource
	search  func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error)
	detail  func(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error)

	mu          sync.Mutex
	active      int
	maxActive   int
	searchCount int
	detailCount int
	queries     []string
}

func (f *fakeSearcher) Installed() ([]engine.InstalledSource, error) { return f.sources, nil }

func (f *fakeSearcher) Search(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
	f.mu.Lock()
	f.active++
	f.searchCount++
	f.queries = append(f.queries, query.Query)
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	if f.search != nil {
		return f.search(ctx, sourceID, query)
	}
	return engine.PageResult{}, nil
}

func (f *fakeSearcher) Details(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error) {
	f.mu.Lock()
	f.detailCount++
	f.mu.Unlock()
	if f.detail != nil {
		return f.detail(ctx, sourceID, mangaID)
	}
	return engine.MangaDetails{}, nil
}

func (f *fakeSearcher) stats() (maxActive, searchCount, detailCount int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive, f.searchCount, f.detailCount
}

func (f *fakeSearcher) seenQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

// fakeMigrationRepo stands in for the stored library and materialized stubs.
type fakeMigrationRepo struct {
	titles []db.Manga
	mu     sync.Mutex
	stubs  map[string]db.Manga
}

func (f *fakeMigrationRepo) ListLibraryBySource(sourceID string) ([]db.Manga, error) {
	out := make([]db.Manga, 0, len(f.titles))
	for _, manga := range f.titles {
		if manga.SourceID == sourceID {
			out = append(out, manga)
		}
	}
	return out, nil
}

func (f *fakeMigrationRepo) GetManga(id string) (db.Manga, error) {
	for _, manga := range f.titles {
		if manga.ID == id {
			return manga, nil
		}
	}
	return db.Manga{}, fmt.Errorf("manga %q not found", id)
}

func (f *fakeMigrationRepo) UpsertMangaStub(manga db.Manga) (db.Manga, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stubs == nil {
		f.stubs = map[string]db.Manga{}
	}
	key := manga.SourceID + "/" + manga.SourceMangaID
	if existing, ok := f.stubs[key]; ok {
		return existing, nil
	}
	manga.ID = "stub-" + manga.SourceMangaID
	f.stubs[key] = manga
	return manga, nil
}

func floatPtr(value float64) *float64 { return &value }

func migrationSpec(searcher migrationSearcher, repo migrationRepository, titles []db.Manga, targets []engine.InstalledSource) migrationJobSpec {
	return migrationJobSpec{
		searcher:      searcher,
		repo:          repo,
		parent:        context.Background(),
		sourceID:      "old",
		targets:       targets,
		titles:        titles,
		searchTimeout: 2 * time.Second,
	}
}

// drainMigration reads a job's frames until the stream closes and returns the
// last state seen for each title.
func drainMigration(t *testing.T, events <-chan migrationFrame) map[string]migrationTitleEvent {
	t.Helper()
	states := map[string]migrationTitleEvent{}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case frame, ok := <-events:
			if !ok {
				return states
			}
			if frame.Type == "title" && frame.Title != nil {
				states[frame.Title.MangaID] = *frame.Title
			}
		case <-timeout:
			t.Fatal("migration job did not finish")
			return states
		}
	}
}

func runMigrationJob(t *testing.T, job *migrationJob) map[string]migrationTitleEvent {
	t.Helper()
	_, events, unsubscribe := job.subscribe()
	defer unsubscribe()
	return drainMigration(t, events)
}

// A source that returns one candidate is trusted without scoring, so a test
// that wants scoring to reject the wrong source must give it two candidates.
func TestMigrationJobMatchesCandidateFromLaterSource(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		switch sourceID {
		case "a":
			return engine.PageResult{Items: []engine.MangaItem{
				{ID: "a1", Title: "Something Else Entirely"},
				{ID: "a2", Title: "Another Unrelated Title"},
			}}, nil
		case "b":
			return engine.PageResult{Items: []engine.MangaItem{{ID: "b1", Title: "Yosuga no Sora"}}}, nil
		}
		return engine.PageResult{}, nil
	}
	searcher.detail = func(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error) {
		return engine.MangaDetails{Chapters: []engine.ChapterItem{
			{Number: floatPtr(1)},
			{Number: floatPtr(12.5)},
			{Number: nil},
		}}, nil
	}
	repo := &fakeMigrationRepo{}
	job := newMigrationManager().start(migrationSpec(searcher, repo, []db.Manga{{ID: "lib1", Title: "Yosuga no Sora"}}, targets))

	states := runMigrationJob(t, job)
	got, ok := states["lib1"]
	if !ok {
		t.Fatalf("title missing from %+v", states)
	}
	if got.Status != migrationStatusSuccess {
		t.Fatalf("status = %q, want success (%+v)", got.Status, got)
	}
	if got.Source == nil || got.Source.ID != "b" {
		t.Fatalf("source = %+v, want the second target", got.Source)
	}
	if got.Manga == nil || got.Manga.ID != "stub-b1" {
		t.Fatalf("manga = %+v, want the materialized stub", got.Manga)
	}
	if got.ChapterCount != 3 {
		t.Fatalf("chapterCount = %d, want 3", got.ChapterCount)
	}
	if got.LatestChapter == nil || *got.LatestChapter != 12.5 {
		t.Fatalf("latestChapter = %v, want 12.5", got.LatestChapter)
	}
	if got.Score < 0.9 {
		t.Fatalf("score = %v, want a strong match", got.Score)
	}
}

func TestMigrationJobReportsNotFoundBelowThreshold(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		return engine.PageResult{Items: []engine.MangaItem{
			{ID: "a1", Title: "A Completely Different Work"},
			{ID: "a2", Title: "Nothing In Common At All"},
		}}, nil
	}
	job := newMigrationManager().start(migrationSpec(searcher, &fakeMigrationRepo{}, []db.Manga{{ID: "lib1", Title: "Yosuga no Sora"}}, targets))

	states := runMigrationJob(t, job)
	if states["lib1"].Status != migrationStatusNotFound {
		t.Fatalf("status = %q, want notFound", states["lib1"].Status)
	}
	if _, _, details := searcher.stats(); details != 0 {
		t.Fatalf("detail fetches = %d, want none for an unmatched title", details)
	}
}

func TestMigrationJobBoundsConcurrentTitles(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	titles := make([]db.Manga, 0, 12)
	for i := 0; i < 12; i++ {
		titles = append(titles, db.Manga{ID: fmt.Sprintf("lib%d", i), Title: fmt.Sprintf("Title %d", i)})
	}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		time.Sleep(30 * time.Millisecond)
		return engine.PageResult{Items: []engine.MangaItem{{ID: "m-" + query.Query, Title: query.Query}}}, nil
	}
	job := newMigrationManager().start(migrationSpec(searcher, &fakeMigrationRepo{}, titles, targets))

	runMigrationJob(t, job)
	maxActive, _, _ := searcher.stats()
	if maxActive > migrationTitleConcurrency {
		t.Fatalf("concurrent searches = %d, want at most %d", maxActive, migrationTitleConcurrency)
	}
	if maxActive < 2 {
		t.Fatalf("concurrent searches = %d, want the searches to overlap", maxActive)
	}
}

func TestMigrationJobTimesOutHangingSource(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		<-ctx.Done()
		return engine.PageResult{}, ctx.Err()
	}
	spec := migrationSpec(searcher, &fakeMigrationRepo{}, []db.Manga{{ID: "lib1", Title: "Yosuga no Sora"}}, targets)
	spec.searchTimeout = 40 * time.Millisecond
	job := newMigrationManager().start(spec)

	start := time.Now()
	states := runMigrationJob(t, job)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hanging source held the job for %s", elapsed)
	}
	// A source that never answers is a failure, not an empty result; reporting
	// it as notFound is the category error this distinction exists to avoid.
	if states["lib1"].Status != migrationStatusFailed {
		t.Fatalf("status = %q, want failed after the timeout", states["lib1"].Status)
	}
	if states["lib1"].Error == "" {
		t.Fatalf("failure carries no error detail: %+v", states["lib1"])
	}
}

func TestMigrationJobCancelTitleStopsOnlyThatTitle(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		if strings.Contains(query.Query, "Blocked") {
			<-ctx.Done()
			return engine.PageResult{}, ctx.Err()
		}
		return engine.PageResult{Items: []engine.MangaItem{{ID: "good", Title: query.Query}}}, nil
	}
	titles := []db.Manga{{ID: "block", Title: "Blocked Title"}, {ID: "ok", Title: "Fine Title"}}
	job := newMigrationManager().start(migrationSpec(searcher, &fakeMigrationRepo{}, titles, targets))

	_, events, unsubscribe := job.subscribe()
	defer unsubscribe()
	if !job.cancelTitle("block") {
		t.Fatal("cancelTitle reported the title missing")
	}
	if job.cancelTitle("nope") {
		t.Fatal("cancelTitle accepted an unknown title")
	}
	states := drainMigration(t, events)
	if states["block"].Status != migrationStatusCancelled {
		t.Fatalf("blocked status = %q, want cancelled", states["block"].Status)
	}
	if states["ok"].Status != migrationStatusSuccess {
		t.Fatalf("other status = %q, want success; one cancel must not stop the job", states["ok"].Status)
	}
}

func TestMigrationJobPrioritizeByChapters(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		return engine.PageResult{Items: []engine.MangaItem{{ID: sourceID + "1", Title: "Same Title"}}}, nil
	}
	searcher.detail = func(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error) {
		latest := floatPtr(10)
		if sourceID == "b" {
			latest = floatPtr(20)
		}
		return engine.MangaDetails{Chapters: []engine.ChapterItem{{Number: latest}}}, nil
	}
	spec := migrationSpec(searcher, &fakeMigrationRepo{}, []db.Manga{{ID: "lib1", Title: "Same Title"}}, targets)
	spec.prioritizeByChapters = true
	job := newMigrationManager().start(spec)

	states := runMigrationJob(t, job)
	got := states["lib1"]
	if got.Status != migrationStatusSuccess || got.Source == nil || got.Source.ID != "b" {
		t.Fatalf("chosen = %+v, want the source with the higher chapter number", got)
	}
	if got.LatestChapter == nil || *got.LatestChapter != 20 {
		t.Fatalf("latestChapter = %v, want 20", got.LatestChapter)
	}
	if _, _, details := searcher.stats(); details != 2 {
		t.Fatalf("detail fetches = %d, want one per source in prioritize mode", details)
	}
}

func TestMigrationJobDeepSearchExpandsQueries(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		return engine.PageResult{Items: []engine.MangaItem{{ID: "m1", Title: "[Group] Real Title (2020)"}}}, nil
	}
	spec := migrationSpec(searcher, &fakeMigrationRepo{}, []db.Manga{{ID: "lib1", Title: "[Group] Real Title (2020)"}}, targets)
	spec.deep = true
	job := newMigrationManager().start(spec)

	states := runMigrationJob(t, job)
	if states["lib1"].Status != migrationStatusSuccess {
		t.Fatalf("status = %q, want success", states["lib1"].Status)
	}
	queries := searcher.seenQueries()
	if len(queries) < 2 {
		t.Fatalf("queries = %v, want the deep search to expand the title", queries)
	}
	cleaned := false
	for _, query := range queries {
		if query == "real title" {
			cleaned = true
		}
		if strings.Contains(query, "[") {
			t.Fatalf("query %q kept the bracketed annotation", query)
		}
	}
	if !cleaned {
		t.Fatalf("queries = %v, want the cleaned form searched", queries)
	}
}

func migrationJobClosed(job *migrationJob) bool {
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.closed
}

func migrationJobStates(job *migrationJob) map[string]migrationTitleEvent {
	job.mu.Lock()
	defer job.mu.Unlock()
	out := map[string]migrationTitleEvent{}
	for _, state := range job.snapshotLocked() {
		out[state.MangaID] = state
	}
	return out
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// startTestServer mounts a server whose migration dependencies are fakes.
func startTestServer(t *testing.T, server *Server) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	server.Mount(router)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts
}

func wsURL(ts *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

// Closing the migration socket must stop the job's remaining searches, which
// is what keeps a page the reader left from issuing source requests.
func TestMigrationJobEventsStopWhenSocketCloses(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	started := make(chan struct{}, 2)
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return engine.PageResult{}, ctx.Err()
	}
	server := NewServer(nil, nil)
	server.searcher = searcher
	server.migrationRepo = &fakeMigrationRepo{}
	job := server.migration.start(migrationSpec(searcher, server.migrationRepo, []db.Manga{{ID: "t1", Title: "One"}, {ID: "t2", Title: "Two"}}, targets))

	ts := startTestServer(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, wsURL(ts, "/api/migration/jobs/"+job.id+"/events"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var snapshot migrationFrame
	if err := wsjson.Read(ctx, connection, &snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshot.Type != "snapshot" || len(snapshot.Titles) != 2 {
		t.Fatalf("snapshot = %+v, want both titles", snapshot)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no search started")
	}
	if err := connection.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}

	waitFor(t, "the job to stop after its socket closed", func() bool { return migrationJobClosed(job) })
	for id, state := range migrationJobStates(job) {
		if state.Status != migrationStatusCancelled {
			t.Fatalf("title %s status = %q, want cancelled", id, state.Status)
		}
	}
}

// A client that reconnects after the job settled is told what finished.
func TestMigrationJobReconnectReceivesFinishedSnapshot(t *testing.T) {
	targets := []engine.InstalledSource{{ID: "a", Name: "A"}}
	searcher := &fakeSearcher{sources: targets}
	searcher.search = func(ctx context.Context, sourceID string, query engine.SearchQuery) (engine.PageResult, error) {
		return engine.PageResult{Items: []engine.MangaItem{{ID: "m1", Title: query.Query}}}, nil
	}
	server := NewServer(nil, nil)
	server.searcher = searcher
	server.migrationRepo = &fakeMigrationRepo{}
	job := server.migration.start(migrationSpec(searcher, server.migrationRepo, []db.Manga{{ID: "lib1", Title: "Same Title"}}, targets))
	states := runMigrationJob(t, job)
	if states["lib1"].Status != migrationStatusSuccess {
		t.Fatalf("setup status = %q, want success", states["lib1"].Status)
	}

	ts := startTestServer(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, wsURL(ts, "/api/migration/jobs/"+job.id+"/events"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.CloseNow()
	var snapshot migrationFrame
	if err := wsjson.Read(ctx, connection, &snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !snapshot.Done {
		t.Fatalf("snapshot = %+v, want done set for a finished job", snapshot)
	}
	if len(snapshot.Titles) != 1 || snapshot.Titles[0].Status != migrationStatusSuccess {
		t.Fatalf("snapshot titles = %+v, want the finished title", snapshot.Titles)
	}
}

func TestCreateMigrationJobEndpoint(t *testing.T) {
	server := NewServer(nil, nil)
	searcher := &fakeSearcher{sources: []engine.InstalledSource{{ID: "old", Name: "Old"}, {ID: "a", Name: "A"}}}
	server.searcher = searcher
	server.migrationRepo = &fakeMigrationRepo{titles: []db.Manga{{ID: "lib1", SourceID: "old", Title: "Title"}}}
	ts := startTestServer(t, server)

	response, err := http.Post(ts.URL+"/api/migration/jobs", "application/json", strings.NewReader(`{"sourceId":"old"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		JobID string `json:"jobId"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	job := server.migration.get(payload.JobID)
	if job == nil {
		t.Fatalf("job %q missing", payload.JobID)
	}
	if len(job.spec.targets) != 1 || job.spec.targets[0].ID != "a" {
		t.Fatalf("targets = %+v, want only the source that is not the title's own", job.spec.targets)
	}
}

// A single title scopes the job to that title and takes its source from the
// stored row, which is what the details page entry point needs.
func TestCreateMigrationJobScopesToManga(t *testing.T) {
	server := NewServer(nil, nil)
	searcher := &fakeSearcher{sources: []engine.InstalledSource{{ID: "old", Name: "Old"}, {ID: "a", Name: "A"}}}
	server.searcher = searcher
	server.migrationRepo = &fakeMigrationRepo{titles: []db.Manga{
		{ID: "lib1", SourceID: "old", Title: "One"},
		{ID: "lib2", SourceID: "old", Title: "Two"},
	}}
	ts := startTestServer(t, server)

	response, err := http.Post(ts.URL+"/api/migration/jobs", "application/json", strings.NewReader(`{"mangaId":"lib2"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		JobID string `json:"jobId"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	job := server.migration.get(payload.JobID)
	if job == nil {
		t.Fatalf("job %q missing", payload.JobID)
	}
	if len(job.spec.titles) != 1 || job.spec.titles[0].ID != "lib2" {
		t.Fatalf("titles = %+v, want only lib2", job.spec.titles)
	}
	if job.spec.sourceID != "old" {
		t.Fatalf("source = %q, want the title's own source", job.spec.sourceID)
	}
	if len(job.spec.targets) != 1 || job.spec.targets[0].ID != "a" {
		t.Fatalf("targets = %+v, want the other source only", job.spec.targets)
	}
}
