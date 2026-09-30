package db

import (
	"encoding/json"
	"strings"
)

// Queue statuses. A stored row is PENDING, DOWNLOADING, PAUSED or FAILED;
// COMPLETED and CANCELED mark a row that has just left the queue and travel
// only in downloader events.
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
	// Hidden categories stay assigned to their titles but are not offered as
	// a library filter.
	Hidden bool `db:"hidden" json:"hidden"`
}

// Manga is a stored library entry. SourceMangaID and SourcePageURL are
// transient inputs that name the source-side entry and its series page when
// the title is linked through manga_sources; neither is a column on the manga
// row.
type Manga struct {
	ID                  string  `db:"id" json:"id"`
	SourceID            string  `db:"source_id" json:"sourceId"`
	SourceMangaID       string  `db:"-" json:"-"`
	SourcePageURL       string  `db:"-" json:"-"`
	Title               string  `db:"title" json:"title"`
	AltTitles           *string `db:"alt_titles" json:"altTitles"`
	Description         *string `db:"description" json:"description"`
	Authors             *string `db:"authors" json:"authors"`
	Artists             *string `db:"artists" json:"artists"`
	Genres              *string `db:"genres" json:"genres"`
	Tags                *string `db:"tags" json:"tags,omitempty"`
	Status              string  `db:"status" json:"status"`
	CoverURL            string  `db:"cover_url" json:"-"`
	CoverCachePath      *string `db:"cover_cache_path" json:"-"`
	CoverContentType    *string `db:"cover_content_type" json:"-"`
	CoverFetchedAt      *int64  `db:"cover_fetched_at" json:"-"`
	InLibrary           bool    `db:"in_library" json:"inLibrary"`
	DownloadFormat      string  `db:"download_format" json:"downloadFormat"`
	DownloadNewChapters bool    `db:"download_new_chapters" json:"downloadNewChapters"`
	// Reader overrides win over the global reader settings for this title. A
	// NULL column means the global setting applies.
	ReaderMode      *string `db:"reader_mode" json:"readerMode,omitempty"`
	ReaderDirection *string `db:"reader_direction" json:"readerDirection,omitempty"`
	ReaderFit       *string `db:"reader_fit" json:"readerFit,omitempty"`
	// Chapter list presentation persists per title. An empty column means
	// unset, and the client falls back to its defaults.
	ChapterSort     string `db:"chapter_sort" json:"chapterSort,omitempty"`
	ChapterFilter   string `db:"chapter_filter" json:"chapterFilter,omitempty"`
	ChapterLanguage string `db:"chapter_language" json:"chapterLanguage,omitempty"`
	CreatedAt       int64  `db:"created_at" json:"createdAt"`
	UpdatedAt       int64  `db:"updated_at" json:"updatedAt"`
	// DetailsFetchedAt records when full details were last pulled from the
	// plugin. Search-level records stay NULL until the first details read.
	DetailsFetchedAt *int64 `db:"details_fetched_at" json:"detailsFetchedAt,omitempty"`
	// The custom fields carry user overrides restored from a library backup.
	// When a custom value is set it wins over the source value at render time;
	// the source columns stay untouched so a refresh keeps working.
	CustomTitle        *string `db:"custom_title" json:"customTitle,omitempty"`
	CustomArtist       *string `db:"custom_artist" json:"customArtist,omitempty"`
	CustomAuthor       *string `db:"custom_author" json:"customAuthor,omitempty"`
	CustomDescription  *string `db:"custom_description" json:"customDescription,omitempty"`
	CustomGenres       *string `db:"custom_genres" json:"customGenres,omitempty"`
	CustomStatus       *string `db:"custom_status" json:"customStatus,omitempty"`
	CustomCoverURL     *string `db:"custom_cover_url" json:"-"`
	Notes              *string `db:"notes" json:"notes,omitempty"`
	Memo               *string `db:"memo" json:"memo,omitempty"`
	SourceVersion      *int64  `db:"source_version" json:"sourceVersion,omitempty"`
	UpdateStrategy     *string `db:"update_strategy" json:"updateStrategy,omitempty"`
	FavoriteModifiedAt *int64  `db:"favorite_modified_at" json:"favoriteModifiedAt,omitempty"`
	Initialized        bool    `db:"initialized" json:"initialized"`
	ExcludedScanlators *string `db:"excluded_scanlators" json:"excludedScanlators,omitempty"`
	ChapterFlags       *int64  `db:"chapter_flags" json:"chapterFlags,omitempty"`
}

