package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/downloader"
)

// writeArtifact stores a two-page download artifact and marks the chapter as
// downloaded, mimicking a completed queue run.
func writeArtifact(t *testing.T, repo *db.Repository, sourceID, chapterID, mangaTitle, format string) string {
	t.Helper()
	archiver := downloader.NewArchiver(filepath.Join(t.TempDir(), "downloads"))
	path, err := archiver.Write(downloader.ArchiveRequest{
		SourceID:    sourceID,
		MangaTitle:  mangaTitle,
		ChapterName: "Chapter 1",
		Format:      format,
		Pages: []downloader.PageData{
			{Bytes: []byte("page-one-bytes"), Extension: ".png"},
			{Bytes: []byte("page-two-bytes"), Extension: ".jpg"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapterID, path); err != nil {
		t.Fatal(err)
	}
	return path
}

// The reader serves downloaded artifacts directly: the bytes never touch the
// image cache or the source, so a downloaded chapter opens instantly and
// works offline.
func TestPageImageServesDownloadedArtifact(t *testing.T) {
	for _, format := range []string{downloader.FormatCBZ, downloader.FormatFolder} {
		t.Run(format, func(t *testing.T) {
			hits := 0
			repo, router, _, sourceID := coverTestRouter(t, &hits)
			manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
			if err != nil {
				t.Fatal(err)
			}
			chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: sourceID, SourceChapterID: "chapter-1"})
			if err != nil {
				t.Fatal(err)
			}
			writeArtifact(t, repo, sourceID, chapter.ID, manga.Title, format)
			pages, err := repo.UpsertPages(chapter.ID, sourceID, []db.Page{{PageIndex: 0}, {PageIndex: 1}})
			if err != nil {
				t.Fatal(err)
			}

			for index, want := range []string{"page-one-bytes", "page-two-bytes"} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pages[index].ID+"/image", nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d: status = %d body = %s", index, rec.Code, rec.Body.String())
				}
				if rec.Body.String() != want {
					t.Fatalf("page %d: body = %q, want %q", index, rec.Body.String(), want)
				}
			}
		})
	}
}

// Chapters downloaded before page lists were persisted at completion get
// their list synthesized from the artifact, without contacting the source.
func TestMaterializePagesDerivesListFromDownloadedArtifact(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: sourceID, SourceChapterID: "chapter-1"})
	if err != nil {
		t.Fatal(err)
	}
	writeArtifact(t, repo, sourceID, chapter.ID, manga.Title, downloader.FormatCBZ)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/chapters/"+chapter.ID+"/pages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var pages []db.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &pages); err != nil {
		t.Fatalf("decode pages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want the artifact entry count", len(pages))
	}
	stored, err := repo.ListPages(chapter.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("derived list was not persisted: %d pages, err = %v", len(stored), err)
	}
}

// A vanished artifact must not break the request chain; the read falls back
// to the caches and the source, which fails on the missing demo plugin.
func TestPageImageFallsThroughWhenArtifactMissing(t *testing.T) {
	hits := 0
	repo, router, _, sourceID := coverTestRouter(t, &hits)
	manga, err := repo.UpsertManga(db.Manga{SourceID: sourceID, SourceMangaID: "remote-manga", Title: "Demo", Status: "ongoing"})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := repo.UpsertChapter(db.Chapter{MangaID: manga.ID, SourceID: sourceID, SourceChapterID: "chapter-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkChapterDownloaded(chapter.ID, filepath.Join(t.TempDir(), "gone", "Chapter 1.cbz")); err != nil {
		t.Fatal(err)
	}
	pages, err := repo.UpsertPages(chapter.ID, sourceID, []db.Page{{PageIndex: 0, RemoteURL: "https://upstream.test/p1.png"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pages[0].ID+"/image", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want the fallback failure for a vanished artifact", rec.Code)
	}
}
