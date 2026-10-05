// Package migration matches titles across sources.
//
// Migrating a title means deciding that one source's entry for a series is the
// same series as another source's entry. Deciding that by title text alone is
// unreliable, so this package normalises titles and scores candidates against
// them, which lets the caller rank a shortlist and reject weak matches before
// anything is written to the database.
package migration

import (
	"unicode/utf8"

	"github.com/agnivade/levenshtein"
)

// MinEligibleThreshold is the lowest similarity a candidate may score and still
// be considered a match. Below it the candidate is treated as unrelated rather
// than as a weak match, so that a source returning loosely related titles does
// not present them as migration targets.
const MinEligibleThreshold = 0.4

// Similarity reports how alike two titles are, from 0 (nothing in common) to 1
// (identical).
//
// The comparison is over runes, not bytes. Manga titles are routinely CJK,
// Cyrillic or accented Latin, and a byte-wise comparison scores those as
// near-misses because every multi-byte rune differs in every byte.
func Similarity(a, b string) float64 {
	longest := max(utf8.RuneCountInString(a), utf8.RuneCountInString(b))
	if longest == 0 {
		// Two empty titles are the same title.
		return 1
	}
	score := 1 - float64(levenshtein.ComputeDistance(a, b))/float64(longest)
	if score < 0 {
		return 0
	}
	return score
}

// Candidate is a search result that can be scored against a title. Only the
// fields matching needs are carried; the caller keeps the full result and
// resolves it by the index Match reports.
type Candidate struct {
	ID    string
	Title string
}

// Match returns the index of the best candidate for want, with the score it
// earned, and reports whether any candidate cleared MinEligibleThreshold.
//
// queryCount is how many queries produced candidates. When a single query
// returned a single candidate, the source's own relevance is trusted and the
// candidate is accepted without scoring, because the source chose it for this
// exact query and a title comparison adds nothing. This mirrors the behaviour
// of the engine this was ported from, and it is the one case where a candidate
// can be accepted without meeting the threshold.
//
// deep selects the deep-cleaning comparison, in which both sides are run
// through CleanTitle first. It expects want to be the raw title; a caller using
// deep search should pass the title as stored rather than a pre-cleaned one.
func Match(candidates []Candidate, want string, queryCount int, deep bool) (index int, score float64, found bool) {
	if len(candidates) == 0 {
		return 0, 0, false
	}
	target := want
	if deep {
		target = CleanTitle(want)
	}
	best := -1
	bestScore := 0.0
	for i, candidate := range candidates {
		candidateScore := 1.0
		if queryCount > 1 || len(candidates) > 1 {
			title := candidate.Title
			if deep {
				title = CleanTitle(title)
			}
			candidateScore = Similarity(target, title)
		}
		if candidateScore < MinEligibleThreshold {
			continue
		}
		if best < 0 || candidateScore > bestScore {
			best, bestScore = i, candidateScore
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return best, bestScore, true
}
