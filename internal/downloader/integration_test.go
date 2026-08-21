package downloader

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/config"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

func TestNetworkMangaDexDownload(t *testing.T) {
	config.LoadEnv()
	if os.Getenv("MAKIDOKU_NETWORK_TESTS") != "1" {
		t.Skip("set MAKIDOKU_NETWORK_TESTS=1 to run the MangaDex download test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dataDir := t.TempDir()
	handle, err := db.Open(filepath.Join(dataDir, "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	catalog := os.Getenv("MAKIDOKU_TEST_REGISTRY")
	if catalog == "" {
		catalog = os.Getenv("MAKIDOKU_REGISTRY_URL")
	}
	eng := engine.New(handle, engine.Options{DataDir: dataDir, RegistryURL: catalog})
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		eng.Close(closeCtx)
	}()
	if _, err := eng.Install(ctx, "mangadex"); err != nil {
		t.Fatalf("install MangaDex: %v", err)
	}

	results, err := eng.Search(ctx, "mangadex", engine.SearchQuery{Query: "Yosuga no Sora", Page: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var mangaID string
	for _, item := range results.Items {
		if strings.EqualFold(strings.TrimSpace(item.Title), "Yosuga no Sora") {
			mangaID = item.ID
			break
		}
	}
	if mangaID == "" {
		for _, item := range results.Items {
			if strings.Contains(strings.ToLower(item.Title), "yosuga no sora") {
				mangaID = item.ID
				break
			}
		}
	}
	if mangaID == "" {
		t.Fatal("Yosuga no Sora was not found")
	}
	details, err := eng.Details(ctx, "mangadex", mangaID)
	if err != nil {
		t.Fatalf("details: %v", err)
	}
	candidates := append([]engine.ChapterItem(nil), details.Chapters...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Number == nil {
			return false
		}
		if candidates[j].Number == nil {
			return true
		}
		return *candidates[i].Number < *candidates[j].Number
	})
	var chapterID string
	var lastPageErr error
	for _, chapter := range candidates {
		if chapter.Language != "en" || chapter.Number == nil {
			continue
		}
		pages, pageErr := eng.Pages(ctx, "mangadex", chapter.ID)
		if pageErr != nil || len(pages) == 0 {
			lastPageErr = pageErr
			continue
		}
		fetchable := true
		for _, page := range pages {
			if _, pageErr = eng.FetchImage(ctx, "mangadex", page.URL, page.Headers); pageErr != nil {
				lastPageErr = pageErr
				fetchable = false
				break
			}
		}
		if !fetchable {
			continue
		}
		chapterID = chapter.ID
		break
	}
	if chapterID == "" {
		t.Fatalf("no fetchable English numbered chapter was found: %v", lastPageErr)
	}

	queue := NewQueue(db.NewRepository(handle), eng, Options{
		Workers: 1, PageInterval: 0, DownloadDir: filepath.Join(dataDir, "downloads"), MaxRetries: 3,
	})
	items, err := queue.EnqueueManga(ctx, "mangadex:"+mangaID, ChapterSelection{IDs: []string{chapterID}}, FormatCBZ)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := queue.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	stored, err := db.NewRepository(handle).GetQueueItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.QueueCompleted || stored.TotalPages < 1 {
		t.Fatalf("queue item = %+v", stored)
	}
	chapter, err := db.NewRepository(handle).GetChapter(stored.ChapterID)
	if err != nil || chapter.DownloadPath == nil {
		t.Fatalf("chapter = %+v, err = %v", chapter, err)
	}
	reader, err := zip.OpenReader(*chapter.DownloadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != stored.TotalPages+1 {
		t.Fatalf("archive entries = %d, want %d pages plus ComicInfo.xml", len(reader.File), stored.TotalPages)
	}
	foundComicInfo := false
	for _, file := range reader.File {
		if file.Name == "ComicInfo.xml" {
			foundComicInfo = true
		}
	}
	if !foundComicInfo {
		t.Fatal("ComicInfo.xml is missing")
	}
	entries, err := os.ReadDir(filepath.Dir(*chapter.DownloadPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary artifact remains: %s", entry.Name())
		}
	}
}
