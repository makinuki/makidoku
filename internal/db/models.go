package db

import "encoding/json"

const (
	QueuePending     = "PENDING"
	QueueDownloading = "DOWNLOADING"
	QueueCompleted   = "COMPLETED"
	QueueFailed      = "FAILED"
	QueuePaused      = "PAUSED"
	QueueCanceled    = "CANCELED"
)

// Models mirror the DDL in migrations/000001_init.up.sql. They are plain
// structs for sqlx mapping; JSON tags follow the REST API shape.

type Source struct {
	ID          string  `db:"id" json:"id"`
	PluginKey   string  `db:"plugin_key" json:"-"`
	Name        string  `db:"name" json:"name"`
	Version     string  `db:"version" json:"version"`
	ABIVersion  int     `db:"abi_version" json:"abiVersion"`
	Lang        string  `db:"lang" json:"lang"`
	BaseURL     string  `db:"base_url" json:"baseUrl"`
	IconURL     *string `db:"icon_url" json:"iconUrl"`
	WasmPath    string  `db:"wasm_path" json:"wasmPath"`
	InstalledAt int64   `db:"installed_at" json:"installedAt"`
}

type PluginStorage struct {
	SourceID  string `db:"source_id" json:"sourceId"`
	Key       string `db:"key" json:"key"`
	Value     string `db:"value" json:"value"`
	UpdatedAt int64  `db:"updated_at" json:"updatedAt"`
}

type Category struct {
	ID        int64  `db:"id" json:"id"`
	Name      string `db:"name" json:"name"`
	SortOrder int    `db:"sort_order" json:"sortOrder"`
}

type Manga struct {
	ID                  string  `db:"id" json:"id"`
	SourceID            string  `db:"source_id" json:"sourceId"`
	SourceMangaID       string  `db:"source_manga_id" json:"-"`
	Title               string  `db:"title" json:"title"`
	AltTitles           *string `db:"alt_titles" json:"altTitles"`
	Description         *string `db:"description" json:"description"`
	Authors             *string `db:"authors" json:"authors"`
	Artists             *string `db:"artists" json:"artists"`
	Genres              *string `db:"genres" json:"genres"`
	Status              string  `db:"status" json:"status"`
	CoverURL            string  `db:"cover_url" json:"-"`
	CoverCachePath      *string `db:"cover_cache_path" json:"-"`
	CoverContentType    *string `db:"cover_content_type" json:"-"`
	CoverFetchedAt      *int64  `db:"cover_fetched_at" json:"-"`
	InLibrary           bool    `db:"in_library" json:"inLibrary"`
	DownloadFormat      string  `db:"download_format" json:"downloadFormat"`
	DownloadNewChapters bool    `db:"download_new_chapters" json:"downloadNewChapters"`
	CreatedAt           int64   `db:"created_at" json:"createdAt"`
	UpdatedAt           int64   `db:"updated_at" json:"updatedAt"`
	// DetailsFetchedAt records when full details were last pulled from the
	// plugin. Search-level records stay NULL until the first details read.
	DetailsFetchedAt *int64 `db:"details_fetched_at" json:"detailsFetchedAt,omitempty"`
}

// MarshalJSON exposes a backend-owned cover route instead of the source URL.
func (m Manga) MarshalJSON() ([]byte, error) {
	type alias Manga
	return json.Marshal(struct {
		alias
		CoverURL string `json:"coverUrl"`
	}{alias: alias(m), CoverURL: "/api/manga/" + m.ID + "/cover"})
}

// LibraryManga is a library title with its user-facing reading metadata.
// SourceName is resolved by the API layer from the installed plugin registry.
type LibraryManga struct {
	Manga
	Categories     []Category       `json:"categories"`
	Progress       *ReadingProgress `json:"progress,omitempty"`
	UnreadChapters int              `json:"unreadChapters"`
	SourceName     string           `json:"sourceName,omitempty"`
}

