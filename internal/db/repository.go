package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Repository groups typed queries. Currently it provides only health helpers;
// domain queries are added as the matching subsystems land.

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

// DB exposes the underlying handle for callers that need raw sqlx.
func (r *Repository) DB() *sqlx.DB { return r.db }

// Ping verifies the connection.
func (r *Repository) Ping() error { return r.db.Ping() }

// Category helpers (used by the library API).

func (r *Repository) ListCategories() ([]Category, error) {
	var out []Category
	err := r.db.Select(&out, `SELECT id, name, sort_order FROM categories ORDER BY sort_order, name`)
	return out, err
}

func (r *Repository) CreateCategory(name string, sortOrder int) (Category, error) {
	res, err := r.db.Exec(`INSERT INTO categories(name, sort_order) VALUES(?, ?)`, name, sortOrder)
	if err != nil {
		return Category{}, err
	}
	id, _ := res.LastInsertId()
	return Category{ID: id, Name: name, SortOrder: sortOrder}, nil
}

func (r *Repository) UpsertManga(manga Manga) (Manga, error) {
	manga.SourceID = strings.TrimSpace(manga.SourceID)
	manga.SourceMangaID = strings.TrimSpace(manga.SourceMangaID)
	if manga.SourceID == "" || manga.SourceMangaID == "" {
		return Manga{}, errors.New("source id and source manga id are required")
	}
	manga.ID = manga.SourceID + ":" + manga.SourceMangaID
	if manga.DownloadFormat == "" {
		manga.DownloadFormat = "cbz"
	}
	if manga.DownloadFormat != "cbz" && manga.DownloadFormat != "folder" {
		return Manga{}, fmt.Errorf("unsupported download format %q", manga.DownloadFormat)
	}
	now := time.Now().Unix()
	if manga.CreatedAt == 0 {
		manga.CreatedAt = now
	}
	if manga.UpdatedAt == 0 {
		manga.UpdatedAt = now
	}

	_, err := r.db.Exec(`INSERT INTO manga(
		id, source_id, source_manga_id, title, alt_titles, description,
		authors, artists, genres, status, cover_url, in_library,
		download_format, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title = excluded.title,
		alt_titles = excluded.alt_titles,
		description = excluded.description,
		authors = excluded.authors,
		artists = excluded.artists,
		genres = excluded.genres,
		status = excluded.status,
		cover_url = excluded.cover_url,
		download_format = excluded.download_format,
		updated_at = excluded.updated_at`,
		manga.ID, manga.SourceID, manga.SourceMangaID, manga.Title,
		manga.AltTitles, manga.Description, manga.Authors, manga.Artists,
		manga.Genres, manga.Status, manga.CoverURL, manga.InLibrary,
		manga.DownloadFormat, manga.CreatedAt, manga.UpdatedAt)
	if err != nil {
		return Manga{}, fmt.Errorf("upsert manga %s: %w", manga.ID, err)
	}
	return r.GetManga(manga.ID)
}

func (r *Repository) GetManga(id string) (Manga, error) {
	var manga Manga
	err := r.db.Get(&manga, `SELECT id, source_id, source_manga_id, title,
		alt_titles, description, authors, artists, genres, status, cover_url,
		in_library, download_format, created_at, updated_at
		FROM manga WHERE id = ?`, id)
	return manga, err
}