// MarshalJSON exposes a backend-owned cover route instead of the source URL.
func (m Manga) MarshalJSON() ([]byte, error) {
	type alias Manga
	return json.Marshal(struct {
		alias
		CoverURL string `json:"coverUrl"`
		MangaDisplay
	}{alias: alias(m), CoverURL: "/api/manga/" + m.ID + "/cover", MangaDisplay: m.display()})
}

// MangaDisplay carries the values a client renders. A custom override set by
// the user wins over the source value; the source columns stay untouched so a
// refresh keeps working.
type MangaDisplay struct {
	DisplayTitle       string  `json:"displayTitle"`
	DisplayDescription *string `json:"displayDescription,omitempty"`
	DisplayAuthors     *string `json:"displayAuthors,omitempty"`
	DisplayArtists     *string `json:"displayArtists,omitempty"`
	DisplayGenres      *string `json:"displayGenres,omitempty"`
	DisplayStatus      string  `json:"displayStatus"`
	DisplayCoverURL    string  `json:"displayCoverUrl"`
}

func (m Manga) display() MangaDisplay {
	out := MangaDisplay{
		DisplayTitle:       m.Title,
		DisplayDescription: m.Description,
		DisplayAuthors:     m.Authors,
		DisplayArtists:     m.Artists,
		DisplayGenres:      m.Genres,
		DisplayStatus:      m.Status,
		DisplayCoverURL:    "/api/manga/" + m.ID + "/cover",
	}
	if custom := trimmed(m.CustomTitle); custom != "" {
		out.DisplayTitle = custom
	}
	out.DisplayDescription = prefer(m.CustomDescription, out.DisplayDescription)
	out.DisplayAuthors = prefer(m.CustomAuthor, out.DisplayAuthors)
	out.DisplayArtists = prefer(m.CustomArtist, out.DisplayArtists)
	out.DisplayGenres = prefer(m.CustomGenres, out.DisplayGenres)
	if custom := trimmed(m.CustomStatus); custom != "" {
		out.DisplayStatus = custom
	}
	return out
}