// MarshalJSON flattens the embedded manga, keeps the backend cover route, and
// preserves the metadata fields that the promoted Manga.MarshalJSON drops.
func (l LibraryManga) MarshalJSON() ([]byte, error) {
	type mangaAlias Manga
	categories := l.Categories
	if categories == nil {
		categories = []Category{}
	}
	return json.Marshal(struct {
		mangaAlias
		Categories     []Category       `json:"categories"`
		Progress       *ReadingProgress `json:"progress,omitempty"`
		UnreadChapters int              `json:"unreadChapters"`
		SourceName     string           `json:"sourceName,omitempty"`
		CoverURL       string           `json:"coverUrl"`
	}{
		mangaAlias:     mangaAlias(l.Manga),
		Categories:     categories,
		Progress:       l.Progress,
		UnreadChapters: l.UnreadChapters,
		SourceName:     l.SourceName,
		CoverURL:       "/api/manga/" + l.Manga.ID + "/cover",
	})
}

// MangaAggregate contains all local state needed by the details and reader
// views. The aggregate is assembled from normalized tables by the repository.
// SourceName is resolved by the API layer from the installed plugin registry.
type MangaAggregate struct {
	Manga          Manga            `json:"manga"`
	Categories     []Category       `json:"categories"`
	Chapters       []Chapter        `json:"chapters"`
	Progress       *ReadingProgress `json:"progress,omitempty"`
	Trackers       []TrackerBinding `json:"trackers"`
	ReadingSeconds int64            `json:"readingSeconds"`
	SourceName     string           `json:"sourceName,omitempty"`
	SourceURL      string           `json:"sourceUrl,omitempty"`
}

// MangaSource is an internal adapter record. It is never returned directly to
// the web client because SourceMangaID and PluginKey are source-site details.
type MangaSource struct {
	MangaID       string `db:"manga_id"`
	SourceID      string `db:"source_id"`
	SourceMangaID string `db:"source_manga_id"`
	PluginKey     string `db:"plugin_key"`
}

type HistoryItem struct {
	Manga    Manga           `json:"manga"`
	Chapter  Chapter         `json:"chapter"`
	Progress ReadingProgress `json:"progress"`
}

type Chapter struct {
	ID              string   `db:"id" json:"id"`
	MangaID         string   `db:"manga_id" json:"mangaId"`
	SourceID        string   `db:"source_id" json:"sourceId"`
	SourceChapterID string   `db:"source_chapter_id" json:"-"`
	ChapterNumber   *float64 `db:"chapter_number" json:"chapterNumber"`
	Volume          *int64   `db:"volume" json:"volume,omitempty"`
	Title           *string  `db:"title" json:"title"`
	Language        *string  `db:"language" json:"language"`
	UploadedAt      *int64   `db:"uploaded_at" json:"uploadedAt"`
	Scanlator       *string  `db:"scanlator" json:"scanlator"`
	Downloaded      bool     `db:"downloaded" json:"downloaded"`
	DownloadPath    *string  `db:"download_path" json:"downloadPath"`
	Read            bool     `db:"-" json:"read"`
	DownloadStatus  string   `db:"download_status" json:"downloadStatus,omitempty"`
}

// Page is the backend-owned reader contract. The source URL and request
// headers are intentionally private and are never serialized to the web API.
type Page struct {
	ID          string  `db:"id" json:"id"`
	ChapterID   string  `db:"chapter_id" json:"chapterId"`
	PageIndex   int     `db:"page_index" json:"index"`
	RemoteURL   string  `db:"remote_url" json:"-"`
	HeadersJSON *string `db:"request_headers" json:"-"`
	IsScrambled bool    `db:"is_scrambled" json:"isScrambled"`
}

