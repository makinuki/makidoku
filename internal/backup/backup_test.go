package backup

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

func TestExportImportRestoresLibraryGraph(t *testing.T) {
	first, err := db.Open(filepath.Join(t.TempDir(), "one.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(first)
	if _, err := first.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('mangadex','MangaDex','1',1,'en','https://mangadex.org','mangadex.wasm',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "mangadex", SourceMangaID: "backup", Title: "Backup", Status: "ongoing", CoverURL: "cover", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	category, err := repo.CreateCategory("Reading", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMangaCategory(manga.ID, category.ID, true); err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "1", ChapterNumber: floatPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(db.ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 1, TotalPages: 2}); err != nil {
		t.Fatal(err)
	}
	payload, err := Export(first)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second, err := db.Open(filepath.Join(t.TempDir(), "two.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := Import(second, payload); err != nil {
		t.Fatal(err)
	}
	got, err := db.NewRepository(second).GetMangaAggregate(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Manga.InLibrary || len(got.Categories) != 1 || len(got.Chapters) != 1 || got.Progress == nil {
		t.Fatalf("restored aggregate = %#v", got)
	}
	var document Document
	if err := json.Unmarshal(payload, &document); err != nil || len(document.MangaCategories) != 1 {
		t.Fatalf("export memberships = %#v, err = %v", document.MangaCategories, err)
	}
}

func floatPtr(v float64) *float64 { return &v }
