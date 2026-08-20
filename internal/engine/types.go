package engine

import "encoding/json"

// ABIVersion is the MakiNuki ABI contract version this host implements.
// Plugins whose abiVersion differs are rejected before execution.
const ABIVersion = 1

// Plugin export names defined by the ABI contract.
const (
	ExportGetMetadata     = "get_metadata"
	ExportGetFilters      = "get_filters"
	ExportSearch          = "search"
	ExportGetDetails      = "get_details"
	ExportGetPages        = "get_pages"
	ExportUnscrambleImage = "unscramble_image"
)

// HttpRequest is the makinuki_fetch input payload.
type HttpRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body"`
}

// HttpResponse is the makinuki_fetch success payload.
type HttpResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// HttpError is the makinuki_fetch payload emitted when the host cannot
// deliver a response, for example when an anti-bot challenge blocks the
// request.
type HttpError struct {
	Error   ErrorCode `json:"error"`
	Status  int       `json:"status,omitempty"`
	URL     string    `json:"url,omitempty"`
	Message string    `json:"message,omitempty"`
}

// StorageEntry is the makinuki_storage_set input payload.
type StorageEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LogEntry is the makinuki_log input payload. Level is one of debug, info,
// warn, error.
type LogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// SourceMetadata is the get_metadata payload. Static exports return raw JSON
// and are never wrapped in the result envelope.
type SourceMetadata struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	ABIVersion   int      `json:"abiVersion"`
	Lang         string   `json:"lang"`
	BaseURL      string   `json:"baseUrl"`
	IconURL      string   `json:"iconUrl"`
	NSFW         bool     `json:"nsfw"`
	AllowedHosts []string `json:"allowedHosts,omitempty"`
}

// SearchQuery is the search input payload.
type SearchQuery struct {
	Query   string         `json:"query"`
	Page    int            `json:"page"`
	Filters map[string]any `json:"filters,omitempty"`
}

// PageResult is the paginated container returned by search.
type PageResult struct {
	Page        int         `json:"page"`
	HasNextPage bool        `json:"hasNextPage"`
	Items       []MangaItem `json:"items"`
}

// MangaItem is a search result entry.
type MangaItem struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	CoverURL      string `json:"coverUrl"`
	LatestChapter string `json:"latestChapter,omitempty"`
	URL           string `json:"url,omitempty"`
}

// MangaDetails is the get_details payload.
type MangaDetails struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	AltTitles   []string      `json:"altTitles,omitempty"`
	Description string        `json:"description,omitempty"`
	Authors     []string      `json:"authors,omitempty"`
	Artists     []string      `json:"artists,omitempty"`
	Genres      []string      `json:"genres,omitempty"`
	Status      string        `json:"status"`
	CoverURL    string        `json:"coverUrl"`
	Chapters    []ChapterItem `json:"chapters"`
}

// ChapterItem is a chapter listing entry. Number is nil for oneshots, extras,
// and unnumbered specials.
type ChapterItem struct {
	ID         string   `json:"id"`
	Number     *float64 `json:"number"`
	Language   string   `json:"language,omitempty"`
	Title      string   `json:"title,omitempty"`
	UploadedAt *int64   `json:"uploadedAt,omitempty"`
	Scanlator  string   `json:"scanlator,omitempty"`
	URL        string   `json:"url,omitempty"`
}

// PageItem is a single reader page. Headers must be replayed on the image
// request; scrambled pages are routed through unscramble_image before
// rendering.
type PageItem struct {
	Index       int               `json:"index"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	IsScrambled bool              `json:"isScrambled"`
	Metadata    *ScrambleInfo     `json:"metadata,omitempty"`
}

// ScrambleInfo describes the tile map of a scrambled image.
type ScrambleInfo struct {
	Layout string `json:"layout"`
	Rows   int    `json:"rows"`
	Cols   int    `json:"cols"`
	TileW  int    `json:"tileW"`
	TileH  int    `json:"tileH"`
	Order  []int  `json:"order"`
}

// pluginResult is the envelope wrapping every dynamic export payload. Success
// and failure are discriminated on ok alone, never on an HTTP status or a
// trapped call.
type pluginResult struct {
	OK    *bool           `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    ErrorCode `json:"code"`
		Message string    `json:"message"`
	} `json:"error"`
}