// PageCache records where the processed image bytes of a page live on disk.
// The byte path and cache key are backend-owned details.
type PageCache struct {
	PageID      string `db:"page_id" json:"-"`
	CacheKey    string `db:"cache_key" json:"-"`
	BytePath    string `db:"byte_path" json:"-"`
	ContentType string `db:"content_type" json:"-"`
	ByteSize    int64  `db:"byte_size" json:"-"`
	FetchedAt   int64  `db:"fetched_at" json:"-"`
}

type ReadingProgress struct {
	MangaID           string `db:"manga_id" json:"mangaId"`
	LastReadChapterID string `db:"last_read_chapter_id" json:"lastReadChapterId"`
	LastReadPage      int    `db:"last_read_page" json:"lastReadPage"`
	TotalPages        int    `db:"total_pages" json:"totalPages"`
	IsCompleted       bool   `db:"is_completed" json:"isCompleted"`
	LastReadAt        int64  `db:"last_read_at" json:"lastReadAt"`
	SessionSeconds    int64  `db:"-" json:"sessionSeconds,omitempty"`
}

type Setting struct {
	Key   string `db:"key" json:"key"`
	Value string `db:"value" json:"value"`
}

type ChapterReadState struct {
	ChapterID string `db:"chapter_id" json:"chapterId"`
	MangaID   string `db:"manga_id" json:"mangaId"`
	Read      bool   `db:"read" json:"read"`
	ReadAt    *int64 `db:"read_at" json:"readAt,omitempty"`
}

type HistoryEvent struct {
	ID         string  `db:"id" json:"id"`
	MangaID    string  `db:"manga_id" json:"mangaId"`
	ChapterID  *string `db:"chapter_id" json:"chapterId,omitempty"`
	Page       *int    `db:"page" json:"page,omitempty"`
	OccurredAt int64   `db:"occurred_at" json:"occurredAt"`
}

type UpdateLog struct {
	ID           string `db:"id" json:"id"`
	MangaID      string `db:"manga_id" json:"mangaId"`
	ChapterID    string `db:"chapter_id" json:"chapterId"`
	SeenAt       int64  `db:"seen_at" json:"seenAt"`
	Acknowledged bool   `db:"acknowledged" json:"acknowledged"`
}

type LibraryUpdateState struct {
	LastRunAt  *int64 `db:"last_run_at" json:"lastRunAt,omitempty"`
	LastStatus string `db:"last_status" json:"lastStatus"`
}

type ReadingSession struct {
	ID         string `db:"id" json:"id"`
	MangaID    string `db:"manga_id" json:"mangaId"`
	Seconds    int64  `db:"seconds" json:"seconds"`
	OccurredAt int64  `db:"occurred_at" json:"occurredAt"`
}

type ReadingDay struct {
	Date    string `db:"date" json:"date"`
	Seconds int64  `db:"seconds" json:"seconds"`
}

type ReadingStats struct {
	ReadingSeconds int64        `json:"readingSeconds"`
	TitleCount     int          `json:"titleCount"`
	ChapterCount   int          `json:"chapterCount"`
	Daily          []ReadingDay `json:"daily"`
}

type TrackerBinding struct {
	ID                  int64    `db:"id" json:"id"`
	MangaID             string   `db:"manga_id" json:"mangaId"`
	TrackerType         string   `db:"tracker_type" json:"trackerType"`
	RemoteID            string   `db:"remote_id" json:"remoteId"`
	RemoteTitle         string   `db:"remote_title" json:"remoteTitle"`
	RemoteScore         *float64 `db:"remote_score" json:"remoteScore"`
	RemoteStatus        *string  `db:"remote_status" json:"remoteStatus"`
	LastSyncedChapter   float64  `db:"last_synced_chapter" json:"lastSyncedChapter"`
	TotalRemoteChapters *int     `db:"total_remote_chapters" json:"totalRemoteChapters"`
	StartedAt           *int64   `db:"started_at" json:"startedAt,omitempty"`
	FinishedAt          *int64   `db:"finished_at" json:"finishedAt,omitempty"`
}

