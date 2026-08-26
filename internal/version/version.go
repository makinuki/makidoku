package version

// Version is the semantic version of the makidoku binary.
// It is set at build time via ldflags. The default is the next unreleased
// version and is overwritten on tagged releases.
var (
	Version = "0.1.0"
	Commit  = "unknown"
	Date    = "unknown"
)
