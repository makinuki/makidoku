package tachibackup

import (
	"net/url"
	"strings"
)

// keyKind selects the URL translation a matched source uses. A backup records
// the series and chapter locators of the writing application, while an
// installed source expects its own identifier, so the importer translates one
// into the other.
type keyKind int

const (
	keyDefault keyKind = iota
	keyMangaDex
	keyAsuraScans
)

// sourceKeyKinds maps a normalised source name or plugin key onto its
// translation rules. Every other source uses keyDefault.
var sourceKeyKinds = map[string]keyKind{
	"mangadex":   keyMangaDex,
	"asurascans": keyAsuraScans,
	"asura":      keyAsuraScans,
	"asuracomic": keyAsuraScans,
}

// keyKindFor resolves the translation rules for an installed source from its
// name and plugin key. An unknown source falls back to the default rules.
func keyKindFor(names ...string) keyKind {
	for _, name := range names {
		if kind, ok := sourceKeyKinds[normalizeName(name)]; ok {
			return kind
		}
	}
	return keyDefault
}

// seriesKey derives the identifier an installed source expects for a series
// from the series locator a backup carries. The second result reports whether
// a known rule matched; an unmatched shape still yields a usable key through
// the default rule.
func seriesKey(kind keyKind, raw string) string {
	segments := pathSegments(raw)
	switch kind {
	case keyMangaDex:
		if value := segmentAfter(segments, "manga", "title"); value != "" {
			return value
		}
	case keyAsuraScans:
		if value := segmentAfter(segments, "series", "comics"); value != "" {
			return value
		}
	}
	return lastSegment(raw, segments)
}

// chapterKey derives the identifier an installed source expects for a chapter.
// seriesID is the translated series identifier, which the Asura template
// embeds, and baseURL is the installed source's front page.
func chapterKey(kind keyKind, raw, seriesID, baseURL string) string {
	segments := pathSegments(raw)
	switch kind {
	case keyMangaDex:
		if value := segmentAfter(segments, "chapter"); value != "" {
			return value
		}
	case keyAsuraScans:
		number := segmentAfter(segments, "chapter")
		if number == "" {
			number = lastSegment(raw, segments)
		}
		if base := strings.TrimRight(strings.TrimSpace(baseURL), "/"); base != "" && seriesID != "" && number != "" {
			return base + "/comics/" + seriesID + "/chapter/" + number
		}
	}
	return lastSegment(raw, segments)
}

// seriesPageURL returns the absolute series page for a title. An installed
// source's own template wins because the recorded path shape may be stale; a
// value that cannot be rebuilt is returned unchanged.
func seriesPageURL(kind keyKind, raw, seriesID, baseURL string) string {
	value := strings.TrimSpace(raw)
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch kind {
	case keyMangaDex:
		if base != "" && seriesID != "" {
			return base + "/title/" + seriesID
		}
	case keyAsuraScans:
		if base != "" && seriesID != "" {
			return base + "/comics/" + seriesID
		}
	default:
		if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
			return parsed.String()
		}
		// Only a value shaped like a path can be rebased; an opaque identifier
		// has no page on its own.
		if path := pathOnly(value); base != "" && strings.HasPrefix(path, "/") {
			return base + path
		}
	}
	return value
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

// segmentAfter returns the segment that follows the first segment equal to one
// of keys, ignoring case.
func segmentAfter(segments []string, keys ...string) string {
	for index, segment := range segments {
		for _, key := range keys {
			if strings.EqualFold(segment, key) && index+1 < len(segments) {
				return segments[index+1]
			}
		}
	}
	return ""
}

// lastSegment returns the final path segment, falling back to the raw value
// when the locator carries no path.
func lastSegment(raw string, segments []string) string {
	if len(segments) > 0 {
		return segments[len(segments)-1]
	}
	return strings.TrimSpace(raw)
}
