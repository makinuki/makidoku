package languages

import (
	"reflect"
	"testing"
)

func TestSetNormalizesAndDeduplicates(t *testing.T) {
	got := Set([]string{" EN ", "pt-BR", "en", "", "  "})
	want := []string{"en", "pt-br"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Set = %v, want %v", got, want)
	}
	if empty := Set(nil); empty != nil {
		t.Fatalf("Set(nil) = %v, want nil", empty)
	}
}

func TestAllowsTreatsUnknownLanguageAsVisible(t *testing.T) {
	selection := []string{"en"}
	cases := map[string]bool{
		"en":    true,
		"EN":    true,
		"ja":    false,
		"":      true,
		"  ":    true,
		"fr-CA": false,
	}
	for language, want := range cases {
		if got := Allows(selection, language); got != want {
			t.Fatalf("Allows(%q) = %v, want %v", language, got, want)
		}
	}
	if !Allows(nil, "ja") {
		t.Fatal("an empty selection must allow every language")
	}
}

func TestListRoundTrip(t *testing.T) {
	if got := FormatList([]string{"JA", "en", "en"}); got != "en,ja" {
		t.Fatalf("FormatList = %q, want en,ja", got)
	}
	if got := ParseList(" en , pt-br "); !reflect.DeepEqual(got, []string{"en", "pt-br"}) {
		t.Fatalf("ParseList = %v", got)
	}
	if got := ParseList(""); got != nil {
		t.Fatalf("ParseList(empty) = %v, want nil", got)
	}
}
