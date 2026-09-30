package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

const DefaultWorkers = 3

type Engine interface {
	Details(ctx context.Context, sourceID, mangaID string) (engine.MangaDetails, error)
	Pages(ctx context.Context, sourceID, chapterID string) ([]engine.PageItem, error)
	FetchImage(ctx context.Context, sourceID, target string, headers map[string]string) ([]byte, error)
	Unscramble(ctx context.Context, sourceID string, data []byte) ([]byte, error)
	TransferHints(ctx context.Context, sourceID string) (engine.RateLimitHints, engine.RetryHints, error)
}

type Options struct {
	Workers      int
	PageInterval time.Duration
	DownloadDir  string
	MaxRetries   int
}

type ChapterSelection struct {
	IDs   []string
	Range string
}

type Stats struct {
	DownloadedPages   int64 `json:"downloadedPages"`
	RetriedRequests   int64 `json:"retriedRequests"`
	ThrottledRequests int64 `json:"throttledRequests"`
}

type Event struct {
	Type   string               `json:"type"`
	Item   db.DownloadQueueItem `json:"item"`
	Stats  Stats                `json:"stats"`
	Paused bool                 `json:"paused"`
}

type Queue struct {
	repo     *db.Repository
	engine   Engine
	options  Options
	archiver *Archiver
	limiter  *DomainLimiter
	events   *eventBroker
	wake     chan struct{}
	paused   atomic.Bool

	downloadedPages atomic.Int64
	retriedRequests atomic.Int64
	// policies caches the per-source pacing and retry policy derived from the
	// source's own transfer hints, which do not change while installed.
	policies sync.Map
}

// sourcePolicy is the effective pacing and retry policy for one source. A
// hint the source declares replaces the host default; an absent hint leaves
// the default in place.
type sourcePolicy struct {
	interval time.Duration
	retries  int
	backoff  time.Duration
}

func NewQueue(repo *db.Repository, sourceEngine Engine, options Options) *Queue {
	if options.Workers < 1 {
		options.Workers = DefaultWorkers
	}
	if options.PageInterval < 0 {
		options.PageInterval = 0
	}
	if options.MaxRetries <= 0 {
		options.MaxRetries = 3
	}
	return &Queue{
		repo: repo, engine: sourceEngine, options: options,
		archiver: NewArchiver(options.DownloadDir),
		limiter:  NewDomainLimiter(options.PageInterval),
		events:   newEventBroker(),
		wake:     make(chan struct{}, 1),
	}
}

func (q *Queue) Stats() Stats {
	return Stats{
		DownloadedPages:   q.downloadedPages.Load(),
		RetriedRequests:   q.retriedRequests.Load(),
		ThrottledRequests: q.limiter.WaitCount(),
	}
}

// policyFor returns the effective pacing and retry policy for a source. The
// source's own hints replace the host defaults; the result is cached because
// the hints do not change while the source is installed.
func (q *Queue) policyFor(ctx context.Context, sourceID string) sourcePolicy {
	if cached, ok := q.policies.Load(sourceID); ok {
		return cached.(sourcePolicy)
	}
	policy := sourcePolicy{
		interval: q.options.PageInterval,
		retries:  q.options.MaxRetries,
		backoff:  DefaultRetryBackoff,
	}
	if rate, retry, err := q.engine.TransferHints(ctx, sourceID); err == nil {
		if rate.IntervalMs != nil && *rate.IntervalMs > 0 {
			policy.interval = time.Duration(*rate.IntervalMs) * time.Millisecond
		}
		if retry.MaxAttempts != nil && *retry.MaxAttempts > 0 {
			policy.retries = int(*retry.MaxAttempts)
		}
		if retry.BackoffMs != nil && *retry.BackoffMs > 0 {
			policy.backoff = time.Duration(*retry.BackoffMs) * time.Millisecond
		}
	}
	q.policies.Store(sourceID, policy)
	return policy
}

// PauseAll stops the workers from claiming more work. An item that is already
// in flight is returned to the queue as it reaches its next boundary, so its
// fetched pages and progress carry over to the resume. The new state is
// announced, so clients that did not send the request follow along.
func (q *Queue) PauseAll() {
	q.paused.Store(true)
	q.publishState()
}

