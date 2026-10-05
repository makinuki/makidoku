package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
	"github.com/makinuki/makidoku/internal/identity"
	"github.com/makinuki/makidoku/internal/settings"
)

// A placeholder source created by a restore is not served by the engine, so
// the migration picker lists it from the store to let its titles move onto an
// installed source.
func TestMigrationSourcesIncludeImported(t *testing.T) {
	repo, router, _, _ := migrationTestRouter(t)
	importedID := "imported-999"
	if _, err := repo.DB().Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed,installed_at)
		VALUES(?,NULL,?,?,?,?,?,NULL,0,?)`, importedID, "Other Site (imported)", "0", 1, "en", "", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertManga(db.Manga{SourceID: importedID, SourceMangaID: "remote-x", Title: "Imported Title", Status: "unknown", InLibrary: true}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/migration/sources", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var entries []struct {
		Source struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"source"`
		Count    int  `json:"count"`
		Imported bool `json:"imported"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Source.ID == importedID {
			if !entry.Imported || entry.Count != 1 {
				t.Fatalf("imported entry = %+v", entry)
			}
			return
		}
	}
	t.Fatalf("imported source missing from %+v", entries)
}

// migrationTestRouter wires two sources so a title can migrate between them.
func migrationTestRouter(t *testing.T) (*db.Repository, chi.Router, string, string) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	oldSource, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	newSource, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?)`, oldSource, "old", "Old", 1, 1, "en", "https://old.test", "missing-old.wasm", now); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?)`, newSource, "replacement", "Replacement", 1, 1, "en", "https://replacement.test", "missing-replacement.wasm", now); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(handle)
	router := chi.NewRouter()
	NewServer(repo, engine.New(handle, engine.Options{DataDir: t.TempDir()})).Mount(router)
	return repo, router, oldSource, newSource
}

// Applying a migration must retire the previous source's chapters (including
// their download artifacts) and leave the title ready for the replacement
// source. The replacement fetch fails on the missing demo binary, so the
// chapter list ends up empty but consistent, and dangling progress is dropped.
func TestApplyMigrationRetiresOldChapters(t *testing.T) {
	repo, router, oldSource, newSource := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: oldSource, SourceChapterID: "chapter-a", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "artifact", "Chapter 1.cbz")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("pages"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapter.ID, artifact); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertReadingProgress(db.ReadingProgress{MangaID: manga.ID, LastReadChapterID: chapter.ID, LastReadPage: 2, TotalPages: 10, LastReadAt: 123}); err != nil {
		t.Fatal(err)
	}
	discovered, err := repo.UpsertManga(db.Manga{SourceID: newSource, SourceMangaID: "remote-b", Title: "Demo", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(map[string]string{"sourceId": newSource, "mangaId": discovered.ID})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/migration/apply", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manga struct {
			Manga      db.Manga `json:"manga"`
			SourceName string   `json:"sourceName"`
		} `json:"manga"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The response must present the replacement plugin, not the retired one.
	if payload.Source != newSource || payload.Manga.Manga.SourceID != newSource || payload.Manga.SourceName != "Replacement" {
		t.Fatalf("payload source = %q/%q/%q, want the replacement", payload.Source, payload.Manga.Manga.SourceID, payload.Manga.SourceName)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("artifact still on disk: %v", err)
	}
	chapters, err := repo.ListChapters(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 0 {
		t.Fatalf("chapters after migration = %d, want the old list retired", len(chapters))
	}
	if _, err := repo.GetReadingProgress(manga.ID); err == nil {
		t.Fatal("dangling reading progress survived migration")
	}
	source, err := repo.GetMangaSource(manga.ID)
	if err != nil || source.SourceID != newSource {
		t.Fatalf("primary source = %+v, err = %v", source, err)
	}
}

// The claimed source must match the replacement manga's actual source.
func TestApplyMigrationRejectsSourceMismatch(t *testing.T) {
	repo, router, oldSource, newSource := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := repo.UpsertManga(db.Manga{SourceID: newSource, SourceMangaID: "remote-b", Title: "Demo", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"sourceId": oldSource, "mangaId": discovered.ID})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/migration/apply", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want a bad request for a source mismatch", rec.Code)
	}
}

// Migrating within the same plugin would merge duplicate chapter sets while
// deleting nothing, so it is rejected outright.
func TestApplyMigrationRejectsSameSource(t *testing.T) {
	repo, router, oldSource, _ := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-b", Title: "Demo", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"sourceId": oldSource, "mangaId": discovered.ID})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/migration/apply", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want a bad request for a same-source replacement", rec.Code, rec.Body.String())
	}
}

// A replacement that is itself in the library would survive the attach step
// as an empty ghost entry, so the migration refuses until it is removed.
func TestApplyMigrationRejectsInLibraryReplacement(t *testing.T) {
	repo, router, oldSource, newSource := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := repo.UpsertManga(db.Manga{SourceID: newSource, SourceMangaID: "remote-b", Title: "Demo", Status: "unknown", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	if _, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: oldSource, SourceChapterID: "chapter-a", ChapterNumber: &number}); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"sourceId": newSource, "mangaId": discovered.ID})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/migration/apply", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want a bad request for an in-library replacement", rec.Code, rec.Body.String())
	}
	chapters, err := repo.ListChapters(manga.ID)
	if err != nil || len(chapters) != 1 {
		t.Fatalf("chapters = %+v, err = %v, want the original list untouched", chapters, err)
	}
}

