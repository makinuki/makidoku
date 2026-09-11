// Package tachibackup decodes and imports the protobuf backup document written
// by the Android application. Only the fields MakiDoku can store are modelled;
// the fields it cannot store are left out of the model and reported by the
// import summary.
package tachibackup

// Backup is the top-level backup record.
type Backup struct {
	Manga         []Manga
	Categories    []Category
	Sources       []Source
	SavedSearches []SavedSearch
	Feeds         []Feed
	// Preferences, SourcePreferences and ExtensionStores are counted but never
	// stored: they describe the writing install, not the library. Preference
	// values can carry credentials, so they are not modelled at all.
	Preferences       int
	SourcePreferences int
	ExtensionStores   int
}

// Manga is one library entry. Categories holds backup category ids, Tracking
// holds the remote tracker bindings, and History holds the per-chapter reading
// history. ViewerFlags is a pointer so an absent field can be told apart from
// an explicit zero.
type Manga struct {
	Source             int64
	URL                string
	Title              string
	Artist             string
	Author             string
	Description        string
	Genre              []string
	Status             int32
	ThumbnailURL       string
	DateAdded          int64
	Viewer             int32
	Chapters           []Chapter
	Categories         []int64
	Tracking           []Tracking
	Favorite           bool
	ChapterFlags       int32
	ViewerFlags        *int32
	History            []History
	UpdateStrategy     int32
	LastModifiedAt     int64
	FavoriteModifiedAt *int64
	ExcludedScanlators []string
	Version            int64
	Notes              string
	Initialized        bool
	// Memo is the writer's per-title JSON state; it is stored verbatim.
	Memo string
	// The custom fields are user overrides the writer carries alongside the
	// source values. CustomGenre is a full replacement list, not a delta.
	CustomStatus       int32
	CustomThumbnailURL string
	CustomTitle        string
	CustomArtist       string
	CustomAuthor       string
	CustomDescription  string
	CustomGenre        []string
	// MergedReferences links additional sources into the title and
	// FlatMetadata carries the source-attached metadata record.
	MergedReferences []MergedReference
	FlatMetadata     *FlatMetadata
}

// Chapter is one chapter record. LastPageRead is a zero-based page index in
// the source format and ChapterNumber may be absent, in which case it is
// derived from the name.
type Chapter struct {
	URL            string
	Name           string
	Scanlator      string
	Read           bool
	Bookmark       bool
	LastPageRead   int64
	DateFetch      int64
	DateUpload     int64
	ChapterNumber  float32
	SourceOrder    int64
	LastModifiedAt int64
	Version        int64
	// Memo is the writer's per-chapter JSON state; it is stored verbatim.
	Memo string
}

// History is one read-history record. LastRead is a Unix millisecond stamp and
// ReadDuration is a millisecond duration.
type History struct {
	URL          string
	LastRead     int64
	ReadDuration int64
}

// Category is one library category.
type Category struct {
	Name   string
	Order  int64
	ID     int64
	Flags  int64
	Hidden bool
}

// SavedSearch is a stored source search: a query plus the source's own filter
// values, which are opaque to this package.
type SavedSearch struct {
	Name       string
	Query      string
	FilterList string
	Source     int64
}

// Feed is one browse shortcut. A feed tied to a saved search opens that search
// on the source; a global feed opens the source itself. Global defaults to
// true because the writer omits a default value.
type Feed struct {
	Source      int64
	Global      bool
	SavedSearch *SavedSearch
}

// MergedReference is one additional source merged into a title.
type MergedReference struct {
	IsInfoManga       bool
	GetChapterUpdates bool
	ChapterSortMode   int32
	ChapterPriority   int32
	DownloadChapters  bool
	MergeURL          string
	MangaURL          string
	MangaSourceID     int64
}

// FlatMetadata is the metadata record a source attaches to a title, together
// with the alternative titles and tags that accompany it.
type FlatMetadata struct {
	Uploader     string
	Extra        string
	IndexedExtra string
	ExtraVersion int32
	Tags         []FlatMetadataTag
	Titles       []FlatMetadataTitle
}

// FlatMetadataTag is one tag of a metadata record, optionally namespaced.
type FlatMetadataTag struct {
	Namespace string
	Name      string
	Type      int32
}

// FlatMetadataTitle is one alternative title of a metadata record.
type FlatMetadataTitle struct {
	Title string
	Type  int32
}

// Tracking is one remote tracker binding. SyncID is the tracker's numeric
// identifier.
type Tracking struct {
	SyncID              int32
	LibraryID           int64
	MediaIDInt          int32
	TrackingURL         string
	Title               string
	LastChapterRead     float32
	TotalChapters       int32
	Score               float32
	Status              int32
	StartedReadingDate  int64
	FinishedReadingDate int64
	Private             bool
	MediaID             int64
}

// Source names one source that appears in the backup. SourceID is the numeric
// identifier the writer assigned to the source extension.
type Source struct {
	Name     string
	SourceID int64
}
