package engine

import (
	"encoding/json"
	"testing"
)

func TestSelectCoverPrefersLargestWidthNotExceedingTarget(t *testing.T) {
	variants := []CoverVariant{
		{URL: "https://covers.test/a.256.jpg", Width: ptr(256)},
		{URL: "https://covers.test/a.512.jpg", Width: ptr(512)},
	}
	got := SelectCover("https://covers.test/a.jpg", variants, PreferredCoverWidth)
	if got != "https://covers.test/a.512.jpg" {
		t.Fatalf("SelectCover target 512 of [256 512] = %q, want the 512 variant", got)
	}

	got = SelectCover("https://covers.test/a.jpg", variants, 300)
	if got != "https://covers.test/a.256.jpg" {
		t.Fatalf("SelectCover target 300 of [256 512] = %q, want the 256 variant", got)
	}
}

func TestSelectCoverFallsBackToSmallestVariantAboveTarget(t *testing.T) {
	variants := []CoverVariant{
		{URL: "https://covers.test/a.1024.jpg", Width: ptr(1024)},
		{URL: "https://covers.test/a.2048.jpg", Width: ptr(2048)},
	}
	got := SelectCover("https://covers.test/a.jpg", variants, 512)
	if got != "https://covers.test/a.1024.jpg" {
		t.Fatalf("SelectCover target 512 of [1024 2048] = %q, want the 1024 variant", got)
	}
}

func TestSelectCoverUsesCanonicalWhenNoVariants(t *testing.T) {
	if got := SelectCover("https://covers.test/a.jpg", nil, 512); got != "https://covers.test/a.jpg" {
		t.Fatalf("SelectCover without variants = %q, want the canonical URL", got)
	}
}

func TestSelectCoverIgnoresUndeclaredWidthsWhenDeclaredExist(t *testing.T) {
	variants := []CoverVariant{
		{URL: "https://covers.test/a.png"},
		{URL: "https://covers.test/a.512.jpg", Width: ptr(512)},
	}
	if got := SelectCover("", variants, 512); got != "https://covers.test/a.512.jpg" {
		t.Fatalf("SelectCover mixed variants = %q, want the declared 512 variant", got)
	}
}

func TestSelectCoverWithoutDeclaredWidthsKeepsSourceOrder(t *testing.T) {
	variants := []CoverVariant{
		{URL: "https://covers.test/a.png"},
		{URL: "https://covers.test/b.png"},
	}
	if got := SelectCover("", variants, 512); got != "https://covers.test/a.png" {
		t.Fatalf("SelectCover undeclared variants = %q, want the first entry", got)
	}
}

func TestSelectCoverReturnsCanonicalForEmptyInput(t *testing.T) {
	if got := SelectCover("", nil, 512); got != "" {
		t.Fatalf("SelectCover without any candidate = %q, want an empty string", got)
	}
}

func ptr(v int) *int { return &v }

func TestMangaPayloadsDecodeOptionalCoversAndVolume(t *testing.T) {
	itemPayload := `{
		"id": "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		"title": "Yosuga no Sora",
		"coverUrl": "https://uploads.test/covers/x/original.jpg",
		"covers": [
			{"url": "https://uploads.test/covers/x/original.jpg.256.jpg", "width": 256},
			{"url": "https://uploads.test/covers/x/original.jpg.512.jpg", "width": 512}
		]
	}`
	var item MangaItem
	if err := json.Unmarshal([]byte(itemPayload), &item); err != nil {
		t.Fatalf("decode MangaItem: %v", err)
	}
	if item.CoverURL != "https://uploads.test/covers/x/original.jpg" {
		t.Fatalf("coverUrl = %q", item.CoverURL)
	}
	if len(item.Covers) != 2 || item.Covers[0].Width == nil || *item.Covers[0].Width != 256 {
		t.Fatalf("covers = %+v", item.Covers)
	}
	if item.Covers[1].URL != "https://uploads.test/covers/x/original.jpg.512.jpg" {
		t.Fatalf("second variant = %+v", item.Covers[1])
	}

	chapterPayload := `{"id": "chapter-slug", "number": 10.5, "volume": 3, "language": "en"}`
	var chapter ChapterItem
	if err := json.Unmarshal([]byte(chapterPayload), &chapter); err != nil {
		t.Fatalf("decode ChapterItem: %v", err)
	}
	if chapter.Volume == nil || *chapter.Volume != 3 {
		t.Fatalf("volume = %+v, want 3", chapter.Volume)
	}

	var absent ChapterItem
	if err := json.Unmarshal([]byte(`{"id": "oneshot", "number": null}`), &absent); err != nil {
		t.Fatalf("decode ChapterItem without volume: %v", err)
	}
	if absent.Volume != nil {
		t.Fatalf("volume = %+v, want absent", absent.Volume)
	}
}