func (r *Repository) UpsertChapter(chapter Chapter) (Chapter, error) {
	chapter.MangaID = strings.TrimSpace(chapter.MangaID)
	chapter.SourceChapterID = strings.TrimSpace(chapter.SourceChapterID)
	if chapter.MangaID == "" || chapter.SourceChapterID == "" {
		return Chapter{}, errors.New("manga id and source chapter id are required")
	}
	chapter.ID = chapter.MangaID + ":" + chapter.SourceChapterID
	_, err := r.db.Exec(`INSERT INTO chapters(
		id, manga_id, source_chapter_id, chapter_number, title, language,
		uploaded_at, scanlator, downloaded, download_path
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		chapter_number = excluded.chapter_number,
		title = excluded.title,
		language = excluded.language,
		uploaded_at = excluded.uploaded_at,
		scanlator = excluded.scanlator`,
		chapter.ID, chapter.MangaID, chapter.SourceChapterID,
		chapter.ChapterNumber, chapter.Title, chapter.Language,
		chapter.UploadedAt, chapter.Scanlator, chapter.Downloaded,
		chapter.DownloadPath)
	if err != nil {
		return Chapter{}, fmt.Errorf("upsert chapter %s: %w", chapter.ID, err)
	}
	return r.GetChapter(chapter.ID)
}

func (r *Repository) GetChapter(id string) (Chapter, error) {
	var chapter Chapter
	err := r.db.Get(&chapter, `SELECT id, manga_id, source_chapter_id,
		chapter_number, title, language, uploaded_at, scanlator, downloaded,
		download_path FROM chapters WHERE id = ?`, id)
	return chapter, err
}

func (r *Repository) ListChapters(mangaID string) ([]Chapter, error) {
	var chapters []Chapter
	err := r.db.Select(&chapters, `SELECT id, manga_id, source_chapter_id,
		chapter_number, title, language, uploaded_at, scanlator, downloaded,
		download_path FROM chapters WHERE manga_id = ?
		ORDER BY chapter_number IS NULL, chapter_number, source_chapter_id`, mangaID)
	return chapters, err
}

