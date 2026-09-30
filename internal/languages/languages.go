// Package languages normalizes ISO 639-1 language codes and resolves the
// chapter-language selections hosts apply to stored chapter payloads.
package languages

import (
	"sort"
	"strings"
)

// Normalize canonicalizes a language code so "PT-BR" and " pt-br " compare
// equal to "pt-br".
func Normalize(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// Set normalizes a selection, dropping blanks and duplicates and sorting the
// result so stored selections compare deterministically. An empty selection
// returns nil.
func Set(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		normalized := Normalize(code)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// Allows reports whether a chapter language passes a selection. An empty
// selection allows everything. A chapter with no language is unknown rather
// than non-matching, so a selection never hides it.
func Allows(selection []string, language string) bool {
	if len(selection) == 0 {
		return true
	}
	normalized := Normalize(language)
	if normalized == "" {
		return true
	}
	for _, item := range selection {
		if item == normalized {
			return true
		}
	}
	return false
}

// ParseList parses the comma-separated form settings store.
func ParseList(value string) []string {
	return Set(strings.Split(value, ","))
}

// FormatList renders a selection as the comma-separated form settings store.
func FormatList(codes []string) string {
	return strings.Join(Set(codes), ",")
}
