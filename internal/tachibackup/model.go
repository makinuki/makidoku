// Package tachibackup decodes and imports the protobuf backup document written
// by the Android application. Only the fields MakiDoku can store are modelled;
// the fields it cannot store are left out of the model and reported by the
// import summary.
package tachibackup

// Backup is the top-level backup record.
type Backup struct {
	Manga      []Manga
	Categories []Category
	Sources    []Source
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
	Name  string
	Order int64
	ID    int64
	Flags int64
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
