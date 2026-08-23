package db

import (
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
