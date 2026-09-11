package tachibackup

import (
	"net/url"
	"strings"
)

// SourceRef describes one installed source as far as source matching needs it.
// BaseURL is the source's front page and PluginKey is its plugin identifier.
type SourceRef struct {
	ID        string
	PluginKey string
	Name      string
	BaseURL   string
}

// sourceAliases maps a normalised backup source name to other normalised names
// that identify the same site. An extension can be renamed between releases,
// and the backup keeps the name the writing install used.
var sourceAliases = map[string][]string{
	"asurascans": {"asurascans", "asura", "asuracomic"},
	"mangadex":   {"mangadex"},
}

// matchSource resolves a backup source to an installed source. Matching is by
// normalised name first, then by the registered host of the series URLs the
// backup carries. It returns the matched source and how it matched.
func matchSource(name string, seriesURLs []string, installed []SourceRef) (SourceRef, string, bool) {
	needle := normalizeName(name)
	if needle != "" {
		for _, candidate := range installed {
			if normalizeName(candidate.Name) == needle || normalizeName(candidate.PluginKey) == needle {
				return candidate, "name", true
			}
		}
		aliases := sourceAliases[needle]
		for _, candidate := range installed {
			if containsName(aliases, normalizeName(candidate.Name)) || containsName(aliases, normalizeName(candidate.PluginKey)) {
				return candidate, "alias", true
			}
		}
	}
	hosts := map[string]struct{}{}
	for _, raw := range seriesURLs {
		if host := registeredHost(raw); host != "" {
			hosts[host] = struct{}{}
		}
	}
	if len(hosts) == 0 {
		return SourceRef{}, "", false
	}
	for _, candidate := range installed {
		if host := registeredHost(candidate.BaseURL); host != "" {
			if _, ok := hosts[host]; ok {
				return candidate, "host", true
			}
		}
	}
	return SourceRef{}, "", false
}

func containsName(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// SiteDetection describes an unmatched backup source as it appears on the
// wire: the host the writing install recorded and the source name that host
// belongs to when it is known.
type SiteDetection struct {
	Host string
	Name string
}

// sourceHosts maps a host suffix onto the source name that serves it. The
// table lets an unnamed backup source be identified from the locators it
// carries, so the import can suggest a name before the user maps it.
var sourceHosts = map[string]string{
	"comick.pictures":      "Comick",
	"comick.io":            "Comick",
	"atsu.moe":             "Atsumaru",
	"weebdex.org":          "WeebDex",
	"batcave.biz":          "BatCave",
	"mangadot.net":         "Mangadotnet",
	"mangapark.net":        "MangaPark",
	"mangapark.io":         "MangaPark",
	"mangafire.to":         "MangaFire",
	"readcomiconline.li":   "ReadComicOnline",
	"mangakakalot.gg":      "Mangakakalot",
	"mangago.me":           "Mangago",
	"madokami.com":         "Madokami",
	"omegascans.org":       "Omega Scans",
	"cubari.moe":           "Cubari",
	"kagane.to":            "Kagane",
	"comix.to":             "Comix",
	"weebcentral.com":      "Weeb Central",
	"compsci88.com":        "Weeb Central",
	"disasterscans.com":    "Disaster Scans",
	"asurascans.com":       "Asura Scans",
	"asuracomic.net":       "Asura Scans",
	"mangadex.org":         "MangaDex",
	"uploads.mangadex.org": "MangaDex",
}

// detectSite identifies an unmatched backup source from its locators. Series
// locators are inspected before covers so the most specific host wins. The
// first host seen is reported even when no name is known, so the report can
// still show the user where the titles came from.
func detectSite(urls []string) SiteDetection {
	out := SiteDetection{}
	// Hosts are checked across every locator first so a CDN host or a path
	// shape cannot claim the source before the site's own host is seen.
	for _, raw := range urls {
		host := hostname(raw)
		if host != "" && out.Host == "" {
			out.Host = host
		}
		if out.Name != "" || host == "" {
			continue
		}
		for suffix, name := range sourceHosts {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				out.Name = name
				break
			}
		}
	}
	if out.Name != "" {
		return out
	}
	for _, raw := range urls {
		if pathHasSegment(raw, "comic") {
			out.Name = "Comick"
			break
		}
	}
	return out
}

// pathHasSegment reports whether the first segment of a locator's path equals
// needle, ignoring case.
func pathHasSegment(raw, needle string) bool {
	segments := pathSegments(raw)
	if len(segments) == 0 {
		return false
	}
	return strings.EqualFold(segments[0], needle)
}

// hostname returns the lowercase host of a locator without a leading www
// label, or an empty string when the locator carries none.
func hostname(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}

// normalizeName reduces a source name to lowercase letters and digits so
// spacing and punctuation differences do not prevent a match.
func normalizeName(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
		}
	}
	return out.String()
}

// registeredHost returns the registrable host of a URL without a leading www
// label, so www.example.com and example.com compare equal.
func registeredHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return host
}