// ResumeAll lets the workers claim queued items again.
func (q *Queue) ResumeAll() {
	q.paused.Store(false)
	q.notify()
	q.publishState()
}

// publishState announces a downloader-level change that carries no queue item.
func (q *Queue) publishState() {
	q.events.publish(Event{Type: "state", Stats: q.Stats(), Paused: q.Paused()})
}

// Paused reports whether the downloader is paused.
func (q *Queue) Paused() bool {
	return q.paused.Load()
}

func (q *Queue) Subscribe() (<-chan Event, func()) {
	return q.events.subscribe()
}

func (q *Queue) List() ([]db.DownloadQueueItem, error) {
	items, err := q.repo.ListQueue()
	if items == nil {
		items = []db.DownloadQueueItem{}
	}
	return items, err
}

// EnqueueManga refreshes title and chapter metadata through the source, stores
// it locally, and adds the selected chapters to the persistent queue.
func (q *Queue) EnqueueManga(ctx context.Context, mangaID string, selection ChapterSelection, format string) ([]db.DownloadQueueItem, error) {
	mangaID = strings.TrimSpace(mangaID)
	if mangaID == "" {
		return nil, errors.New("manga id is required")
	}
	existingManga, err := q.repo.GetManga(mangaID)
	if err != nil {
		return nil, err
	}
	source, err := q.repo.GetMangaSource(mangaID)
	if err != nil {
		return nil, err
	}
	details, err := q.engine.Details(ctx, source.SourceID, source.SourceMangaID)
	if err != nil {
		return nil, err
	}
	if format == "" {
		format = FormatCBZ
		if existingManga.DownloadFormat != "" {
			format = existingManga.DownloadFormat
		}
	}
	manga, err := q.repo.UpsertManga(db.Manga{
		ID:             mangaID,
		SourceID:       source.SourceID,
		SourceMangaID:  source.SourceMangaID,
		Title:          details.Title,
		AltTitles:      jsonString(details.AltTitles),
		Description:    stringPointer(details.Description),
		Authors:        jsonString(details.Authors),
		Artists:        jsonString(details.Artists),
		Genres:         jsonString(details.Genres),
		Status:         details.Status,
		CoverURL:       engine.SelectCover(details.CoverURL, details.Covers, engine.PreferredCoverWidth),
		DownloadFormat: format,
	})
	if err != nil {
		return nil, err
	}
	// The details round-trip already happened, so record the freshness stamp:
	// opening the title must not repeat it.
	if err := q.repo.SetMangaDetailsFetched(manga.ID, time.Now().Unix()); err != nil {
		return nil, err
	}

	chapters := make([]db.Chapter, 0, len(details.Chapters))
	for _, item := range details.Chapters {
		chapter, err := q.repo.UpsertChapter(db.Chapter{
			MangaID:         manga.ID,
			SourceID:        source.SourceID,
			SourceChapterID: item.ID,
			ChapterNumber:   item.Number,
			Volume:          item.Volume,
			Title:           stringPointer(item.Title),
			Language:        stringPointer(item.Language),
			UploadedAt:      item.UploadedAt,
			Scanlator:       stringPointer(item.Scanlator),
			Locked:          item.Locked,
		})
		if err != nil {
			return nil, err
		}
		chapters = append(chapters, chapter)
	}
	selected, err := selectChapters(chapters, selection)
	if err != nil {
		return nil, err
	}
	items := make([]db.DownloadQueueItem, 0, len(selected))
	for _, chapter := range selected {
		item, err := q.repo.EnqueueChapter(chapter.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		q.publish("queued", item)
	}
	q.notify()
	return items, nil
}

// Run processes queue items in the background until ctx is canceled.
func (q *Queue) Run(ctx context.Context) error {
	if err := q.repo.ResetInterruptedQueue(); err != nil {
		return err
	}
	var workers sync.WaitGroup
	for index := 0; index < q.options.Workers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			q.runWorker(ctx)
		}()
	}
	<-ctx.Done()
	q.notify()
	workers.Wait()
	return nil
}

