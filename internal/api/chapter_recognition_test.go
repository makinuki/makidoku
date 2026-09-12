package api

import (
	"testing"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

// A source that re-issues an unnumbered chapter must still adopt the existing
// local record: the number is derived from the chapter title before matching,
// so the canonical id, reading state, and downloads stay on one row.
func TestAdoptedChapterIDsDerivesUnnumberedTitles(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	language := func(value string) *string { return &value }
	before := []db.Chapter{
		{ID: "ch-12", SourceChapterID: "old-12", ChapterNumber: number(12), Language: language("en")},
	}
	incoming := withDerivedChapterNumbers("Demo", []engine.ChapterItem{
		{ID: "new-12", Title: "Demo Vol.1 Ch.12: Arrival", Language: "en"},
	})
	if incoming[0].Number == nil || *incoming[0].Number != 12 {
		t.Fatalf("derived number = %v, want 12", incoming[0].Number)
	}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["new-12"] != "ch-12" {
		t.Fatalf("unnumbered re-issue was not adopted: %+v", adopted)
	}
}

// A declared number is authoritative and is never replaced by a derived one.
func TestWithDerivedChapterNumbersKeepsDeclared(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	incoming := withDerivedChapterNumbers("Demo", []engine.ChapterItem{
		{ID: "a", Title: "Demo Ch.99", Number: number(3)},
	})
	if incoming[0].Number == nil || *incoming[0].Number != 3 {
		t.Fatalf("declared number was replaced: %v", incoming[0].Number)
	}
}

// A chapter with neither a declared number nor a parseable title stays
// unnumbered, so unrelated specials are never merged.
func TestWithDerivedChapterNumbersLeavesUnparseable(t *testing.T) {
	incoming := withDerivedChapterNumbers("Demo", []engine.ChapterItem{{ID: "a", Title: "Bonus"}})
	if incoming[0].Number != nil {
		t.Fatalf("unparseable chapter gained number %v", *incoming[0].Number)
	}
}
