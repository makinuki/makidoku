package db

import (
	"testing"
	"time"
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

func TestMigrateMangaPreservesLocalStateAndDownloads(t *testing.T) {
	repo := testRepository(t)
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('asura','Asura','1',1,'en','https://asura.example','asura.wasm',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	old, err := repo.UpsertManga(Manga{SourceID: "mangadex", SourceMangaID: "old", Title: "Yosuga no Sora", Status: "ongoing", CoverURL: "old-cover"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetMangaLibrary(old.ID, true); err != nil {
		t.Fatal(err)
	}
	category, err := repo.CreateCategory("Reading", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaCategory(old.ID, category.ID, true); err != nil {
		t.Fatal(err)
	}
	number := 12.0
	oldChapter, err := repo.UpsertChapter(Chapter{MangaID: old.ID, SourceChapterID: "old-12", ChapterNumber: &number, Title: stringPtr("Chapter 12")})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(oldChapter.ID, `downloads\yosuga-12.cbz`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueChapter(oldChapter.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(ReadingProgress{MangaID: old.ID, LastReadChapterID: oldChapter.ID, LastReadPage: 4, TotalPages: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTrackerBinding(TrackerBinding{MangaID: old.ID, TrackerType: "anilist", RemoteID: "123", RemoteTitle: "Yosuga no Sora"}); err != nil {
		t.Fatal(err)
	}

	replacement := Manga{SourceID: "asura", SourceMangaID: "new", Title: old.Title, Status: old.Status, CoverURL: "new-cover"}
	newNumber := 12.0
	aggregate, err := repo.MigrateManga(old.ID, replacement, []Chapter{{SourceChapterID: "new-12", ChapterNumber: &newNumber, Title: stringPtr("Chapter 12")}})
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Manga.ID != "asura:new" || !aggregate.Manga.InLibrary {
		t.Fatalf("migrated manga = %+v", aggregate.Manga)
	}
	if len(aggregate.Categories) != 1 || len(aggregate.Trackers) != 1 || aggregate.Progress == nil {
		t.Fatalf("migrated state = %+v", aggregate)
	}
	if aggregate.Progress.LastReadChapterID != "asura:new:new-12" {
		t.Fatalf("progress chapter = %q", aggregate.Progress.LastReadChapterID)
	}
	chapter, err := repo.GetChapter("asura:new:new-12")
	if err != nil {
		t.Fatal(err)
	}
	if !chapter.Downloaded || chapter.DownloadPath == nil || *chapter.DownloadPath != `downloads\yosuga-12.cbz` {
		t.Fatalf("download artifact = %+v", chapter)
	}
	if _, err := repo.GetManga(old.ID); err == nil {
		t.Fatal("old manga still exists")
	}
}

func floatPtr(v float64) *float64 { return &v }
func stringPtr(v string) *string  { return &v }