// Drain processes the currently pending queue and returns after every worker
// finds no more claimable items, reporting how many chapters finished. A paused
// downloader leaves its items untouched.
func (q *Queue) Drain(ctx context.Context) (int, error) {
	if err := q.repo.ResetInterruptedQueue(); err != nil {
		return 0, err
	}
	errs := make(chan error, q.options.Workers)
	var workers sync.WaitGroup
	var completed atomic.Int64
	for index := 0; index < q.options.Workers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				if q.paused.Load() {
					return
				}
				item, err := q.repo.ClaimNextQueueItem()
				if err != nil {
					errs <- err
					return
				}
				if item == nil {
					return
				}
				finished, err := q.process(ctx, *item)
				if err != nil {
					errs <- err
					continue
				}
				if finished {
					completed.Add(1)
				}
			}
		}()
	}
	workers.Wait()
	close(errs)
	var first error
	for err := range errs {
		if first == nil {
			first = err
		}
	}
	return int(completed.Load()), first
}

func (q *Queue) runWorker(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if q.paused.Load() {
			q.wait(ctx)
			continue
		}
		item, err := q.repo.ClaimNextQueueItem()
		if err != nil {
			slog.Warn("downloader claim failed", "err", err)
			q.wait(ctx)
			continue
		}
		if item == nil {
			q.wait(ctx)
			continue
		}
		if _, err := q.process(ctx, *item); err != nil {
			slog.Warn("downloader failed", "chapter", item.ChapterID, "err", err)
		}
	}
}

func (q *Queue) wait(ctx context.Context) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-q.wake:
	case <-timer.C:
	}
}