// The migration preferences come from the settings store, and an explicit
// request value overrides them.
func TestMigrationFlagsResolveFromSettings(t *testing.T) {
	repo, _, _, _ := migrationTestRouter(t)
	server := NewServer(repo, nil)
	service := settings.New(repo)
	server.SetSettings(service)

	if flags := server.migrationFlags(); !flags.has(migrationFlagChapter) || !flags.has(migrationFlagRemoveDownload) {
		t.Fatalf("default flags = %d, want both enabled", flags)
	}
	if err := service.Set("migration.carry_read_state", "false"); err != nil {
		t.Fatal(err)
	}
	if err := service.Set("migration.remove_downloads", "false"); err != nil {
		t.Fatal(err)
	}
	if flags := server.migrationFlags(); flags != 0 {
		t.Fatalf("flags after disabling both = %d, want 0", flags)
	}
	if !migrationFlag(0).valid() || !migrationFlagDefault.valid() {
		t.Fatal("known flag masks should be valid")
	}
	if migrationFlag(1 << 4).valid() {
		t.Fatal("an unknown flag bit should be rejected")
	}
}

// Read state carries as a boundary: the replacement chapters at or below the
// highest number read become read, and the progress pointer follows the
// highest of them. Unnumbered chapters never qualify.
func TestHighestAtOrBelowPicksCarryBoundary(t *testing.T) {
	one, three, seven := 1.0, 3.0, 7.0
	chapters := []db.Chapter{
		{ID: "c1", ChapterNumber: &one},
		{ID: "c3", ChapterNumber: &three},
		{ID: "c7", ChapterNumber: &seven},
		{ID: "extra"},
	}
	boundary := highestAtOrBelow(chapters, 5)
	if boundary == nil || boundary.ID != "c3" {
		t.Fatalf("boundary = %+v, want c3", boundary)
	}
	if !boundaryIsLast(chapters, &chapters[2]) {
		t.Fatal("c7 should be the last numbered chapter")
	}
	if boundaryIsLast(chapters, boundary) {
		t.Fatal("c3 should not be the last numbered chapter")
	}
	if highestAtOrBelow(chapters, 0) != nil {
		t.Fatal("no chapter qualifies below the lowest number")
	}
}

// The read-state boundary and bookmarks are read and written through the
// repository: everything at or below the boundary becomes read, and the
// chapter above it stays untouched.
func TestMigrationReadStateCarryInRepository(t *testing.T) {
	repo, _, oldSource, _ := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	var chapters []db.Chapter
	for _, number := range []float64{1, 2, 3, 4} {
		value := number
		chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: oldSource, SourceChapterID: fmt.Sprintf("chapter-%v", number), ChapterNumber: &value})
		if err != nil {
			t.Fatal(err)
		}
		chapters = append(chapters, chapter)
	}
	if err := repo.SetChapterRead(chapters[1].ID, manga.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetChapterBookmark(chapters[2].ID, true); err != nil {
		t.Fatal(err)
	}

	highest, err := repo.HighestReadChapterNumber(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if highest == nil || *highest != 2 {
		t.Fatalf("highest read = %v, want 2", highest)
	}
	bookmarks, err := repo.ListBookmarkedChapterNumbers(manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bookmarks) != 1 || bookmarks[0] != 3 {
		t.Fatalf("bookmarks = %v, want [3]", bookmarks)
	}

	if _, err := repo.MarkChaptersReadThrough(manga.ID, 3); err != nil {
		t.Fatal(err)
	}
	var readThrough int
	if err := repo.DB().QueryRow(`SELECT COUNT(*) FROM chapter_read_state rs
		JOIN chapters c ON c.id=rs.chapter_id
		WHERE rs.manga_id=? AND rs.read=1 AND c.chapter_number<=3`, manga.ID).Scan(&readThrough); err != nil {
		t.Fatal(err)
	}
	if readThrough != 3 {
		t.Fatalf("read chapters at or below 3 = %d, want 3", readThrough)
	}
	var aboveBoundary int
	if err := repo.DB().QueryRow(`SELECT COUNT(*) FROM chapter_read_state WHERE manga_id=? AND chapter_id=?`, manga.ID, chapters[3].ID).Scan(&aboveBoundary); err != nil {
		t.Fatal(err)
	}
	if aboveBoundary != 0 {
		t.Fatalf("chapter above the boundary was marked read")
	}
}

// Removing downloads is a preference: with it off, the retired chapters still
// leave the library but their files stay on disk.
func TestApplyMigrationKeepsArtifactsWhenRemovalIsDisabled(t *testing.T) {
	repo, router, oldSource, newSource := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	number := 1.0
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: oldSource, SourceChapterID: "chapter-a", ChapterNumber: &number})
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "kept.cbz")
	if err := os.WriteFile(artifact, []byte("pages"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapter.ID, artifact); err != nil {
		t.Fatal(err)
	}
	discovered, err := repo.UpsertManga(db.Manga{SourceID: newSource, SourceMangaID: "remote-b", Title: "Demo", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"sourceId": newSource, "mangaId": discovered.ID, "flags": 0})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/manga/"+manga.ID+"/migration/apply", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact was removed despite the remove-download flag being off: %v", err)
	}
}
