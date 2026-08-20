package db

// Models mirror the DDL in migrations/000001_init.up.sql. They are plain
// structs for sqlx mapping; JSON tags follow the REST API shape.

type Source struct {
	ID          string  `db:"id" json:"id"`
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
	ID             string  `db:"id" json:"id"`
	SourceID       string  `db:"source_id" json:"sourceId"`
	SourceMangaID  string  `db:"source_manga_id" json:"sourceMangaId"`
	Title          string  `db:"title" json:"title"`
	AltTitles      *string `db:"alt_titles" json:"altTitles"`
	Description    *string `db:"description" json:"description"`
	Authors        *string `db:"authors" json:"authors"`
	Artists        *string `db:"artists" json:"artists"`
	Genres         *string `db:"genres" json:"genres"`
	Status         string  `db:"status" json:"status"`
	CoverURL       string  `db:"cover_url" json:"coverUrl"`
	InLibrary      bool    `db:"in_library" json:"inLibrary"`
	DownloadFormat string  `db:"download_format" json:"downloadFormat"`
	CreatedAt      int64   `db:"created_at" json:"createdAt"`
	UpdatedAt      int64   `db:"updated_at" json:"updatedAt"`
}

type Chapter struct {
	ID              string   `db:"id" json:"id"`
	MangaID         string   `db:"manga_id" json:"mangaId"`
	SourceChapterID string   `db:"source_chapter_id" json:"sourceChapterId"`
	ChapterNumber   *float64 `db:"chapter_number" json:"chapterNumber"`
	Title           *string  `db:"title" json:"title"`
	Language        *string  `db:"language" json:"language"`
	UploadedAt      *int64   `db:"uploaded_at" json:"uploadedAt"`
	Scanlator       *string  `db:"scanlator" json:"scanlator"`
	Downloaded      bool     `db:"downloaded" json:"downloaded"`
	DownloadPath    *string  `db:"download_path" json:"downloadPath"`
}

type ReadingProgress struct {
	MangaID           string `db:"manga_id" json:"mangaId"`
	LastReadChapterID string `db:"last_read_chapter_id" json:"lastReadChapterId"`
	LastReadPage      int    `db:"last_read_page" json:"lastReadPage"`
	TotalPages        int    `db:"total_pages" json:"totalPages"`
	IsCompleted       bool   `db:"is_completed" json:"isCompleted"`
	LastReadAt        int64  `db:"last_read_at" json:"lastReadAt"`
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