func (r *Repository) UpsertReadingProgress(progress ReadingProgress) (ReadingProgress, error) {
	if strings.TrimSpace(progress.MangaID) == "" || strings.TrimSpace(progress.LastReadChapterID) == "" {
		return ReadingProgress{}, errors.New("manga id and chapter id are required")
	}
	if progress.LastReadPage < 1 || progress.TotalPages < 1 || progress.LastReadPage > progress.TotalPages {
		return ReadingProgress{}, errors.New("invalid reading progress")
	}
	var chapterManga string
	if err := r.db.Get(&chapterManga, `SELECT manga_id FROM chapters WHERE id=?`, progress.LastReadChapterID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReadingProgress{}, errors.New("last read chapter does not exist")
		}
		return ReadingProgress{}, err
	}
	if chapterManga != progress.MangaID {
		return ReadingProgress{}, errors.New("last read chapter does not belong to manga")
	}
	progress.LastReadAt = time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO reading_progress(
		manga_id, last_read_chapter_id, last_read_page, total_pages, is_completed, last_read_at
	) VALUES(?, ?, ?, ?, ?, ?)
	ON CONFLICT(manga_id) DO UPDATE SET last_read_chapter_id=excluded.last_read_chapter_id,
	last_read_page=excluded.last_read_page, total_pages=excluded.total_pages,
	is_completed=excluded.is_completed, last_read_at=excluded.last_read_at`,
		progress.MangaID, progress.LastReadChapterID, progress.LastReadPage,
		progress.TotalPages, progress.IsCompleted, progress.LastReadAt)
	if err != nil {
		return ReadingProgress{}, err
	}
	return r.GetReadingProgress(progress.MangaID)
}

func (r *Repository) GetReadingProgress(mangaID string) (ReadingProgress, error) {
	var p ReadingProgress
	err := r.db.Get(&p, `SELECT manga_id, last_read_chapter_id, last_read_page, total_pages,
		is_completed, last_read_at FROM reading_progress WHERE manga_id = ?`, mangaID)
	return p, err
}

func (r *Repository) UpsertTrackerBinding(binding TrackerBinding) (TrackerBinding, error) {
	if strings.TrimSpace(binding.MangaID) == "" || strings.TrimSpace(binding.TrackerType) == "" || strings.TrimSpace(binding.RemoteID) == "" {
		return TrackerBinding{}, errors.New("manga id, tracker type and remote id are required")
	}
	existing, lookupErr := r.GetTrackerBinding(binding.MangaID, binding.TrackerType)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return TrackerBinding{}, lookupErr
	}
	rebound := lookupErr == nil && existing.RemoteID != binding.RemoteID
	_, err := r.db.Exec(`INSERT INTO tracker_bindings(
		manga_id, tracker_type, remote_id, remote_title, remote_score, remote_status,
		last_synced_chapter, total_remote_chapters
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(manga_id, tracker_type) DO UPDATE SET remote_id=excluded.remote_id,
	remote_title=excluded.remote_title, remote_score=excluded.remote_score,
	remote_status=excluded.remote_status, total_remote_chapters=excluded.total_remote_chapters`,
		binding.MangaID, binding.TrackerType, binding.RemoteID, binding.RemoteTitle,
		binding.RemoteScore, binding.RemoteStatus, binding.LastSyncedChapter, binding.TotalRemoteChapters)
	if err != nil {
		return TrackerBinding{}, err
	}
	if rebound {
		if _, err := r.db.Exec(`DELETE FROM tracker_sync_jobs WHERE binding_id=?`, existing.ID); err != nil {
			return TrackerBinding{}, err
		}
		if _, err := r.db.Exec(`UPDATE tracker_bindings SET last_synced_chapter=0 WHERE id=?`, existing.ID); err != nil {
			return TrackerBinding{}, err
		}
	}
	return r.GetTrackerBinding(binding.MangaID, binding.TrackerType)
}

func (r *Repository) GetTrackerBinding(mangaID, trackerType string) (TrackerBinding, error) {
	var b TrackerBinding
	err := r.db.Get(&b, `SELECT id, manga_id, tracker_type, remote_id, remote_title, remote_score,
		remote_status, last_synced_chapter, total_remote_chapters FROM tracker_bindings WHERE manga_id=? AND tracker_type=?`, mangaID, trackerType)
	return b, err
}

func (r *Repository) GetTrackerBindingByID(id int64) (TrackerBinding, error) {
	var b TrackerBinding
	err := r.db.Get(&b, `SELECT id, manga_id, tracker_type, remote_id, remote_title, remote_score, remote_status,
		last_synced_chapter, total_remote_chapters FROM tracker_bindings WHERE id=?`, id)
	return b, err
}

func (r *Repository) ListTrackerBindings(mangaID string) ([]TrackerBinding, error) {
	var out []TrackerBinding
	err := r.db.Select(&out, `SELECT id, manga_id, tracker_type, remote_id, remote_title, remote_score,
		remote_status, last_synced_chapter, total_remote_chapters FROM tracker_bindings WHERE manga_id=? ORDER BY tracker_type`, mangaID)
	return out, err
}

func (r *Repository) DeleteTrackerBinding(mangaID, trackerType string) error {
	_, err := r.db.Exec(`DELETE FROM tracker_bindings WHERE manga_id=? AND tracker_type=?`, mangaID, trackerType)
	return err
}

func (r *Repository) SaveTrackerCredential(trackerType string, accessToken, refreshToken []byte, expiresAt *int64, metadata []byte) error {
	now := time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO tracker_credentials(tracker_type,access_token,refresh_token,expires_at,metadata,created_at,updated_at)
	VALUES(?,?,?,?,?,?,?) ON CONFLICT(tracker_type) DO UPDATE SET access_token=excluded.access_token,refresh_token=excluded.refresh_token,
	expires_at=excluded.expires_at,metadata=excluded.metadata,updated_at=excluded.updated_at`, trackerType, accessToken, refreshToken, expiresAt, metadata, now, now)
	return err
}

func (r *Repository) LoadTrackerCredential(trackerType string) (TrackerCredentialRecord, error) {
	var c TrackerCredentialRecord
	err := r.db.Get(&c, `SELECT tracker_type,access_token,refresh_token,expires_at,metadata FROM tracker_credentials WHERE tracker_type=?`, trackerType)
	return c, err
}