func trimmed(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func prefer(custom *string, fallback *string) *string {
	if trimmed(custom) != "" {
		return custom
	}
	return fallback
}

// LibraryManga is a library title with its user-facing reading metadata.
// SourceName is resolved by the API layer from the installed plugin registry.
type LibraryManga struct {
	Manga
	Categories         []Category       `json:"categories"`
	Progress           *ReadingProgress `json:"progress,omitempty"`
	UnreadChapters     int              `json:"unreadChapters"`
	DownloadedChapters int              `json:"downloadedChapters"`
	TotalChapters      int              `json:"totalChapters"`
	BookmarkedChapters int              `json:"bookmarkedChapters"`
	Languages          []string         `json:"languages"`
	SourceName         string           `json:"sourceName,omitempty"`
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
		Categories         []Category       `json:"categories"`
		Progress           *ReadingProgress `json:"progress,omitempty"`
		UnreadChapters     int              `json:"unreadChapters"`
		DownloadedChapters int              `json:"downloadedChapters"`
		TotalChapters      int              `json:"totalChapters"`
		BookmarkedChapters int              `json:"bookmarkedChapters"`
		Languages          []string         `json:"languages"`
		SourceName         string           `json:"sourceName,omitempty"`
		CoverURL           string           `json:"coverUrl"`
		MangaDisplay
	}{
		mangaAlias:         mangaAlias(l.Manga),
		Categories:         categories,
		Progress:           l.Progress,
		UnreadChapters:     l.UnreadChapters,
		DownloadedChapters: l.DownloadedChapters,
		TotalChapters:      l.TotalChapters,
		BookmarkedChapters: l.BookmarkedChapters,
		Languages:          l.Languages,
		SourceName:         l.SourceName,
		CoverURL:           "/api/manga/" + l.Manga.ID + "/cover",
		MangaDisplay:       l.Manga.display(),
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
	// RefreshError carries a failed on-demand details refresh to the client
	// while the stored aggregate is still served.
	RefreshError string `json:"refreshError,omitempty"`
}

// MangaSource is an internal adapter record. It is never returned directly to
// the web client because SourceMangaID and PluginKey are source-site details.
type MangaSource struct {
	MangaID       string `db:"manga_id"`
	SourceID      string `db:"source_id"`
	SourceMangaID string `db:"source_manga_id"`
	PluginKey     string `db:"plugin_key"`
	// URL is the series page this link declared, empty until a listing that
	// carries one has been seen.
	URL string `db:"url"`
}

// Feed is a source browse shortcut. A global feed opens the source itself; a
// feed that carries a saved search opens the source with that search applied.
type Feed struct {
	ID        string `db:"id" json:"id"`
	SourceID  string `db:"source_id" json:"sourceId"`
	IsGlobal  bool   `db:"is_global" json:"global"`
	FeedOrder int64  `db:"feed_order" json:"feedOrder"`
}

// SavedSearch is one stored source search: a query plus the source's own
// filter values, kept in the source's encoding so it can be replayed.
type SavedSearch struct {
	ID          string  `db:"id" json:"id"`
	SourceID    string  `db:"source_id" json:"sourceId"`
	FeedID      *string `db:"feed_id" json:"feedId,omitempty"`
	Name        string  `db:"name" json:"name"`
	Query       string  `db:"query" json:"query"`
	Filters     string  `db:"filters" json:"filters"`
	SearchOrder int64   `db:"search_order" json:"searchOrder"`
}

// MangaMerge is one additional source merged into a title. The role flags
// describe how the merged source participates; the link itself is also
// recorded in manga_sources so the refresh path can pull its chapters.
type MangaMerge struct {
	ID                string  `db:"id" json:"id"`
	MangaID           string  `db:"manga_id" json:"mangaId"`
	SourceID          string  `db:"source_id" json:"sourceId"`
	SourceMangaID     string  `db:"source_manga_id" json:"sourceMangaId"`
	URL               *string `db:"url" json:"url,omitempty"`
	IsInfoManga       bool    `db:"is_info_manga" json:"isInfoManga"`
	GetChapterUpdates bool    `db:"get_chapter_updates" json:"getChapterUpdates"`
	ChapterSortMode   int64   `db:"chapter_sort_mode" json:"chapterSortMode"`
	ChapterPriority   int64   `db:"chapter_priority" json:"chapterPriority"`
	DownloadChapters  bool    `db:"download_chapters" json:"downloadChapters"`
	MergeOrder        int64   `db:"merge_order" json:"mergeOrder"`
}

// MangaTitle is one alternative title of a title, with the type the source
// assigned to it.
type MangaTitle struct {
	ID        string `db:"id" json:"id"`
	MangaID   string `db:"manga_id" json:"mangaId"`
	Title     string `db:"title" json:"title"`
	TitleType int64  `db:"title_type" json:"titleType"`
}

// MangaTag is one tag of a title, optionally namespaced.
type MangaTag struct {
	ID        string  `db:"id" json:"id"`
	MangaID   string  `db:"manga_id" json:"mangaId"`
	Namespace *string `db:"namespace" json:"namespace,omitempty"`
	Name      string  `db:"name" json:"name"`
	TagType   int64   `db:"tag_type" json:"tagType"`
}

// MangaMetadata is the free-form metadata record a source can attach to a
// title. Extra is stored verbatim because only the source understands it.
type MangaMetadata struct {
	MangaID      string  `db:"manga_id" json:"mangaId"`
	Uploader     *string `db:"uploader" json:"uploader,omitempty"`
	Extra        string  `db:"extra" json:"extra"`
	IndexedExtra *string `db:"indexed_extra" json:"indexedExtra,omitempty"`
	ExtraVersion int64   `db:"extra_version" json:"extraVersion"`
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
	Locked          bool     `db:"locked" json:"locked"`
	Downloaded      bool     `db:"downloaded" json:"downloaded"`
	DownloadPath    *string  `db:"download_path" json:"downloadPath"`
	// Bookmark is the user-facing flag. The remaining source fields mirror the
	// source's own record and are kept for round-tripping and merge decisions.
	Bookmark             bool    `db:"bookmark" json:"bookmark"`
	SourceOrder          *int64  `db:"source_order" json:"sourceOrder,omitempty"`
	SourceVersion        *int64  `db:"source_version" json:"sourceVersion,omitempty"`
	SourceLastModifiedAt *int64  `db:"source_last_modified_at" json:"sourceLastModifiedAt,omitempty"`
	SourceFetchedAt      *int64  `db:"source_fetched_at" json:"sourceFetchedAt,omitempty"`
	Memo                 *string `db:"memo" json:"memo,omitempty"`
	Read                 bool    `db:"-" json:"read"`
	DownloadStatus       string  `db:"download_status" json:"downloadStatus,omitempty"`
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
	ReadingSeconds int64         `json:"readingSeconds"`
	TitleCount     int           `json:"titleCount"`
	ChapterCount   int           `json:"chapterCount"`
	Daily          []ReadingDay  `json:"daily"`
	Overview       StatsOverview `json:"overview"`
	Titles         StatsTitles   `json:"titles"`
	Chapters       StatsChapters `json:"chapters"`
	Trackers       StatsTrackers `json:"trackers"`
	TopTitles      []TopTitle    `json:"topTitles"`
}

// StatsOverview summarizes library-wide reading state.
type StatsOverview struct {
	LibraryMangaCount   int   `json:"libraryMangaCount"`
	CompletedMangaCount int   `json:"completedMangaCount"`
	TotalReadDuration   int64 `json:"totalReadDuration"`
}

// StatsTitles summarizes how much of the library is being tracked or updated.
type StatsTitles struct {
	UpdateEnabledCount int `json:"updateEnabledCount"`
	StartedMangaCount  int `json:"startedMangaCount"`
}

// StatsChapters summarizes chapter consumption and downloads.
type StatsChapters struct {
	TotalChapterCount int `json:"totalChapterCount"`
	ReadChapterCount  int `json:"readChapterCount"`
	DownloadCount     int `json:"downloadCount"`
}

// StatsTrackers summarizes remote tracker bindings.
type StatsTrackers struct {
	TrackedTitleCount int     `json:"trackedTitleCount"`
	MeanScore         float64 `json:"meanScore"`
	TrackerCount      int     `json:"trackerCount"`
}

// TopTitle is one entry of the per-title reading breakdown.
type TopTitle struct {
	MangaID      string `db:"manga_id" json:"mangaId"`
	Title        string `db:"title" json:"title"`
	Seconds      int64  `db:"seconds" json:"seconds"`
	ChaptersRead int    `db:"chapters_read" json:"chaptersRead"`
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
	// RemoteURL, RemoteLibraryID and IsPrivate mirror the binding's remote
	// record. They are informational; only the remote identifier is used to
	// address the entry.
	RemoteURL       *string `db:"remote_url" json:"remoteUrl,omitempty"`
	RemoteLibraryID *string `db:"remote_library_id" json:"remoteLibraryId,omitempty"`
	IsPrivate       bool    `db:"is_private" json:"isPrivate"`
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
	// Position is the user-visible queue order; the worker claims the lowest
	// position first.
	Position int64 `db:"position" json:"position"`
}

// DownloadQueueItem includes the source, manga and chapter data needed by a
// worker so claiming an item does not require a series of follow-up queries.
type DownloadQueueItem struct {
	DownloadQueue
	MangaID          string   `db:"manga_id" json:"mangaId"`
	SourceID         string   `db:"source_id" json:"sourceId"`
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
	UploadedAt       *int64   `db:"uploaded_at" json:"uploadedAt,omitempty"`
	Scanlator        *string  `db:"scanlator" json:"scanlator,omitempty"`
	// DonePagesJSON holds the JSON-encoded page indexes already persisted in
	// a previous pass so an interrupted download resumes instead of
	// restarting. It is worker bookkeeping and never leaves the backend.
	DonePagesJSON string `db:"done_pages" json:"-"`
}
