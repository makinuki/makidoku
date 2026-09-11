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
	if _, err := first.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('mangadex','mangadex','MangaDex','1',1,'en','https://mangadex.org','mangadex.wasm',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	series := "https://mangadex.org/title/uuid/backup"
	manga, err := repo.UpsertManga(db.Manga{SourceID: "mangadex", SourceMangaID: "backup", SourcePageURL: series, Title: "Backup", Status: "ongoing", CoverURL: "cover", InLibrary: true})
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
	link, err := db.NewRepository(second).GetMangaSource(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if link.SourceID != "mangadex" || link.SourceMangaID != "backup" || link.PluginKey == "" || link.URL != series {
		t.Fatalf("restored manga source = %+v", link)
	}
	restoredChapter := got.Chapters[0]
	if restoredChapter.SourceID != "mangadex" || restoredChapter.SourceChapterID != "1" {
		t.Fatalf("restored chapter resolution = %+v", restoredChapter)
	}
	var document Document
	if err := json.Unmarshal(payload, &document); err != nil || len(document.MangaCategories) != 1 {
		t.Fatalf("export memberships = %#v, err = %v", document.MangaCategories, err)
	}
	if len(document.MangaSources) != 1 || len(document.ChapterSources) != 1 {
		t.Fatalf("export source links: mangaSources=%#v chapterSources=%#v", document.MangaSources, document.ChapterSources)
	}
}

func floatPtr(v float64) *float64 { return &v }

// Restoring a backup must not resurrect uninstalled plugins or re-point
// healthy local installs at artifact paths recorded on another machine.
// Source metadata still follows the document; install state stays local.
func TestImportKeepsLocalSourceInstallState(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "dest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	now := time.Now().Unix()
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at,installed) VALUES('local','local','Local','1',1,'en','https://local.test','local-local.wasm',?,1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at,installed) VALUES('gone','gone','Gone','1',1,'en','https://gone.test',NULL,?,0)`, now); err != nil {
		t.Fatal(err)
	}

	document := map[string]any{
		"version": float64(1),
		"sources": []any{
			map[string]any{"id": "local", "plugin_key": "local", "name": "Renamed", "version": "9", "abi_version": float64(1), "lang": "en", "base_url": "https://other.test", "icon_url": nil, "wasm_path": "/other/machine/local.wasm", "installed_at": float64(now)},
			map[string]any{"id": "gone", "plugin_key": "gone", "name": "Gone", "version": "2", "abi_version": float64(1), "lang": "en", "base_url": "https://gone.test", "icon_url": nil, "wasm_path": "/other/machine/gone.wasm", "installed_at": float64(now)},
			map[string]any{"id": "fresh", "plugin_key": "fresh", "name": "Fresh", "version": "1", "abi_version": float64(1), "lang": "en", "base_url": "https://fresh.test", "icon_url": nil, "wasm_path": "/other/machine/fresh.wasm", "installed_at": float64(now)},
		},
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := Import(handle, payload); err != nil {
		t.Fatal(err)
	}

	var name, wasmPath string
	var installed int
	if err := handle.Get(&name, `SELECT name FROM sources WHERE id='local'`); err != nil {
		t.Fatal(err)
	}
	if name != "Renamed" {
		t.Fatalf("source metadata not updated: name = %q", name)
	}
	if err := handle.Get(&wasmPath, `SELECT wasm_path FROM sources WHERE id='local'`); err != nil {
		t.Fatal(err)
	}
	if wasmPath != "local-local.wasm" {
		t.Fatalf("local install re-pointed at %q", wasmPath)
	}
	if err := handle.Get(&installed, `SELECT installed FROM sources WHERE id='gone'`); err != nil {
		t.Fatal(err)
	}
	if installed != 0 {
		t.Fatal("import resurrected an uninstalled source")
	}
	if err := handle.Get(&installed, `SELECT installed FROM sources WHERE id='fresh'`); err != nil {
		t.Fatal(err)
	}
	if installed != 0 {
		t.Fatal("unknown backup source imported as installed")
	}
	if err := handle.Get(&wasmPath, `SELECT COALESCE(wasm_path,'') FROM sources WHERE id='fresh'`); err != nil {
		t.Fatal(err)
	}
	if wasmPath != "" {
		t.Fatalf("unknown source imported with wasm path %q", wasmPath)
	}
}

func TestExportImportPreservesRuntimeState(t *testing.T) {
	first, err := db.Open(filepath.Join(t.TempDir(), "one.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(first)
	if _, err := first.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES('s','s','Source','1',1,'en','https://source.test','s.wasm',?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manga, err := repo.UpsertManga(db.Manga{SourceID: "s", SourceMangaID: "m", Title: "State", Status: "ongoing", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceChapterID: "c", ChapterNumber: floatPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	started, finished := int64(1704153600), int64(1706832000)
	binding, err := repo.UpsertTrackerBinding(db.TrackerBinding{MangaID: manga.ID, TrackerType: "anilist", RemoteID: "1", RemoteTitle: "State"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateTrackerTracking(manga.ID, binding.TrackerType, floatPtr(8.5), &started, &finished); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetChapterRead(chapter.ID, manga.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(db.ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 2, TotalPages: 2, SessionSeconds: 37, IsCompleted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordUpdate(manga.ID, chapter.ID, 1707000000); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetLibraryUpdateState("completed", 1707000001); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSetting("appearance.date_format", `"absolute"`); err != nil {
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
	secondRepo := db.NewRepository(second)
	gotBinding, err := secondRepo.GetTrackerBinding(manga.ID, "anilist")
	if err != nil || gotBinding.RemoteScore == nil || *gotBinding.RemoteScore != 8.5 || gotBinding.StartedAt == nil || *gotBinding.StartedAt != started || gotBinding.FinishedAt == nil || *gotBinding.FinishedAt != finished {
		t.Fatalf("binding = %+v, err=%v", gotBinding, err)
	}
	state, err := secondRepo.GetChapterRead(chapter.ID)
	if err != nil || !state.Read {
		t.Fatalf("read state = %+v, err=%v", state, err)
	}
	seconds, err := secondRepo.ReadingSeconds(manga.ID)
	if err != nil || seconds != 37 {
		t.Fatalf("reading seconds = %d, err=%v", seconds, err)
	}
	setting, err := secondRepo.GetSetting("appearance.date_format")
	if err != nil || setting.Value != `"absolute"` {
		t.Fatalf("setting = %+v, err=%v", setting, err)
	}
	updates, err := secondRepo.ListUpdateLogs(true)
	if err != nil || len(updates) != 1 {
		t.Fatalf("updates = %+v, err=%v", updates, err)
	}
	updateState, err := secondRepo.GetLibraryUpdateState()
	if err != nil || updateState.LastStatus != "completed" {
		t.Fatalf("update state = %+v, err=%v", updateState, err)
	}
}
