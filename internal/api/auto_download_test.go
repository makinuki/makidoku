package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

func testServer(t *testing.T) (*Server, *db.Repository) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','demo','Demo','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	return NewServer(repo, nil, newFakeDownloads()), repo
}

// Automatic downloads are gated on the per-title flag: a title that has not
// opted in must not queue anything, and an opted-in title must queue exactly
// the chapters it was handed.
func TestEnqueueNewChaptersHonorsTitleFlag(t *testing.T) {
	server, repo := testServer(t)
	downloads := server.downloads.(*fakeDownloads)
	off, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "off", Title: "Off", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	on, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "on", Title: "On", Status: "ongoing", DownloadNewChapters: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := server.EnqueueNewChapters(context.Background(), off.ID, []string{"chapter-1"}); err != nil {
		t.Fatal(err)
	}
	if downloads.mangaID != "" {
		t.Fatalf("disabled title queued %q", downloads.mangaID)
	}

	if err := server.EnqueueNewChapters(context.Background(), on.ID, []string{"chapter-1", "chapter-2"}); err != nil {
		t.Fatal(err)
	}
	if downloads.mangaID != on.ID || len(downloads.selection.IDs) != 2 {
		t.Fatalf("enabled title queued manga=%q selection=%+v", downloads.mangaID, downloads.selection)
	}

	downloads.mangaID = ""
	if err := server.EnqueueNewChapters(context.Background(), on.ID, nil); err != nil {
		t.Fatal(err)
	}
	if downloads.mangaID != "" {
		t.Fatalf("empty chapter list queued %q", downloads.mangaID)
	}
}

// A source that re-issues a chapter under a new identifier must update the
// existing local chapter rather than fork a duplicate or report a new
// release. Matching runs a ladder of decreasing confidence and pairs only
// one-to-one candidates.
func TestAdoptedChapterIDs(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	language := func(value string) *string { return &value }
	before := []db.Chapter{
		{ID: "ch-1", SourceChapterID: "old-1", ChapterNumber: number(1), Language: language("en")},
		{ID: "ch-2", SourceChapterID: "old-2", ChapterNumber: number(2), Language: language("en")},
		{ID: "ch-3", SourceChapterID: "old-3", ChapterNumber: nil, Language: language("en")},
		{ID: "ch-4", SourceChapterID: "old-4", ChapterNumber: number(4), Language: language("pt-br")},
	}
	incoming := []engine.ChapterItem{
		{ID: "new-1", Number: number(1), Language: "en"},
		{ID: "new-2", Number: number(2), Language: "en"},
		{ID: "old-2", Number: number(2), Language: "en"},
		{ID: "new-3", Number: nil, Language: "en"},
		{ID: "new-4", Number: number(4), Language: "en"},
		{ID: "new-5", Number: number(5), Language: "en"},
	}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["new-1"] != "ch-1" {
		t.Fatalf("re-issued chapter was not adopted: %+v", adopted)
	}
	if _, ok := adopted["new-2"]; ok {
		t.Fatalf("chapter still offered by the source was adopted: %+v", adopted)
	}
	if _, ok := adopted["new-3"]; ok {
		t.Fatalf("unnumbered chapter was adopted: %+v", adopted)
	}
	if _, ok := adopted["new-4"]; ok {
		t.Fatalf("chapter with a different language was adopted: %+v", adopted)
	}
	if _, ok := adopted["new-5"]; ok {
		t.Fatalf("new chapter number was adopted: %+v", adopted)
	}
	if len(adopted) != 1 {
		t.Fatalf("adopted = %+v, want one entry", adopted)
	}
}

// A stored locator that equals the page URL the source now publishes names the
// same chapter, so the row is adopted and its locator rewritten.
func TestAdoptedChapterIDsUsesPublishedPageURL(t *testing.T) {
	before := []db.Chapter{{ID: "ch-9", SourceChapterID: "1?style=pages"}}
	incoming := []engine.ChapterItem{{ID: "svc-9", URL: "1?style=pages", Title: "Chapter 9"}}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["svc-9"] != "ch-9" {
		t.Fatalf("page URL did not adopt the stored row: %+v", adopted)
	}
}

// An unnumbered extra or prologue carries only a title. The title alone is
// enough to keep the reading state on the row the source re-issued.
func TestAdoptedChapterIDsAdoptsUnnumberedTitle(t *testing.T) {
	title := "Prologue"
	before := []db.Chapter{{ID: "ch-p", SourceChapterID: "old-prologue", Title: &title}}
	incoming := []engine.ChapterItem{{ID: "new-prologue", Title: "Prologue"}}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["new-prologue"] != "ch-p" {
		t.Fatalf("unnumbered title was not adopted: %+v", adopted)
	}
}

// Titles compare across case and whitespace, so a cosmetic re-issue still
// lands on the stored row.
func TestAdoptedChapterIDsNormalizesTitle(t *testing.T) {
	title := "  Bonus  Story "
	before := []db.Chapter{{ID: "ch-b", SourceChapterID: "old-bonus", Title: &title}}
	incoming := []engine.ChapterItem{{ID: "new-bonus", Title: "bonus story"}}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["new-bonus"] != "ch-b" {
		t.Fatalf("normalized title was not adopted: %+v", adopted)
	}
}

