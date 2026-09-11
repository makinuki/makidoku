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
// release. Only chapters the source no longer offers are candidates, and each
// is claimed once so simultaneous releases of one number stay distinct.
func TestReplacementChapterIDs(t *testing.T) {
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
	adopted := replacementChapterIDs(before, incoming)
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

// Only one incoming chapter may claim a single orphaned record.
func TestReplacementChapterIDsClaimsEachRecordOnce(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	language := func(value string) *string { return &value }
	before := []db.Chapter{{ID: "ch-1", SourceChapterID: "old-1", ChapterNumber: number(3), Language: language("en")}}
	incoming := []engine.ChapterItem{
		{ID: "new-a", Number: number(3), Language: "en"},
		{ID: "new-b", Number: number(3), Language: "en"},
	}
	adopted := replacementChapterIDs(before, incoming)
	if len(adopted) != 1 {
		t.Fatalf("adopted = %+v, want exactly one claim", adopted)
	}
}