func (r *Repository) DeleteTrackerCredential(trackerType string) error {
	_, err := r.db.Exec(`DELETE FROM tracker_credentials WHERE tracker_type=?`, trackerType)
	return err
}

func (r *Repository) ListTrackerCredentials() ([]TrackerCredential, error) {
	var out []TrackerCredential
	err := r.db.Select(&out, `SELECT tracker_type,expires_at,created_at,updated_at FROM tracker_credentials ORDER BY tracker_type`)
	return out, err
}

func (r *Repository) UpdateTrackerSyncedChapter(bindingID int64, chapter float64) error {
	_, err := r.db.Exec(`UPDATE tracker_bindings SET last_synced_chapter = CASE WHEN last_synced_chapter > ? THEN last_synced_chapter ELSE ? END WHERE id=?`, chapter, chapter, bindingID)
	return err
}

func (r *Repository) EnqueueTrackerSync(mangaID string, bindingID int64, chapter float64) (TrackerSyncJob, error) {
	now := time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO tracker_sync_jobs(manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,created_at)
		VALUES(?,?,?,'PENDING',0,?,?) ON CONFLICT(manga_id,binding_id,chapter_number) DO UPDATE SET
		status=CASE WHEN tracker_sync_jobs.status='COMPLETED' THEN tracker_sync_jobs.status ELSE 'PENDING' END,
		next_attempt_at=CASE WHEN tracker_sync_jobs.status='COMPLETED' THEN tracker_sync_jobs.next_attempt_at ELSE excluded.next_attempt_at END,
		error_message=CASE WHEN tracker_sync_jobs.status='COMPLETED' THEN tracker_sync_jobs.error_message ELSE NULL END`, mangaID, bindingID, chapter, now, now)
	if err != nil {
		return TrackerSyncJob{}, err
	}
	var job TrackerSyncJob
	err = r.db.Get(&job, `SELECT id,manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,error_message,created_at,completed_at FROM tracker_sync_jobs WHERE manga_id=? AND binding_id=? AND chapter_number=?`, mangaID, bindingID, chapter)
	return job, err
}

func (r *Repository) ClaimTrackerSync(now int64) (*TrackerSyncJob, error) {
	return r.claimTrackerSync(now, "")
}

func (r *Repository) ClaimTrackerSyncForTracker(now int64, trackerType string) (*TrackerSyncJob, error) {
	return r.claimTrackerSync(now, trackerType)
}

func (r *Repository) claimTrackerSync(now int64, trackerType string) (*TrackerSyncJob, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var query string
	var args []any
	if trackerType == "" {
		query = `SELECT id FROM tracker_sync_jobs WHERE status='PENDING' AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT 1`
		args = []any{now}
	} else {
		query = `SELECT j.id FROM tracker_sync_jobs j JOIN tracker_bindings b ON b.id=j.binding_id WHERE j.status='PENDING' AND j.next_attempt_at<=? AND b.tracker_type=? ORDER BY j.next_attempt_at,j.id LIMIT 1`
		args = []any{now, trackerType}
	}
	if err := tx.Get(&id, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE tracker_sync_jobs SET status='RUNNING' WHERE id=?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var job TrackerSyncJob
	if err := r.db.Get(&job, `SELECT id,manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,error_message,created_at,completed_at FROM tracker_sync_jobs WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) CompleteTrackerSync(id int64) error {
	now := time.Now().Unix()
	_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='COMPLETED',completed_at=?,error_message=NULL WHERE id=?`, now, id)
	return err
}

func (r *Repository) FailTrackerSync(id int64, retry bool, message string) error {
	if !retry {
		_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='FAILED',attempts=attempts+1,error_message=? WHERE id=?`, message, id)
		return err
	}
	var attempts int
	if err := r.db.Get(&attempts, `SELECT attempts FROM tracker_sync_jobs WHERE id=?`, id); err != nil {
		return err
	}
	attempts++
	delay := int64(1 << min(attempts, 5))
	next := time.Now().Unix() + delay
	_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='PENDING',attempts=?,next_attempt_at=?,error_message=? WHERE id=?`, attempts, next, message, id)
	return err
}

func (r *Repository) ResetInterruptedTrackerSync() error {
	_, err := r.db.Exec(`UPDATE tracker_sync_jobs SET status='PENDING',next_attempt_at=? WHERE status='RUNNING'`, time.Now().Unix())
	return err
}

func (r *Repository) ListTrackerSyncJobs() ([]TrackerSyncJob, error) {
	var jobs []TrackerSyncJob
	err := r.db.Select(&jobs, `SELECT id,manga_id,binding_id,chapter_number,status,attempts,next_attempt_at,error_message,created_at,completed_at FROM tracker_sync_jobs ORDER BY created_at DESC,id DESC`)
	return jobs, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (r *Repository) EnqueueChapter(chapterID string) (DownloadQueueItem, error) {
	now := time.Now().Unix()
	_, err := r.db.Exec(`INSERT INTO download_queue(
		chapter_id, status, progress, total_pages, downloaded_pages,
		error_message, queued_at
	) VALUES(?, ?, 0, 0, 0, NULL, ?)
	ON CONFLICT(chapter_id) DO UPDATE SET
		status = CASE
			WHEN download_queue.status IN (?, ?) THEN excluded.status
			ELSE download_queue.status
		END,
		progress = CASE
			WHEN download_queue.status IN (?, ?) THEN 0
			ELSE download_queue.progress
		END,
		total_pages = CASE
			WHEN download_queue.status IN (?, ?) THEN 0
			ELSE download_queue.total_pages
		END,
		downloaded_pages = CASE
			WHEN download_queue.status IN (?, ?) THEN 0
			ELSE download_queue.downloaded_pages
		END,
		error_message = CASE
			WHEN download_queue.status IN (?, ?) THEN NULL
			ELSE download_queue.error_message
		END,
		queued_at = CASE
			WHEN download_queue.status IN (?, ?) THEN excluded.queued_at
			ELSE download_queue.queued_at
		END`,
		chapterID, QueuePending, now,
		QueueFailed, QueueCanceled, QueueFailed, QueueCanceled,
		QueueFailed, QueueCanceled, QueueFailed, QueueCanceled,
		QueueFailed, QueueCanceled, QueueFailed, QueueCanceled)
	if err != nil {
		return DownloadQueueItem{}, fmt.Errorf("enqueue chapter %s: %w", chapterID, err)
	}
	return r.GetQueueItemByChapter(chapterID)
}

func (r *Repository) UpdateQueueProgress(id int64, totalPages, downloadedPages int, queueErr error) error {
	if totalPages < 0 || downloadedPages < 0 || downloadedPages > totalPages {
		return errors.New("invalid queue progress")
	}
	progress := 0
	if totalPages > 0 {
		progress = downloadedPages * 100 / totalPages
	}
	var message *string
	if queueErr != nil {
		value := queueErr.Error()
		message = &value
	}
	result, err := r.db.Exec(`UPDATE download_queue SET
		status = CASE WHEN status = ? THEN ? ELSE status END,
		progress = ?, total_pages = ?, downloaded_pages = ?, error_message = ?
		WHERE id = ? AND status IN (?, ?)`, QueuePending, QueueDownloading,
		progress, totalPages, downloadedPages, message, id,
		QueuePending, QueueDownloading)
	if err != nil {
		return err
	}
	return requireChange(result, "update queue progress")
}

func (r *Repository) MarkQueueFailed(id int64, queueErr error) error {
	message := "download failed"
	if queueErr != nil {
		message = queueErr.Error()
	}
	result, err := r.db.Exec(`UPDATE download_queue SET status = ?, error_message = ?
		WHERE id = ? AND status = ?`, QueueFailed, message, id, QueueDownloading)
	if err != nil {
		return err
	}
	return requireChange(result, "mark queue item failed")
}

func (r *Repository) MarkChapterDownloaded(chapterID, path string) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`UPDATE chapters SET downloaded = 1, download_path = ? WHERE id = ?`, path, chapterID)
	if err != nil {
		return err
	}
	if err := requireChange(result, "mark chapter downloaded"); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE download_queue SET status = ?, progress = 100,
		downloaded_pages = total_pages, error_message = NULL WHERE chapter_id = ?`,
		QueueCompleted, chapterID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) PauseQueueItem(id int64) error {
	return r.transitionQueueItem(id, QueuePaused, QueuePending, QueueDownloading)
}

