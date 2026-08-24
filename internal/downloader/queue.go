package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	Type  string               `json:"type"`
	Item  db.DownloadQueueItem `json:"item"`
	Stats Stats                `json:"stats"`
}

type Queue struct {
	repo     *db.Repository
	engine   Engine
	options  Options
	archiver *Archiver
	limiter  *DomainLimiter
	events   *eventBroker
	wake     chan struct{}

	downloadedPages atomic.Int64
	retriedRequests atomic.Int64
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
// finds no more claimable items. Paused and canceled items are left untouched.
func (q *Queue) Drain(ctx context.Context) error {
	if err := q.repo.ResetInterruptedQueue(); err != nil {
		return err
	}
	errs := make(chan error, q.options.Workers)
	var workers sync.WaitGroup
	for index := 0; index < q.options.Workers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				if ctx.Err() != nil {
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
				if err := q.process(ctx, *item); err != nil {
					errs <- err
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
	return first
}

func (q *Queue) runWorker(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		item, err := q.repo.ClaimNextQueueItem()
		if err != nil {
			log.Printf("downloader: claim failed: %v", err)
			q.wait(ctx)
			continue
		}
		if item == nil {
			q.wait(ctx)
			continue
		}
		if err := q.process(ctx, *item); err != nil {
			log.Printf("downloader: %s failed: %v", item.ChapterID, err)
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

func (q *Queue) process(ctx context.Context, item db.DownloadQueueItem) error {
	pages, err := q.engine.Pages(ctx, item.SourceID, item.SourceChapterID)
	if err != nil {
		return q.fail(item, err)
	}
	if len(pages) == 0 {
		return q.fail(item, errors.New("source returned no pages"))
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })
	done := parseDonePages(item.DonePagesJSON)
	// Fetched pages land in a per-item staging directory so an interrupted
	// attempt resumes from disk instead of refetching everything.
	tempDir := filepath.Join(q.options.DownloadDir, ".tmp", strconv.FormatInt(item.ID, 10))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return q.fail(item, err)
	}
	if err := q.repo.UpdateQueueProgress(item.ID, len(pages), len(done), nil); err != nil {
		if q.stopped(item.ID) {
			return nil
		}
		return q.fail(item, err)
	}

	downloaded := make([]PageData, 0, len(pages))
	for index, page := range pages {
		if data, ext, ok := readStagedPage(tempDir, index); ok {
			downloaded = append(downloaded, PageData{Bytes: data, Extension: ext})
			continue
		}
		if q.stopped(item.ID) {
			return nil
		}
		data, err := retryFetch(ctx, q.options.MaxRetries, func(fetchCtx context.Context) ([]byte, error) {
			if err := q.limiter.Wait(fetchCtx, page.URL); err != nil {
				return nil, err
			}
			return q.engine.FetchImage(fetchCtx, item.SourceID, page.URL, page.Headers)
		}, func(sleepCtx context.Context, delay time.Duration) error {
			q.retriedRequests.Add(1)
			return sleepContext(sleepCtx, delay)
		})
		if err != nil {
			return q.fail(item, err)
		}
		if q.stopped(item.ID) {
			return nil
		}
		if page.IsScrambled {
			data, err = q.engine.Unscramble(ctx, item.SourceID, data)
			if err != nil {
				return q.fail(item, err)
			}
		}
		if err := stagePage(tempDir, index, data, imageExtension(page.URL, data)); err != nil {
			return q.fail(item, err)
		}
		downloaded = append(downloaded, PageData{Bytes: data, Extension: imageExtension(page.URL, data)})
		done = append(done, index)
		q.downloadedPages.Add(1)
		if err := q.repo.UpdateQueueProgress(item.ID, len(pages), len(done), nil); err != nil {
			if q.stopped(item.ID) {
				return nil
			}
			return q.fail(item, err)
		}
		if err := q.repo.SaveQueuePageProgress(item.ID, len(pages), done); err != nil {
			if q.stopped(item.ID) {
				return nil
			}
			return q.fail(item, err)
		}
		q.publishCurrent("progress", item.ID)
	}
	if q.stopped(item.ID) {
		return nil
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
		return q.fail(item, err)
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
		return q.fail(item, err)
	}
	if err := q.repo.MarkChapterDownloaded(item.ChapterID, archivePath); err != nil {
		q.removeArtifact(archivePath)
		return q.fail(item, err)
	}
	// The archive is final; staged pages are no longer needed.
	if err := os.RemoveAll(tempDir); err != nil {
		log.Printf("downloader: removing staged pages for %s failed: %v", item.ChapterID, err)
	}
	q.publishCurrent("completed", item.ID)
	return nil
}

// removeArtifact deletes an archive that was written but never registered, so
// a failed bookkeeping write cannot leave an orphaned file on disk.
func (q *Queue) removeArtifact(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("downloader: removing incomplete artifact %s failed: %v", path, err)
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
		// A missing row means the item no longer exists (for example its
		// chapter was retired by a migration): stop working on it.
		return true
	}
	return item.Status == db.QueuePaused || item.Status == db.QueueCanceled
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

func (q *Queue) Cancel(id int64) error {
	if err := q.repo.CancelQueueItem(id); err != nil {
		return err
	}
	q.publishCurrent("canceled", id)
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

// ClearFinished deletes terminal queue rows. It reports how many rows went
// away so the caller can answer with the removed count.
func (q *Queue) ClearFinished() (int64, error) {
	removed, err := q.repo.ClearFinishedQueueItems()
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (q *Queue) publishCurrent(eventType string, id int64) {
	item, err := q.repo.GetQueueItem(id)
	if err == nil {
		q.publish(eventType, item)
	}
}

func (q *Queue) publish(eventType string, item db.DownloadQueueItem) {
	q.events.publish(Event{Type: eventType, Item: item, Stats: q.Stats()})
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
			log.Printf("events: subscriber buffer full, dropping %s event", event.Type)
		}
	}
}
