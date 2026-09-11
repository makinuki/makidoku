package updater

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

func TestRunRecordsNewChaptersAndLastRun(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "updates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	chapterOne, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "chapter-1"})
	if err != nil {
		t.Fatal(err)
	}
	chapterTwo, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "chapter-2"})
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, func(context.Context, string) ([]string, error) { return []string{chapterOne.ID, chapterTwo.ID}, nil })
	count, err := service.Run(context.Background())
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	logs, err := repo.ListUpdateLogs(false)
	if err != nil || len(logs) != 2 || logs[0].MangaID != manga.ID {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	state, err := repo.GetLibraryUpdateState()
	if err != nil || state.LastRunAt == nil || state.LastStatus != "completed" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestRunContinuesAfterTitleFailure(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "updates-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	first, _ := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "a", Title: "A", Status: "ongoing", InLibrary: true})
	second, _ := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "b", Title: "B", Status: "ongoing", InLibrary: true})
	chapter, _ := repo.UpsertChapter(db.Chapter{MangaID: second.ID, SourceChapterID: "c"})
	service := New(repo, func(_ context.Context, mangaID string) ([]string, error) {
		if mangaID == first.ID {
			return nil, errors.New("offline")
		}
		return []string{chapter.ID}, nil
	})
	count, err := service.Run(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	state, _ := repo.GetLibraryUpdateState()
	if state.LastStatus != "completed_with_errors" {
		t.Fatalf("state=%+v", state)
	}
}

// The updater forwards every newly detected chapter to its enqueue hook; the
// automatic download policy is applied by the hook, not here.
func TestRunForwardsNewChaptersToEnqueue(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "updates-download.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','Source','1',1,'en','https://source.test','s.wasm',1)`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "M", Status: "ongoing", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	service := New(repo, func(context.Context, string) ([]string, error) { return []string{chapter.ID}, nil })
	var queued []string
	service.SetEnqueue(func(_ context.Context, gotManga string, chapterIDs []string) error {
		if gotManga != manga.ID {
			t.Fatalf("manga id = %s", gotManga)
		}
		queued = append(queued, chapterIDs...)
		return nil
	})
	if _, err := service.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0] != chapter.ID {
		t.Fatalf("queued = %v", queued)
	}
}