func (q *Queue) process(ctx context.Context, item db.DownloadQueueItem) (bool, error) {
	pages, err := q.engine.Pages(ctx, item.SourceID, item.SourceChapterID)
	if err != nil {
		return false, q.fail(item, err)
	}
	if len(pages) == 0 {
		return false, q.fail(item, errors.New("the source returned no pages for this chapter"))
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })
	done := parseDonePages(item.DonePagesJSON)
	// Fetched pages land in a per-item staging directory so an interrupted
	// attempt resumes from disk instead of refetching everything.
	tempDir := filepath.Join(q.options.DownloadDir, ".tmp", strconv.FormatInt(item.ID, 10))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return false, q.fail(item, err)
	}
	if err := q.repo.UpdateQueueProgress(item.ID, len(pages), len(done), nil); err != nil {
		if q.interrupted(item) {
			return false, nil
		}
		return false, q.fail(item, err)
	}

	downloaded := make([]PageData, 0, len(pages))
	policy := q.policyFor(ctx, item.SourceID)
	for index, page := range pages {
		if data, ext, ok := readStagedPage(tempDir, index); ok {
			downloaded = append(downloaded, PageData{Bytes: data, Extension: ext})
			continue
		}
		if q.interrupted(item) {
			return false, nil
		}
		data, err := retryFetchWith(ctx, policy.retries, policy.backoff, func(fetchCtx context.Context) ([]byte, error) {
			if err := q.limiter.WaitWithInterval(fetchCtx, page.URL, policy.interval); err != nil {
				return nil, err
			}
			return q.engine.FetchImage(fetchCtx, item.SourceID, page.URL, page.Headers)
		}, func(sleepCtx context.Context, delay time.Duration) error {
			q.retriedRequests.Add(1)
			return sleepContext(sleepCtx, delay)
		})
		if err != nil {
			return false, q.fail(item, err)
		}
		if q.interrupted(item) {
			return false, nil
		}
		if page.IsScrambled {
			data, err = q.engine.Unscramble(ctx, item.SourceID, data)
			if err != nil {
				return false, q.fail(item, err)
			}
		}
		if err := stagePage(tempDir, index, data, imageExtension(page.URL, data)); err != nil {
			return false, q.fail(item, err)
		}
		downloaded = append(downloaded, PageData{Bytes: data, Extension: imageExtension(page.URL, data)})
		done = append(done, index)
		q.downloadedPages.Add(1)
		if err := q.repo.UpdateQueueProgress(item.ID, len(pages), len(done), nil); err != nil {
			if q.interrupted(item) {
				return false, nil
			}
			return false, q.fail(item, err)
		}
		if err := q.repo.SaveQueuePageProgress(item.ID, len(pages), done); err != nil {
			if q.interrupted(item) {
				return false, nil
			}
			return false, q.fail(item, err)
		}
		q.publishCurrent("progress", item.ID)
	}
	if q.interrupted(item) {
		return false, nil
	}

	archivePath, err := q.archiver.Write(ArchiveRequest{
		SourceID:    item.SourceID,
		MangaTitle:  item.MangaTitle,
		ChapterName: chapterName(item),
		Format:      item.DownloadFormat,
		Pages:       downloaded,
		ComicInfo: ComicInfo{
			Title:       valueOr(item.ChapterTitle, chapterName(item)),
			Series:      item.MangaTitle,
			Number:      formatChapterNumber(item.ChapterNumber),
			Volume:      volumeOrZero(item.Volume),
			Summary:     valueOr(item.MangaDescription, ""),
			Writers:     decodeStrings(item.MangaAuthors),
			Pencillers:  decodeStrings(item.MangaArtists),
			Genres:      decodeStrings(item.MangaGenres),
			PageCount:   len(downloaded),
			LanguageISO: valueOr(item.Language, ""),
			SourceName:  item.SourceName,
		},
	})
	if err != nil {
		return false, q.fail(item, err)
	}
	// The fetched page list is persisted with the artifact so the reader can
	// open the chapter without contacting the source again.
	pageRows := make([]db.Page, 0, len(pages))
	for _, page := range pages {
		headersJSON := ""
		if len(page.Headers) > 0 {
			if raw, err := json.Marshal(page.Headers); err == nil {
				headersJSON = string(raw)
			}
		}
		headersCopy := headersJSON
		pageRows = append(pageRows, db.Page{
			ChapterID:   item.ChapterID,
			PageIndex:   page.Index,
			RemoteURL:   page.URL,
			HeadersJSON: &headersCopy,
			IsScrambled: page.IsScrambled,
		})
	}
	if _, err := q.repo.UpsertPages(item.ChapterID, pageRows); err != nil {
		q.removeArtifact(archivePath)
		return false, q.fail(item, err)
	}
	if err := q.repo.CompleteQueueDownload(item.ChapterID, archivePath); err != nil {
		q.removeArtifact(archivePath)
		return false, q.fail(item, err)
	}
	// The archive is final; staged pages are no longer needed.
	if err := os.RemoveAll(tempDir); err != nil {
		slog.Warn("downloader removing staged pages failed", "chapter", item.ChapterID, "err", err)
	}
	// The row left the queue when the chapter was marked downloaded, so the
	// event carries the finished item itself: clients drop a terminal row.
	item.Status = db.QueueCompleted
	item.Progress = 100
	q.publish("completed", item)
	return true, nil
}

// removeStaged deletes the staging directory of a row that will not resume, so
// a removed row cannot leave fetched pages behind.
func (q *Queue) removeStaged(id int64) {
	staged := filepath.Join(q.options.DownloadDir, ".tmp", strconv.FormatInt(id, 10))
	if err := os.RemoveAll(staged); err != nil {
		slog.Warn("downloader removing staged pages failed", "queue", id, "err", err)
	}
}

// removeArtifact deletes an archive that was written but never registered, so
// a failed bookkeeping write cannot leave an orphaned file on disk.
func (q *Queue) removeArtifact(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("downloader removing incomplete artifact failed", "path", path, "err", err)
	}
}

// parseDonePages reads the persisted page index list; unparsable content
// simply means "nothing to resume".
func parseDonePages(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var done []int
	_ = json.Unmarshal([]byte(raw), &done)
	return done
}

// stagePage stores one fetched page in the item's staging directory. The
// extension travels in the file name so a resumed pass can reconstruct the
// PageData without refetching.
func stagePage(dir string, index int, data []byte, ext string) error {
	name := filepath.Join(dir, fmt.Sprintf("%06d%s", index, ext))
	return os.WriteFile(name, data, 0o644)
}

