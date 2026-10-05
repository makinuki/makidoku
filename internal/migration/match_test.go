package migration

import (
	"math"
	"strings"
	"testing"
)

func TestSimilarityIsRuneAware(t *testing.T) {
	// A byte-wise comparison sees no overlap between two different CJK titles
	// and scores them at zero. These are visually similar multi-byte strings, so
	// a rune-aware comparison must score them above an unrelated title.
	cases := []struct {
		name     string
		a, b     string
		minScore float64
	}{
		{"identical", "shounen", "shounen", 1},
		{"empty against empty", "", "", 1},
		{"ascii near miss", "one piece", "one peice", 0.75},
		{"cjk identical", "呪術廻戦", "呪術廻戦", 1},
		{"cjk one rune apart", "呪術廻戦", "呪術廻线", 0.5},
		{"cyrillic identical", "вокал маньяка", "вокал маньяка", 1},
		{"accents differ", "shounen", "shounén", 0.7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Similarity(tc.a, tc.b)
			if got < tc.minScore {
				t.Fatalf("Similarity(%q, %q) = %v, want at least %v", tc.a, tc.b, got, tc.minScore)
			}
			if got > 1 {
				t.Fatalf("Similarity(%q, %q) = %v, must not exceed 1", tc.a, tc.b, got)
			}
		})
	}
}

func TestSimilarityRejectsCrossoverAsUnrelated(t *testing.T) {
	// Distinct CJK titles must not score as matches of each other. This is the
	// case a byte-wise implementation gets wrong.
	got := Similarity("呪術廻戦", "進撃の巨人")
	if got >= MinEligibleThreshold {
		t.Fatalf("unrelated CJK titles scored %v, want below %v", got, MinEligibleThreshold)
	}
}

func TestSimilarityHandlesEmptyAgainstNonEmpty(t *testing.T) {
	if got := Similarity("", "shounen"); got != 0 {
		t.Fatalf("Similarity against empty = %v, want 0", got)
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"already clean", "one piece", "one piece"},
		{"lower cases", "One Piece", "one piece"},
		{"strips bracketed annotation", "Solo Leveling (Web Novel)", "solo leveling"},
		{"strips nested brackets", "Title [Outer (inner) text]", "title"},
		{"drops an unmatched closing bracket but keeps its text", "Title) extra", "title extra"},
		{"strips double brackets", "Title [a] [b]", "title"},
		{"keeps hyphenated words", "Fruits-Basket", "fruits-basket"},
		{"collapses repeated spaces", "one    piece", "one piece"},
		{"removes dash splitter", "One Piece - The Beginning", "one piece the beginning"},
		{"strips russian chapter marker", "Название - глава 12", "название"},
		{"empty stays empty", "", ""},
		{"only brackets cleans to empty", "[just an annotation]", ""},
		{"keeps cjk when ascii stripping would empty it", "人生ノート", "人生ノート"},
		{"keeps a cjk title beside latin text", "人生 - chapter 1", "人生 chapter 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CleanTitle(tc.input); got != tc.want {
				t.Fatalf("CleanTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCleanTitleKeepsNonLatinNames(t *testing.T) {
	// A title written entirely in a non-Latin script must survive cleaning with
	// its name intact. Reducing these to an empty string would leave deep search
	// with nothing to query and make every non-Latin series unmigratable.
	for _, input := range []string{
		"Название серии",
		"人生ノートの物語",
		"進撃の巨人",
		"백만물의 신",
		"\u0639\u0627\u0644\u0645 \u0627\u0644\u0641",
	} {
		cleaned := CleanTitle(input)
		if cleaned == "" {
			t.Fatalf("CleanTitle(%q) returned empty, want the name preserved", input)
		}
		// Cleaning lowercases, so the first letter to compare against is the
		// lowercased one rather than the character as written.
		first := strings.ToLower(string([]rune(input)[0]))
		if !strings.HasPrefix(cleaned, first) {
			t.Fatalf("CleanTitle(%q) = %q, want it to still begin with %q", input, cleaned, first)
		}
	}
}

func TestCleanTitleIsIdempotent(t *testing.T) {
	// Cleaning feeds similarity scoring, so cleaning twice must not keep
	// changing the title.
	for _, input := range []string{
		"One Piece (Web Novel) [Complete]",
		"呪術廻戦 - 1",
		"Название - глава 3",
		"Solo Leveling",
	} {
		once := CleanTitle(input)
		if twice := CleanTitle(once); once != twice {
			t.Fatalf("CleanTitle not idempotent for %q: %q then %q", input, once, twice)
		}
	}
}

func TestCleanTitleNeverPanicsOnBrackets(t *testing.T) {
	// Bracket handling tracks nesting depth, and a title with unbalanced
	// delimiters must not drive that counter somewhere the writer cannot
	// handle.
	for _, input := range []string{
		"", "(", ")", "((((", "))))", "([{", "}])", "()()()", "][", "a)b(c",
		strings.Repeat("(", 200), strings.Repeat("[", 200) + "x",
	} {
		got := CleanTitle(input)
		if strings.ContainsAny(got, "([{<)]}>") {
			t.Fatalf("CleanTitle(%q) = %q, brackets should have been removed", input, got)
		}
	}
}

func TestDeepQueries(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty title yields nothing", "", nil},
		{"whitespace yields nothing", "   ", nil},
		{
			name:  "expands to longest and leading words",
			input: "the return of the crazy demon",
			want: []string{
				"the return of the crazy demon",
				"return crazy",
				"return",
				"the return",
				"the",
			},
		},
		{"single word title yields one query", "solo", []string{"solo"}},
		{
			name:  "two word title keeps the longest word first",
			input: "solo leveling",
			want:  []string{"solo leveling", "leveling solo", "leveling", "solo"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeepQueries(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("DeepQueries(%q) = %q, want %q", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("DeepQueries(%q) = %q, want %q", tc.input, got, tc.want)
				}
			}
		})
	}
}

func TestDeepQueriesNeverReturnsBlank(t *testing.T) {
	// A blank query would be sent to a source as a search for nothing, which
	// returns an unfiltered catalogue and makes matching meaningless.
	for _, input := range []string{"", " ", "\t\n", strings.Repeat(" ", 20)} {
		for _, q := range DeepQueries(input) {
			if strings.TrimSpace(q) == "" {
				t.Fatalf("DeepQueries(%q) produced a blank query", input)
			}
		}
	}
}

func TestDeepQueriesDeduplicates(t *testing.T) {
	for _, input := range []string{"a b", "one two three", "solo leveling"} {
		queries := DeepQueries(input)
		seen := map[string]bool{}
		for _, q := range queries {
			if seen[q] {
				t.Fatalf("DeepQueries(%q) repeated %q: %q", input, q, queries)
			}
			seen[q] = true
		}
	}
}

func TestMatchPicksBestCandidate(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Title: "Some Unrelated Series"},
		{ID: "b", Title: "The Return of the Crazy Demon"},
		{ID: "c", Title: "The Return of the Crazy Demon (Alternative Title)"},
	}
	index, score, found := Match(candidates, "The Return of the Crazy Demon", 1, false)
	if !found {
		t.Fatal("expected a match")
	}
	if candidates[index].ID != "b" {
		t.Fatalf("matched %q, want the exact title", candidates[index].ID)
	}
	if math.Abs(score-1) > 1e-9 {
		t.Fatalf("score = %v, want 1 for an exact title", score)
	}
}

