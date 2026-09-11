package api

import (
	"bytes"
	"encoding/json"
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
		Source     string `json:"source"`
		ChapterMap map[string]string
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

// Candidate searches report how many plugins failed instead of presenting a
// partial outage as an empty catalog.
func TestMigrationCandidatesReportFailedSources(t *testing.T) {
	repo, router, oldSource, newSource := migrationTestRouter(t)
	manga, err := repo.UpsertManga(db.Manga{SourceID: oldSource, SourceMangaID: "remote-a", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	// Both plugins point at unreachable hosts; both searches fail.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manga/"+manga.ID+"/migration/candidates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Candidates    []db.Manga `json:"candidates"`
		FailedSources int        `json:"failedSources"`
		Searched      int        `json:"searched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	// The title's own plugin is skipped; the remaining one fails on its
	// missing binary.
	if payload.FailedSources != 1 || payload.Searched != 1 {
		t.Fatalf("failed = %d searched = %d, want the single other plugin counted", payload.FailedSources, payload.Searched)
	}
	_ = newSource
}

// Reading progress carries over when the replacement source has a chapter
// with the same number.
func TestRemapProgressMatchesByChapterNumber(t *testing.T) {
	oldNumber := 7.0
	other := 9.0
	retired := []db.Chapter{
		{ID: "old-7", ChapterNumber: &oldNumber},
		{ID: "old-9", ChapterNumber: &other},
	}
	newNumber := 7.0
	replacement := []db.Chapter{
		{ID: "new-7", ChapterNumber: &newNumber},
	}
	progress := db.ReadingProgress{MangaID: "m", LastReadChapterID: "old-7", LastReadPage: 3, TotalPages: 20, LastReadAt: 42}

	mapped := remapProgress(progress, retired, replacement)
	if mapped == nil || mapped.LastReadChapterID != "new-7" {
		t.Fatalf("remapped progress = %+v, want it moved to new-7", mapped)
	}
	if mapped.LastReadPage != 3 || mapped.TotalPages != 20 || mapped.LastReadAt != 42 {
		t.Fatalf("remapped progress lost its state: %+v", mapped)
	}

	missing := remapProgress(progress, retired, nil)
	if missing != nil {
		t.Fatalf("unmatched progress = %+v, want nil", missing)
	}
}
