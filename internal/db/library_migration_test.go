package db

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLibraryRepositoryPersistsMembershipCategoriesAndAggregate(t *testing.T) {
	repo := testRepository(t)

	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "yosuga", Title: "Yosuga no Sora", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetMangaLibrary(manga.ID, true); err != nil {
		t.Fatal(err)
	}
	category, err := repo.CreateCategory("Reading", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaCategory(manga.ID, category.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "45", ChapterNumber: floatPtr(45), Title: stringPtr("Finale")}); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListLibrary("yosuga", category.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != manga.ID {
		t.Fatalf("library = %#v", items)
	}
	aggregate, err := repo.GetMangaAggregate(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregate.Chapters) != 1 || len(aggregate.Categories) != 1 || !aggregate.Manga.InLibrary {
		t.Fatalf("aggregate = %#v", aggregate)
	}
}

func TestGetMangaAggregateMarshalsEmptyCollectionsAsArrays(t *testing.T) {
	repo := testRepository(t)

	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "bare", Title: "Bare", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := repo.GetMangaAggregate(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Nil slices marshal as null; the web client expects arrays.
	if aggregate.Chapters == nil || aggregate.Categories == nil || aggregate.Trackers == nil {
		t.Fatalf("nil collection in aggregate: chapters=%v categories=%v trackers=%v", aggregate.Chapters == nil, aggregate.Categories == nil, aggregate.Trackers == nil)
	}
	encoded, err := json.Marshal(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"chapters":[]`, `"categories":[]`, `"trackers":[]`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("aggregate JSON missing %s: %s", field, encoded)
		}
	}
}

func TestLibraryRepositoryListsReadingHistory(t *testing.T) {
	repo := testRepository(t)

	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "history", Title: "History", Status: "ongoing", CoverURL: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "1", ChapterNumber: floatPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 2, TotalPages: 4}); err != nil {
		t.Fatal(err)
	}
	history, err := repo.ListHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Manga.ID != manga.ID {
		t.Fatalf("history = %#v", history)
	}
}

func floatPtr(v float64) *float64 { return &v }
func stringPtr(v string) *string  { return &v }

// Earlier builds appended a history row per page turn. The migration keeps
// the newest row of each manga and chapter pair.
func TestHistoryCoalesceMigrationKeepsNewest(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "coalesce", Title: "Coalesce", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceChapterID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	seed := []struct {
		id        string
		chapterID any
		page      int
		at        int64
	}{
		{"coalesce-1", chapter.ID, 61, 100},
		{"coalesce-2", chapter.ID, 62, 200},
		{"coalesce-3", chapter.ID, 61, 150},
		{"coalesce-4", nil, 0, 50},
		{"coalesce-5", nil, 0, 75},
	}
	for _, row := range seed {
		if _, err := repo.db.Exec(`INSERT INTO history_events(id,manga_id,chapter_id,page,occurred_at) VALUES(?,?,?,?,?)`, row.id, manga.ID, row.chapterID, row.page, row.at); err != nil {
			t.Fatal(err)
		}
	}
	const name = "migrations/000019_history_coalesce.up.sql"
	if _, err := repo.db.Exec(`DELETE FROM _migrations WHERE name=?`, name); err != nil {
		t.Fatal(err)
	}
	if err := migrate(repo.db); err != nil {
		t.Fatal(err)
	}
	events, err := repo.ListHistoryEvents(10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	for _, event := range events {
		switch {
		case event.ChapterID != nil && *event.ChapterID == chapter.ID:
			if event.Page == nil || *event.Page != 62 {
				t.Fatalf("chapter event = %+v", event)
			}
		case event.ChapterID == nil:
			if event.OccurredAt != 75 {
				t.Fatalf("title event = %+v", event)
			}
		default:
			t.Fatalf("unexpected event = %+v", event)
		}
	}
}

// A refresh used to store the source upload time in milliseconds while every
// other timestamp is in seconds. The migration folds values that can only be
// milliseconds and leaves a seconds value alone.
func TestChapterUploadedAtMigrationFoldsMilliseconds(t *testing.T) {
	repo := testRepository(t)
	manga, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "timestamps", Title: "Timestamps", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	milliseconds := int64(1_700_000_000_000)
	seconds := int64(1_600_000_000)
	if _, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceID: "mangadex", SourceChapterID: "ms", ChapterNumber: &number, UploadedAt: &milliseconds}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChapter(Chapter{MangaID: manga.ID, SourceID: "mangadex", SourceChapterID: "s", ChapterNumber: &number, UploadedAt: &seconds}); err != nil {
		t.Fatal(err)
	}
	const name = "migrations/000017_chapter_uploaded_at_seconds.up.sql"
	if _, err := repo.db.Exec(`DELETE FROM _migrations WHERE name=?`, name); err != nil {
		t.Fatal(err)
	}
	if err := migrate(repo.db); err != nil {
		t.Fatal(err)
	}
	var folded, untouched int64
	if err := repo.db.Get(&folded, `SELECT c.uploaded_at FROM chapters c JOIN chapter_sources cs ON cs.chapter_id=c.id WHERE cs.source_chapter_id='ms'`); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.Get(&untouched, `SELECT c.uploaded_at FROM chapters c JOIN chapter_sources cs ON cs.chapter_id=c.id WHERE cs.source_chapter_id='s'`); err != nil {
		t.Fatal(err)
	}
	if folded != milliseconds/1000 {
		t.Fatalf("millisecond value = %d, want %d", folded, milliseconds/1000)
	}
	if untouched != seconds {
		t.Fatalf("seconds value = %d, want %d", untouched, seconds)
	}
}
