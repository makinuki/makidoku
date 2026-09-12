package tachibackup

import (
	"net/url"
	"strings"
)

// seriesLocator returns the series locator a backup recorded, unchanged. The
// recorded value is the writing application's own identifier for the series,
// and only the source that recorded it knows how to read it, so the importer
// stores it as written instead of translating it.
func seriesLocator(raw string) string {
	return strings.TrimSpace(raw)
}

// chapterLocator returns the chapter locator a backup recorded, unchanged.
func chapterLocator(raw string) string {
	return strings.TrimSpace(raw)
}

// seriesPageURL returns the series page a backup recorded, and an empty string
// when the recorded locator is not already an absolute URL. A relative path or
// an opaque identifier is not a page of its own once it is separated from the
// site it was recorded on, so the page is resolved from the source the first
// time the title is opened.
func seriesPageURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() {
		return ""
	}
	return parsed.String()
}

// pathSegments returns the non-empty path segments of a backup locator. A
// value without a scheme is parsed as a relative reference, which still
// separates its path from any query or fragment.
func pathSegments(raw string) []string {
	out := []string{}
	for _, segment := range strings.Split(pathOnly(raw), "/") {
		if segment = strings.TrimSpace(segment); segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

// pathOnly returns the path of a backup locator with any query and fragment
// removed. An absolute URL contributes its path; a relative value is already
// a path.
func pathOnly(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil {
		return parsed.Path
	}
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		return value[:index]
	}
	return value
}