func TestMatchRejectsWeakCandidates(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Title: "Cooking Master Boy"},
		{ID: "b", Title: "Attack on Titan"},
	}
	if _, _, found := Match(candidates, "The Return of the Crazy Demon", 1, false); found {
		t.Fatal("unrelated titles must not be offered as a match")
	}
}

func TestMatchHandlesEmptyCandidateList(t *testing.T) {
	if _, _, found := Match(nil, "anything", 1, false); found {
		t.Fatal("an empty candidate list must not report a match")
	}
}

func TestMatchAcceptsSingleCandidateWithoutScoring(t *testing.T) {
	// One query returning exactly one candidate is trusted: the source chose it
	// for this exact query. This is the one path where the threshold is not
	// applied, and it is deliberate.
	candidates := []Candidate{{ID: "only", Title: "Something Wholly Unrelated"}}
	index, score, found := Match(candidates, "The Return of the Crazy Demon", 1, false)
	if !found {
		t.Fatal("a lone candidate from a single query must be accepted")
	}
	if candidates[index].ID != "only" || score != 1 {
		t.Fatalf("got index %d score %v, want the lone candidate at score 1", index, score)
	}

	// The same candidate is rejected once a second query widens the pool.
	if _, _, found := Match(candidates, "The Return of the Crazy Demon", 2, false); found {
		t.Fatal("with more than one query the threshold must apply")
	}
}

func TestMatchDeepCleaningIgnoresDecoration(t *testing.T) {
	// The titles differ only by decoration, which deep cleaning removes.
	candidates := []Candidate{
		{ID: "a", Title: "Solo Leveling (Web Novel) - Chapter 1"},
	}
	index, _, found := Match(candidates, "Solo Leveling", 1, true)
	if !found {
		t.Fatal("deep search must match a decorated title against its bare form")
	}
	if candidates[index].ID != "a" {
		t.Fatalf("matched %q, want a", candidates[index].ID)
	}
}

func TestMatchDeepCleaningMatchesAcrossScripts(t *testing.T) {
	// The point of the cleaning guard: a non-Latin title must still be
	// matchable, otherwise cleaning reduces both sides to nothing.
	candidates := []Candidate{
		{ID: "a", Title: "進撃の巨人"},
	}
	index, _, found := Match(candidates, "進撃の巨人 (Attack on Titan)", 1, true)
	if !found {
		t.Fatal("a decorated non-Latin title must match its bare form")
	}
	if candidates[index].ID != "a" {
		t.Fatalf("matched %q, want a", candidates[index].ID)
	}
}
