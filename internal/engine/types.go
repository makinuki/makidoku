package engine

import "encoding/json"

// ABIVersion is the MakiNuki ABI contract version this host implements.
// Plugins whose abiVersion differs are rejected before execution.
const ABIVersion = 1

// Plugin export names defined by the ABI contract.
const (
	ExportGetMetadata     = "get_metadata"
	ExportGetFilters      = "get_filters"
	ExportGetSettings     = "get_settings"
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
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	ABIVersion   int             `json:"abiVersion"`
	Lang         string          `json:"lang"`
	BaseURL      string          `json:"baseUrl"`
	IconURL      string          `json:"iconUrl"`
	NSFW         bool            `json:"nsfw"`
	AllowedHosts []string        `json:"allowedHosts,omitempty"`
	RateLimit    *RateLimitHints `json:"rateLimit,omitempty"`
	Retry        *RetryHints     `json:"retry,omitempty"`
}

// RateLimitHints is the request pacing policy a source suggests. Absent
// fields leave the host default in place; hosts may honor, cap, or let users
// override the values.
type RateLimitHints struct {
	IntervalMs *int64 `json:"intervalMs,omitempty"`
	Burst      *int64 `json:"burst,omitempty"`
}

// RetryHints is the retry policy a source suggests for failed requests.
type RetryHints struct {
	MaxAttempts *int64 `json:"maxAttempts,omitempty"`
	BackoffMs   *int64 `json:"backoffMs,omitempty"`
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
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	CoverURL      string         `json:"coverUrl,omitempty"`
	LatestChapter string         `json:"latestChapter,omitempty"`
	URL           string         `json:"url,omitempty"`
	Covers        []CoverVariant `json:"covers,omitempty"`
}

// CoverVariant describes one rendition of the cover artwork. Width and
// height carry the served pixel size when the source declares it.
type CoverVariant struct {
	URL    string `json:"url"`
	Width  *int   `json:"width,omitempty"`
	Height *int   `json:"height,omitempty"`
}

// MangaDetails is the get_details payload.
type MangaDetails struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	AltTitles   []string       `json:"altTitles,omitempty"`
	Description string         `json:"description,omitempty"`
	Authors     []string       `json:"authors,omitempty"`
	Artists     []string       `json:"artists,omitempty"`
	Genres      []string       `json:"genres,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Status      string         `json:"status"`
	CoverURL    string         `json:"coverUrl,omitempty"`
	Chapters    []ChapterItem  `json:"chapters"`
	Covers      []CoverVariant `json:"covers,omitempty"`
}

// ChapterItem is a chapter listing entry. Number is nil for oneshots, extras,
// and unnumbered specials; Volume carries the source-declared volume grouping
// when present.
type ChapterItem struct {
	ID         string   `json:"id"`
	Number     *float64 `json:"number"`
	Volume     *int64   `json:"volume,omitempty"`
	Language   string   `json:"language,omitempty"`
	Title      string   `json:"title,omitempty"`
	UploadedAt *int64   `json:"uploadedAt,omitempty"`
	Scanlator  string   `json:"scanlator,omitempty"`
	Locked     bool     `json:"locked,omitempty"`
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

// SettingKind is one of the setting kinds a source may declare.
type SettingKind string

const (
	SettingKindCheckbox SettingKind = "checkbox"
	SettingKindSelect   SettingKind = "select"
	SettingKindText     SettingKind = "text"
)

// SettingOption is one label/value pair of a select setting.
type SettingOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SettingSchema is one declared setting returned by get_settings. Default is
// the raw JSON default value whose type depends on Type: a boolean for
// checkbox, a string for select and text.
type SettingSchema struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Type        SettingKind     `json:"type"`
	Options     []SettingOption `json:"options,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Sensitive   bool            `json:"sensitive,omitempty"`
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