// readStagedPage returns previously staged bytes for a page index, reporting
// false when the page was never staged or its file went missing.
func readStagedPage(dir string, index int) ([]byte, string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", false
	}
	for _, entry := range entries {
		name := entry.Name()
		var staged int
		if n, err := fmt.Sscanf(name, "%06d", &staged); err == nil && n == 1 && staged == index {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, "", false
			}
			return data, filepath.Ext(name), true
		}
	}
	return nil, "", false
}

func (q *Queue) fail(item db.DownloadQueueItem, cause error) error {
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	if q.stopped(item.ID) {
		return nil
	}
	if err := q.repo.MarkQueueFailed(item.ID, cause); err != nil {
		return fmt.Errorf("%v; recording failure: %w", cause, err)
	}
	q.publishCurrent("failed", item.ID)
	return cause
}

func (q *Queue) stopped(id int64) bool {
	item, err := q.repo.GetQueueItem(id)
	if err != nil {
		// A missing row means the item is no longer queued: it finished, was
		// canceled, or its chapter was retired. Either way there is nothing
		// left to work on.
		return true
	}
	return item.Status == db.QueuePaused
}

// interrupted reports whether work on the item must stop. A canceled or
// removed row stops as before; a paused downloader returns the item to the
// pending state with its progress and staged pages intact, so the resume
// continues from where it stopped instead of restarting the chapter.
func (q *Queue) interrupted(item db.DownloadQueueItem) bool {
	if q.stopped(item.ID) {
		// The row is gone or canceled, so its staged pages can never resume:
		// they are dropped together with the row.
		q.removeStaged(item.ID)
		return true
	}
	if !q.paused.Load() {
		return false
	}
	if err := q.repo.ReleaseQueueItem(item.ID); err != nil {
		slog.Warn("downloader releasing paused item failed", "chapter", item.ChapterID, "err", err)
	}
	q.publishCurrent("pending", item.ID)
	return true
}

func (q *Queue) Pause(id int64) error {
	if err := q.repo.PauseQueueItem(id); err != nil {
		return err
	}
	q.publishCurrent("paused", id)
	return nil
}

func (q *Queue) Resume(id int64) error {
	if err := q.repo.ResumeQueueItem(id); err != nil {
		return err
	}
	q.publishCurrent("resumed", id)
	q.notify()
	return nil
}

// Cancel removes a row from the queue. The removed row is published so open
// clients drop it, and its staged pages are discarded with it.
func (q *Queue) Cancel(id int64) error {
	item, err := q.repo.GetQueueItem(id)
	if err != nil {
		return err
	}
	if err := q.repo.CancelQueueItem(id); err != nil {
		return err
	}
	q.removeStaged(id)
	item.Status = db.QueueCanceled
	q.publish("canceled", item)
	return nil
}

// Retry returns a failed download to the pending state and wakes the worker.
func (q *Queue) Retry(id int64) error {
	if err := q.repo.RetryQueueItem(id); err != nil {
		return err
	}
	q.publishCurrent("pending", id)
	q.notify()
	return nil
}

// CancelAll removes every row that can still make progress and reports how
// many were affected. Each removed row is published so open clients drop it,
// and any staged pages go with it.
func (q *Queue) CancelAll() (int64, error) {
	items, err := q.repo.CancelAllQueueItems()
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		q.removeStaged(item.ID)
		item.Status = db.QueueCanceled
		q.publish("canceled", item)
	}
	return int64(len(items)), nil
}

// Reorder persists the queue order given as item ids in display order and
// announces the change, so clients can refetch the snapshot instead of trying
// to merge the ordering into their local state.
func (q *Queue) Reorder(ids []int64) error {
	if err := q.repo.SetQueueOrder(ids); err != nil {
		return err
	}
	q.events.publish(Event{Type: "reordered", Stats: q.Stats(), Paused: q.Paused()})
	return nil
}

func (q *Queue) publishCurrent(eventType string, id int64) {
	item, err := q.repo.GetQueueItem(id)
	if err == nil {
		q.publish(eventType, item)
	}
}

