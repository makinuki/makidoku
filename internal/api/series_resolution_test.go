package api

import "testing"

// lastPathSegment reduces every legacy locator shape a backup records to the
// segment a plugin publishes as its own id.
func TestLastPathSegment(t *testing.T) {
	cases := map[string]string{
		"/manga/0211ade1-e04a-4dcf-a2bf-f0e6636ffdbf": "0211ade1-e04a-4dcf-a2bf-f0e6636ffdbf",
		"/series/solo-leveling":                       "solo-leveling",
		"/manga/jeonjijeok-dokja-sijeomm.5k9q":        "jeonjijeok-dokja-sijeomm.5k9q",
		"https://example.test/series/demo/":           "demo",
		"bare-id":                                     "bare-id",
		"/manga/uuid?style=pages":                     "uuid",
		"/manga/uuid#fragment":                        "uuid",
		"  ":                                          "",
	}
	for input, want := range cases {
		if got := lastPathSegment(input); got != want {
			t.Fatalf("lastPathSegment(%q) = %q, want %q", input, got, want)
		}
	}
}

// The trailing token is the shape rule for a source that names its series
// "<name>.<id>". A value without that shape yields nothing rather than
// repeating the identifier.
func TestTrailingToken(t *testing.T) {
	cases := map[string]string{
		"jeonjijeok-dokja-sijeomm.5k9q":        "5k9q",
		"0211ade1-e04a-4dcf-a2bf-f0e6636ffdbf": "",
		"name.":                                "",
		".id":                                  "id",
		"":                                     "",
	}
	for input, want := range cases {
		if got := trailingToken(input); got != want {
			t.Fatalf("trailingToken(%q) = %q, want %q", input, got, want)
		}
	}
}

// The ladder offers the recorded page, the last path segment and its trailing
// token, in that order and without duplicates.
func TestSeriesCandidates(t *testing.T) {
	if got, want := seriesCandidates("/manga/jeonjijeok-dokja-sijeomm.5k9q", ""), []string{"jeonjijeok-dokja-sijeomm.5k9q", "5k9q"}; !equalStrings(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got, want := seriesCandidates("/manga/uuid", "uuid"), []string{"uuid"}; !equalStrings(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got := seriesCandidates("  ", "  "); len(got) != 0 {
		t.Fatalf("blank locator produced %v, want none", got)
	}
}

// A search hit is accepted when it carries one of the tokens the recorded
// locator offered as the source own id.
func TestMatchesAnyToken(t *testing.T) {
	if !matchesAnyToken([]string{"5k9q", "jeonjijeok-dokja-sijeomm.5k9q"}, "5k9q") {
		t.Fatal("trailing token did not match the plugin id")
	}
	if matchesAnyToken(nil, "5k9q") {
		t.Fatal("an empty token set matched an identifier")
	}
	if matchesAnyToken([]string{"5k9q"}, "") {
		t.Fatal("a blank identifier matched a token")
	}
}

// A stored link and a published identifier can be two spellings of one
// identity, so the page url lookup accepts the last path segment of either.
func TestSourceURLMatches(t *testing.T) {
	cases := []struct {
		name        string
		stored      string
		identifiers []string
		want        bool
	}{
		{"recorded path against the published id", "/manga/7c062006-5ea7-4fbc-a39d-23e3f66420e4", []string{"7c062006-5ea7-4fbc-a39d-23e3f66420e4", "https://mangadex.org/title/7c062006-5ea7-4fbc-a39d-23e3f66420e4"}, true},
		{"the recorded value itself", "/series/nano-machine", []string{"/series/nano-machine"}, true},
		{"a different title", "/series/nano-machine", []string{"/series/other", "https://asurascans.com/comics/other"}, false},
		{"a bare stored identifier", "5k9q", []string{"5k9q"}, true},
	}
	for _, testCase := range cases {
		if got := sourceURLMatches(testCase.stored, testCase.identifiers...); got != testCase.want {
			t.Fatalf("%s: sourceURLMatches(%q, %q) = %v, want %v", testCase.name, testCase.stored, testCase.identifiers, got, testCase.want)
		}
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