func (r *Repository) ResumeQueueItem(id int64) error {
	return r.transitionQueueItem(id, QueuePending, QueuePaused)
}

func (r *Repository) CancelQueueItem(id int64) error {
	return r.transitionQueueItem(id, QueueCanceled,
		QueuePending, QueueDownloading, QueuePaused, QueueFailed)
}

func (r *Repository) transitionQueueItem(id int64, target string, allowed ...string) error {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(allowed)), ",")
	args := make([]any, 0, len(allowed)+2)
	args = append(args, target, id)
	for _, status := range allowed {
		args = append(args, status)
	}
	result, err := r.db.Exec(`UPDATE download_queue SET status = ? WHERE id = ? AND status IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	return requireChange(result, "change queue item status")
}

func (r *Repository) ResetInterruptedQueue() error {
	_, err := r.db.Exec(`UPDATE download_queue SET status = ? WHERE status = ?`, QueuePending, QueueDownloading)
	return err
}

func (r *Repository) ClaimNextQueueItem() (*DownloadQueueItem, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	if err := tx.Get(&id, `SELECT id FROM download_queue WHERE status = ? ORDER BY queued_at, id LIMIT 1`, QueuePending); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	result, err := tx.Exec(`UPDATE download_queue SET status = ?, error_message = NULL
		WHERE id = ? AND status = ?`, QueueDownloading, id, QueuePending)
	if err != nil {
		return nil, err
	}
	if err := requireChange(result, "claim queue item"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	item, err := r.GetQueueItem(id)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetQueueItem(id int64) (DownloadQueueItem, error) {
	var item DownloadQueueItem
	err := r.db.Get(&item, queueSelect+` WHERE q.id = ?`, id)
	return item, err
}

func (r *Repository) GetQueueItemByChapter(chapterID string) (DownloadQueueItem, error) {
	var item DownloadQueueItem
	err := r.db.Get(&item, queueSelect+` WHERE q.chapter_id = ?`, chapterID)
	return item, err
}

func (r *Repository) ListQueue() ([]DownloadQueueItem, error) {
	var items []DownloadQueueItem
	err := r.db.Select(&items, queueSelect+` ORDER BY q.queued_at, q.id`)
	return items, err
}

const queueSelect = `SELECT
	q.id, q.chapter_id, q.status, q.progress, q.total_pages,
	q.downloaded_pages, q.error_message, q.queued_at,
	c.manga_id, c.source_chapter_id, c.chapter_number,
	c.title AS chapter_title, c.language, c.scanlator,
	m.source_id, m.source_manga_id, m.title AS manga_title,
	m.description AS manga_description, m.authors AS manga_authors,
	m.artists AS manga_artists, m.genres AS manga_genres, m.download_format,
	s.name AS source_name
	FROM download_queue q
	JOIN chapters c ON c.id = q.chapter_id
	JOIN manga m ON m.id = c.manga_id
	JOIN sources s ON s.id = m.source_id`

func requireChange(result sql.Result, action string) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%s: item was not in an allowed state", action)
	}
	return nil
}