func (q *Queue) publish(eventType string, item db.DownloadQueueItem) {
	q.events.publish(Event{Type: eventType, Item: item, Stats: q.Stats(), Paused: q.Paused()})
}

func (q *Queue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func selectChapters(chapters []db.Chapter, selection ChapterSelection) ([]db.Chapter, error) {
	wanted := map[string]struct{}{}
	for _, id := range selection.IDs {
		wanted[strings.TrimSpace(id)] = struct{}{}
	}
	var lower, upper float64
	hasRange := strings.TrimSpace(selection.Range) != ""
	if hasRange {
		var err error
		lower, upper, err = parseChapterRange(selection.Range)
		if err != nil {
			return nil, err
		}
	}
	selected := make([]db.Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		include := false
		if _, ok := wanted[chapter.ID]; ok {
			include = true
		}
		if _, ok := wanted[chapter.SourceChapterID]; ok {
			include = true
		}
		if hasRange && chapter.ChapterNumber != nil && *chapter.ChapterNumber >= lower && *chapter.ChapterNumber <= upper {
			include = true
		}
		if include {
			selected = append(selected, chapter)
		}
	}
	if len(selected) == 0 {
		if len(wanted) == 0 && !hasRange {
			return nil, errors.New("no chapters selected")
		}
		return nil, errors.New("chapter selection matched no chapters")
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].ChapterNumber == nil {
			return false
		}
		if selected[j].ChapterNumber == nil {
			return true
		}
		return *selected[i].ChapterNumber < *selected[j].ChapterNumber
	})
	return selected, nil
}

func parseChapterRange(value string) (float64, float64, error) {
	parts := strings.SplitN(strings.TrimSpace(value), "-", 2)
	lower, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid chapter range %q", value)
	}
	upper := lower
	if len(parts) == 2 {
		upper, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid chapter range %q", value)
		}
	}
	if upper < lower {
		return 0, 0, fmt.Errorf("invalid chapter range %q", value)
	}
	return lower, upper, nil
}

func imageExtension(rawURL string, data []byte) string {
	if target, err := url.Parse(rawURL); err == nil {
		extension := strings.ToLower(path.Ext(target.Path))
		switch extension {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
			return extension
		}
	}
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	default:
		return ".jpg"
	}
}

func chapterName(item db.DownloadQueueItem) string {
	name := "Oneshot"
	if item.ChapterNumber != nil {
		name = "Chapter " + formatChapterNumber(item.ChapterNumber)
	}
	if item.ChapterTitle != nil && strings.TrimSpace(*item.ChapterTitle) != "" {
		name += " - " + strings.TrimSpace(*item.ChapterTitle)
	}
	if item.Language != nil && strings.TrimSpace(*item.Language) != "" {
		name += " [" + strings.TrimSpace(*item.Language) + "]"
	}
	return name
}

func formatChapterNumber(number *float64) string {
	if number == nil {
		return ""
	}
	return strconv.FormatFloat(*number, 'f', -1, 64)
}

func volumeOrZero(volume *int64) int {
	if volume == nil {
		return 0
	}
	return int(*volume)
}

func jsonString(values []string) *string {
	if len(values) == 0 {
		return nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	value := string(raw)
	return &value
}

func decodeStrings(raw *string) []string {
	if raw == nil || *raw == "" {
		return nil
	}
	var values []string
	_ = json.Unmarshal([]byte(*raw), &values)
	return values
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func valueOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

type eventBroker struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[int]chan Event
}

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: map[int]chan Event{}}
}

func (b *eventBroker) subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	channel := make(chan Event, 32)
	b.subscribers[id] = channel
	b.mu.Unlock()
	return channel, func() {
		b.mu.Lock()
		if current, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(current)
		}
		b.mu.Unlock()
	}
}

func (b *eventBroker) publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, channel := range b.subscribers {
		select {
		case channel <- event:
		default:
			// A slow subscriber's buffer is full; the event is dropped so the
			// download pipeline never blocks on a stalled reader. The
			// subscriber reconciles through its periodic snapshot refetch.
			slog.Warn("events subscriber buffer full, dropping event", "type", event.Type)
		}
	}
}
