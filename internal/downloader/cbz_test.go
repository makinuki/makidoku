package downloader

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestArchiverWritesCBZWithZeroPaddedPagesAndComicInfo(t *testing.T) {
	root := t.TempDir()
	archiver := NewArchiver(root)
	path, err := archiver.Write(ArchiveRequest{
		SourceID:    "mangadex",
		MangaTitle:  "Yosuga no Sora",
		ChapterName: "Chapter 1",
		Format:      FormatCBZ,
		Pages: []PageData{
			{Bytes: []byte("one"), Extension: ".jpg"},
			{Bytes: []byte("two"), Extension: ".png"},
		},
		ComicInfo: ComicInfo{Series: "Yosuga no Sora", Number: "1", PageCount: 2},
	})
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if !strings.HasSuffix(path, ".cbz") {
		t.Fatalf("path = %q", path)
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open cbz: %v", err)
	}
	defer reader.Close()
	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	want := []string{"001.jpg", "002.png", "ComicInfo.xml"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("zip files = %v, want %v", names, want)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary artifact remains: %s", entry.Name())
		}
	}
}

func TestArchiverWritesExtractedFolder(t *testing.T) {
	archiver := NewArchiver(t.TempDir())
	path, err := archiver.Write(ArchiveRequest{
		SourceID: "mangadex", MangaTitle: `A Title: Test`, ChapterName: "Oneshot",
		Format: FormatFolder, Pages: []PageData{{Bytes: []byte("page"), Extension: ".jpg"}},
		ComicInfo: ComicInfo{Series: "A Title: Test", PageCount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001.jpg", "ComicInfo.xml"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestArchiverReplacesExistingExtractedFolder(t *testing.T) {
	archiver := NewArchiver(t.TempDir())
	request := ArchiveRequest{
		SourceID: "mangadex", MangaTitle: "Title", ChapterName: "Chapter 1",
		Format: FormatFolder, Pages: []PageData{{Bytes: []byte("old"), Extension: ".jpg"}},
		ComicInfo: ComicInfo{Series: "Title", PageCount: 1},
	}
	path, err := archiver.Write(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Pages[0].Bytes = []byte("new")
	if _, err := archiver.Write(request); err != nil {
		t.Fatalf("replace folder: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(path, "001.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new" {
		t.Fatalf("page = %q", raw)
	}
}
