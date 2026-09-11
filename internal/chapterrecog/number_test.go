package chapterrecog

import (
	"math"
	"testing"
)

// TestParse ports the chapter-recognition corpus: every case is a real release
// naming style, and the expected value is the number a reader would show.
func TestParse(t *testing.T) {
	cases := []struct {
		manga   string
		chapter string
		want    float64
	}{
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch.4: Misrepresentation", 4.0},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol. 1 Ch. 4: Misrepresentation", 4.0},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch.4.1: Misrepresentation", 4.1},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch.4.4: Misrepresentation", 4.4},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch.4.a: Misrepresentation", 4.1},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch.4.b: Misrepresentation", 4.2},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch.4.extra: Misrepresentation", 4.99},
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch. 4: Misrepresentation", 4.0},
		{"Bleach", "Bleach 567 Down With Snowwhite", 567.0},
		{"Bleach", "Bleach 567.1 Down With Snowwhite", 567.1},
		{"Bleach", "Bleach 567.4 Down With Snowwhite", 567.4},
		{"Bleach", "Bleach 567.a Down With Snowwhite", 567.1},
		{"Bleach", "Bleach 567.b Down With Snowwhite", 567.2},
		{"Bleach", "Bleach 567.extra Down With Snowwhite", 567.99},
		{"Solanin", "Solanin 028 Vol. 2", 28.0},
		{"Solanin", "Solanin 028.1 Vol. 2", 28.1},
		{"Solanin", "Solanin 028.4 Vol. 2", 28.4},
		{"Solanin", "Solanin 028.a Vol. 2", 28.1},
		{"Solanin", "Solanin 028.b Vol. 2", 28.2},
		{"Solanin", "Solanin 028.extra Vol. 2", 28.99},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 028", 28.0},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 028.1", 28.1},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 028.4", 28.4},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 028.a", 28.1},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 028.b", 28.2},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 028.extra", 28.99},
		{"Onepunch-Man", "Onepunch-Man Punch Ver002 086 : Creeping Darkness [3]", 86.0},
		{"random", "Vol.1 Ch.5v.2: Alones", 5.0},
		{"Ayame 14", "Ayame 14 1 - The summer of 14", 1.0},
		{"Ayame 14", "Vol.1 Ch.1: March 25 (First Day Cohabiting)", 1.0},
		{"random", "Vol.001 Ch.003: Kaguya Doesn't Know Much", 3.0},
		{"Ansatsu Kyoushitsu", "Ansatsu Kyoushitsu 011v002: Assembly Time", 11.0},
		{"Tokyo ESP", "Tokyo ESP 027: Part 002: Chapter 001", 27.0},
		{"One-punch Man", "Mag Version 195.5", 195.5},
		{"random", "Fairy Tail 404: 00:00", 404.0},
		{"random", "Asu No Yoichi 19a", 19.1},
		{"Fairy Tail", "Fairy Tail 404.extravol002", 404.99},
		{"Fairy Tail", "Fairy Tail 404 extravol002", 404.99},
		{"Fairy Tail", "Fairy Tail 404.omakevol002", 404.98},
		{"Fairy Tail", "Fairy Tail 404 omakevol002", 404.98},
		{"Fairy Tail", "Fairy Tail 404.specialvol002", 404.97},
		{"Fairy Tail", "Fairy Tail 404 specialvol002", 404.97},
		{"One Piece", "One Piece 300,a", 300.1},
		{"One Piece", "One Piece Ch,123,extra", 123.99},
		{"One Piece", "One Piece the sunny, goes swimming 024,005", 24.005},
		{"Solo Leveling", "ch 122-a", 122.1},
		{"Solo Leveling", "Solo Leveling Ch.123-extra", 123.99},
		{"Solo Leveling", "Solo Leveling, 024-005", 24.005},
		{"Solo Leveling", "Ch.191-200 Read Online", 191.2},
		{"D.I.C.E", "D.I.C.E[Season 001] Ep. 007", 7.0},
		{"The Gamer", "S3 - Chapter 20", 20.0},
		{"One Outs", "One Outs 001", 1.0},
		{"The Sister of the Woods with a Thousand Young", "The 1st Night", 1.0},
		{"The Sister of the Woods with a Thousand Young", "The 2nd Night", 2.0},
		{"The Sister of the Woods with a Thousand Young", "The 3rd Night", 3.0},
		{"The Sister of the Woods with a Thousand Young", "The 4th Night", 4.0},
	}

	for _, tc := range cases {
		got := Parse(tc.manga, tc.chapter, nil)
		if got == nil {
			t.Errorf("Parse(%q, %q) = nil, want %v", tc.manga, tc.chapter, tc.want)
			continue
		}
		if math.Abs(*got-tc.want) > 1e-9 {
			t.Errorf("Parse(%q, %q) = %v, want %v", tc.manga, tc.chapter, *got, tc.want)
		}
	}
}

// A title with no recognizable number yields nil so callers keep the chapter
// unkeyed rather than guessing.
func TestParseWithoutNumber(t *testing.T) {
	if got := Parse("random", "Foo", nil); got != nil {
		t.Fatalf("Parse(\"Foo\") = %v, want nil", *got)
	}
}

// A declared number is authoritative: it is returned unchanged, including the
// sentinel that marks a deliberately unknown number, and a placeholder of -1 is
// treated as absent and derived instead.
func TestParseDeclaredNumber(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	if got := Parse("Bleach", "Bleach 567", number(12.5)); got == nil || *got != 12.5 {
		t.Fatalf("declared number = %v, want 12.5", got)
	}
	if got := Parse("Bleach", "Bleach 567", number(-2)); got == nil || *got != -2 {
		t.Fatalf("sentinel number = %v, want -2", got)
	}
	if got := Parse("Bleach", "Bleach 567", number(-1)); got == nil || *got != 567 {
		t.Fatalf("placeholder number = %v, want a derived 567", got)
	}
	if got := Parse("random", "Foo", number(-1)); got == nil || *got != -1 {
		t.Fatalf("placeholder without a title number = %v, want -1", got)
	}
}
