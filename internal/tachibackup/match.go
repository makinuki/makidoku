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
