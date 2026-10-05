package migration

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CleanTitle reduces a title to the part that identifies the series, dropping
// the parts that decorate it: bracketed annotations, chapter markers, and
// punctuation that varies between sources.
//
// Several steps are guarded by a length check. Stripping is skipped when it
// would leave too little text, because a title that is mostly brackets or
// mostly punctuation is short enough that the series name *is* the whole thing,
// and reducing it to a stub would make it match the wrong series.
func CleanTitle(title string) string {
	lowered := strings.ToLower(title)

	cleaned := removeTextInBrackets(lowered, true)
	// A title that was mostly one bracket group leaves almost nothing, which
	// means the brackets were part of the name rather than an annotation. Try
	// reading the delimiters the other way round and keep whichever result has
	// more text.
	if runeLen(cleaned) <= 5 {
		cleaned = removeTextInBrackets(lowered, false)
	}

	cleaned = strings.TrimSpace(chapterMarker.ReplaceAllString(cleaned, " "))

	// Titles are routinely CJK, Cyrillic or otherwise non-ASCII, and for those
	// the letters are the series name. Stripping punctuation to ASCII letters
	// alone would empty the title, so a title carrying any non-ASCII letter
	// keeps its letters in every script and loses only what is not one.
	//
	// The condition is the presence of a non-ASCII letter rather than the
	// length of what stripping would leave. A length test misfires on exactly
	// the titles it is meant to protect: a Cyrillic title of eight letters
	// reduces to eight spaces, which is long enough to look like a successful
	// strip while having removed the entire name.
	if hasNonASCIILetter(cleaned) {
		cleaned = nonLetter.ReplaceAllString(cleaned, " ")
	} else {
		cleaned = nonAlphanumeric.ReplaceAllString(cleaned, " ")
	}

	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.ReplaceAll(cleaned, " - ", " ")
	cleaned = spaceRun.ReplaceAllString(cleaned, " ")
	return strings.TrimSpace(cleaned)
}

// DeepQueries returns the queries a deep search runs for a title. A source that
// does not match on the full title often still matches on its distinctive words,
// so the title is also searched by its longest words and its leading words.
//
// The results are deduplicated and keep their order, because the first query is
// the full title and should stay the primary one.
func DeepQueries(cleanedTitle string) []string {
	words := strings.Fields(cleanedTitle)
	if len(words) == 0 {
		return nil
	}
	longestFirst := slices.Clone(words)
	sort.SliceStable(longestFirst, func(i, j int) bool {
		return runeLen(longestFirst[i]) > runeLen(longestFirst[j])
	})

	groups := [][]string{
		{cleanedTitle},
		take(longestFirst, 2),
		take(longestFirst, 1),
		take(words, 2),
		take(words, 1),
	}

	seen := make(map[string]struct{}, len(groups))
	queries := make([]string, 0, len(groups))
	for _, group := range groups {
		query := strings.TrimSpace(strings.Join(group, " "))
		if query == "" {
			continue
		}
		if _, ok := seen[query]; ok {
			continue
		}
		seen[query] = struct{}{}
		queries = append(queries, query)
	}
	return queries
}

// removeTextInBrackets drops bracket groups and anything nested inside them.
// Reading forward treats ([{< as opening; reading backward swaps the two sets,
// which is how a title that leads with its closing delimiter is recovered.
func removeTextInBrackets(text string, readForward bool) string {
	opening, closing := "([{<", ")]}>"
	if !readForward {
		opening, closing = closing, opening
	}
	runes := []rune(text)
	if !readForward {
		slices.Reverse(runes)
	}
	var kept strings.Builder
	kept.Grow(len(text))
	depth := 0
	for _, r := range runes {
		switch {
		case strings.ContainsRune(opening, r):
			depth++
		case strings.ContainsRune(closing, r) && depth > 0:
			depth--
		case depth == 0:
			kept.WriteRune(r)
		}
	}
	if readForward {
		return kept.String()
	}
	reversed := []rune(kept.String())
	slices.Reverse(reversed)
	return string(reversed)
}

func take(items []string, n int) []string {
	return items[:min(n, len(items))]
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}

// hasNonASCIILetter reports whether value carries a letter outside ASCII.
func hasNonASCIILetter(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

var (
	// Chapter markers in Russian transliteration, which sources append to a
	// series title when a specific chapter is the entry point.
	chapterMarker = regexp.MustCompile(`(-\s?часть\s?|\-\s?глава\s?)\d*`)
	// Anything outside ASCII letters, digits, hyphen and space.
	nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9- ]`)
	// Any letter in any script, plus digits, hyphen and space.
	nonLetter = regexp.MustCompile(`[^\p{L}0-9- ]`)
	spaceRun  = regexp.MustCompile(` +`)
)