// Only one incoming chapter may claim a single orphaned record. An ambiguous
// key is skipped, because reading state on the wrong chapter is worse than a
// chapter left unlinked.
func TestAdoptedChapterIDsSkipsAmbiguousKey(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	language := func(value string) *string { return &value }
	before := []db.Chapter{{ID: "ch-1", SourceChapterID: "old-1", ChapterNumber: number(3), Language: language("en")}}
	incoming := []engine.ChapterItem{
		{ID: "new-a", Number: number(3), Language: "en"},
		{ID: "new-b", Number: number(3), Language: "en"},
	}
	adopted := adoptedChapterIDs(before, incoming)
	if len(adopted) != 0 {
		t.Fatalf("ambiguous key produced %+v, want no claim", adopted)
	}
}

// A pinned chapter id that the source still publishes claims its own row and
// is not re-pointed at another candidate.
func TestAdoptedChapterIDsKeepsPinnedID(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	before := []db.Chapter{{ID: "ch-1", SourceChapterID: "stable-id", ChapterNumber: number(7)}}
	incoming := []engine.ChapterItem{{ID: "stable-id", Number: number(7)}}
	adopted := adoptedChapterIDs(before, incoming)
	if len(adopted) != 0 {
		t.Fatalf("exact id match produced %+v, want no re-point", adopted)
	}
}

// A backup records no chapter language, so a stored row can only be paired
// with a language-carrying source chapter on the number and the release group.
// Two groups publishing the same number and title must still stay apart.
func TestAdoptedChapterIDsPairsByNumberAndScanlator(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	scanlator := func(value string) *string { return &value }
	title := "Chapter 393"
	before := []db.Chapter{
		{ID: "ch-qi", SourceChapterID: `{"id":"40301","source":"user"}`, ChapterNumber: number(393), Title: &title, Scanlator: scanlator("QI Scans")},
		{ID: "ch-hive", SourceChapterID: `{"id":"40302","source":"user"}`, ChapterNumber: number(393), Title: &title, Scanlator: scanlator("HiveToons")},
	}
	incoming := []engine.ChapterItem{
		{ID: "user:40301", Number: number(393), Title: title, Scanlator: "QI Scans", Language: "en"},
		{ID: "user:40302", Number: number(393), Title: title, Scanlator: "HiveToons", Language: "en"},
	}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["user:40301"] != "ch-qi" || adopted["user:40302"] != "ch-hive" {
		t.Fatalf("number and scanlator did not pair the rows: %+v", adopted)
	}
}

// A number on its own is never an identity. Rows that share it but were
// published by different groups are distinct chapters and must not merge.
func TestAdoptedChapterIDsKeepsScanlatorsApart(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	scanlator := func(value string) *string { return &value }
	before := []db.Chapter{
		{ID: "ch-blank", SourceChapterID: "old-blank", ChapterNumber: number(1), Scanlator: scanlator("\u200b")},
		{ID: "ch-flame", SourceChapterID: "old-flame", ChapterNumber: number(1), Scanlator: scanlator("Flame Comics")},
	}
	incoming := []engine.ChapterItem{
		{ID: "new-blank", Number: number(1), Scanlator: "\u200b", Language: "en"},
		{ID: "new-flame", Number: number(1), Scanlator: "Flame Comics", Language: "en"},
	}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted["new-blank"] != "ch-blank" || adopted["new-flame"] != "ch-flame" || len(adopted) != 2 {
		t.Fatalf("distinct release groups were not kept apart: %+v", adopted)
	}
}

// A backup records the site path, so the stored locator can end in the
// identifier the source publishes even though neither the locator nor the page
// URL is that identifier.
func TestAdoptedChapterIDsUsesTrailingIdentifier(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	id := "006f8bc3-0062-4c74-8d7b-f8d836603c13"
	before := []db.Chapter{{ID: "ch-8", SourceChapterID: "/chapter/" + id, ChapterNumber: number(8)}}
	incoming := []engine.ChapterItem{{
		ID:       id,
		URL:      "https://mangadex.org/chapter/" + id,
		Number:   number(8),
		Language: "en",
	}}
	adopted := adoptedChapterIDs(before, incoming)
	if adopted[id] != "ch-8" {
		t.Fatalf("trailing identifier did not pair the rows: %+v", adopted)
	}
}

// The trailing token is the last path segment, and a bare number is the
// chapter number rather than an identifier, so it is never a token.
func TestIdentifierToken(t *testing.T) {
	cases := map[string]string{
		"/chapter/006f8bc3-0062-4c74-8d7b-f8d836603c13": "006f8bc3-0062-4c74-8d7b-f8d836603c13",
		"/chapters/01J8Z9V4N6H7Q3K2M5P0R1S8TX":          "01J8Z9V4N6H7Q3K2M5P0R1S8TX",
		"/series/example/chapter/1":                     "",
		"/read/gist/abc/212/0":                          "",
		"plain-id":                                      "plain-id",
		"12":                                            "",
		"":                                              "",
	}
	for input, want := range cases {
		got, ok := identifierToken(input)
		if want == "" && ok {
			t.Fatalf("identifierToken(%q) = %q, want no token", input, got)
		}
		if want != "" && (!ok || got != want) {
			t.Fatalf("identifierToken(%q) = %q (%v), want %q", input, got, ok, want)
		}
	}
}