type TrackerCredential struct {
	TrackerType string `db:"tracker_type" json:"trackerType"`
	ExpiresAt   *int64 `db:"expires_at" json:"expiresAt,omitempty"`
	CreatedAt   int64  `db:"created_at" json:"createdAt"`
	UpdatedAt   int64  `db:"updated_at" json:"updatedAt"`
}

type TrackerCredentialRecord struct {
	TrackerType  string `db:"tracker_type"`
	AccessToken  []byte `db:"access_token"`
	RefreshToken []byte `db:"refresh_token"`
	ExpiresAt    *int64 `db:"expires_at"`
	Metadata     []byte `db:"metadata"`
}

const (
	SyncPending = "PENDING"
	SyncRunning = "RUNNING"
	SyncFailed  = "FAILED"
	SyncDone    = "COMPLETED"
)

type TrackerSyncJob struct {
	ID            int64   `db:"id" json:"id"`
	MangaID       string  `db:"manga_id" json:"mangaId"`
	BindingID     int64   `db:"binding_id" json:"bindingId"`
	ChapterNumber float64 `db:"chapter_number" json:"chapterNumber"`
	Status        string  `db:"status" json:"status"`
	Attempts      int     `db:"attempts" json:"attempts"`
	NextAttemptAt int64   `db:"next_attempt_at" json:"nextAttemptAt"`
	ErrorMessage  *string `db:"error_message" json:"errorMessage,omitempty"`
	CreatedAt     int64   `db:"created_at" json:"createdAt"`
	CompletedAt   *int64  `db:"completed_at" json:"completedAt,omitempty"`
}

type DownloadQueue struct {
	ID              int64   `db:"id" json:"id"`
	ChapterID       string  `db:"chapter_id" json:"chapterId"`
	Status          string  `db:"status" json:"status"`
	Progress        int     `db:"progress" json:"progress"`
	TotalPages      int     `db:"total_pages" json:"totalPages"`
	DownloadedPages int     `db:"downloaded_pages" json:"downloadedPages"`
	ErrorMessage    *string `db:"error_message" json:"errorMessage"`
	QueuedAt        int64   `db:"queued_at" json:"queuedAt"`
}

// DownloadQueueItem includes the source, manga and chapter data needed by a
// worker so claiming an item does not require a series of follow-up queries.
type DownloadQueueItem struct {
	DownloadQueue
	MangaID          string   `db:"manga_id" json:"mangaId"`
	SourceID         string   `db:"source_id" json:"sourceId"`
	SourceMangaID    string   `db:"source_manga_id" json:"-"`
	MangaTitle       string   `db:"manga_title" json:"mangaTitle"`
	MangaDescription *string  `db:"manga_description" json:"mangaDescription,omitempty"`
	MangaAuthors     *string  `db:"manga_authors" json:"mangaAuthors,omitempty"`
	MangaArtists     *string  `db:"manga_artists" json:"mangaArtists,omitempty"`
	MangaGenres      *string  `db:"manga_genres" json:"mangaGenres,omitempty"`
	DownloadFormat   string   `db:"download_format" json:"downloadFormat"`
	SourceName       string   `db:"source_name" json:"sourceName"`
	SourceChapterID  string   `db:"source_chapter_id" json:"-"`
	ChapterNumber    *float64 `db:"chapter_number" json:"chapterNumber,omitempty"`
	Volume           *int64   `db:"volume" json:"volume,omitempty"`
	ChapterTitle     *string  `db:"chapter_title" json:"chapterTitle,omitempty"`
	Language         *string  `db:"language" json:"language,omitempty"`
	Scanlator        *string  `db:"scanlator" json:"scanlator,omitempty"`
	// DonePagesJSON holds the JSON-encoded page indexes already persisted in
	// a previous pass so an interrupted download resumes instead of
	// restarting. It is worker bookkeeping and never leaves the backend.
	DonePagesJSON string `db:"done_pages" json:"-"`
}
