package tachibackup

import (
	"bytes"
	"compress/gzip"
	"errors"
	"math"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// The fixtures are assembled from raw wire fields so the decoder is exercised
// against the byte layout a protobuf writer produces, not against an encoder
// from this package.

func testTag(num protowire.Number, typ protowire.Type) []byte {
	return protowire.AppendTag(nil, num, typ)
}

func testVarint(num protowire.Number, value uint64) []byte {
	out := testTag(num, protowire.VarintType)
	return protowire.AppendVarint(out, value)
}

func testBytes(num protowire.Number, value []byte) []byte {
	out := testTag(num, protowire.BytesType)
	return protowire.AppendBytes(out, value)
}

func testString(num protowire.Number, value string) []byte {
	return testBytes(num, []byte(value))
}

func testFloat(num protowire.Number, value float32) []byte {
	out := testTag(num, protowire.Fixed32Type)
	return protowire.AppendFixed32(out, math.Float32bits(value))
}

func testConcat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// testBackup returns a small but complete backup document.
func testBackup() []byte {
	category := testConcat(
		testString(1, "Reading"),
		testVarint(2, 3),
		testVarint(3, 7),
	)
	chapter := testConcat(
		testString(1, "https://mangadex.org/chapter/aaa"),
		testString(2, "Chapter 12"),
		testString(3, "Group"),
		testVarint(4, 1),
		testVarint(6, 4),
		testVarint(8, 1_600_000_000_000),
		testFloat(9, 12),
		testVarint(11, 1_500_000),
	)
	history := testConcat(
		testString(1, "https://mangadex.org/chapter/aaa"),
		testVarint(2, 1_700_000_000_000),
		testVarint(3, 90_000),
	)
	tracking := testConcat(
		testVarint(1, 2),
		testVarint(2, 1_234),
		testString(4, "https://anilist.co/manga/1"),
		testString(5, "Sample"),
		testFloat(6, 12),
		testVarint(7, 40),
		testFloat(8, 85),
		testVarint(9, 1),
		testVarint(100, 99_999),
	)
	manga := testConcat(
		testVarint(1, 2499283573021220255),
		testString(2, "sample-uuid"),
		testString(3, "Sample Title"),
		testString(4, "Artist"),
		testString(5, "Author"),
		testString(6, "Description"),
		testString(7, "Action"),
		testString(7, "Adventure"),
		testVarint(8, 1),
		testString(9, "https://mangadex.org/covers/sample.jpg"),
		testVarint(13, 1_600_000_000_000),
		testVarint(14, 0),
		testBytes(16, chapter),
		testVarint(17, 7),
		testBytes(18, tracking),
		testVarint(103, 2),
		testBytes(104, history),
		testVarint(106, 1_650_000_000_000),
		testVarint(111, 1),
	)
	source := testConcat(
		testString(1, "MangaDex"),
		testVarint(2, 2499283573021220255),
	)
	root := testConcat(
		testBytes(1, manga),
		testBytes(2, category),
		testBytes(101, source),
	)
	return root
}

func TestDecodeRawBackup(t *testing.T) {
	backup, err := Decode(testBackup())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(backup.Manga) != 1 || len(backup.Categories) != 1 || len(backup.Sources) != 1 {
		t.Fatalf("decoded %d manga, %d categories, %d sources", len(backup.Manga), len(backup.Categories), len(backup.Sources))
	}
	manga := backup.Manga[0]
	if manga.Title != "Sample Title" || manga.URL != "sample-uuid" || manga.Source != 2499283573021220255 {
		t.Fatalf("manga = %+v", manga)
	}
	if !manga.Favorite || !manga.Initialized {
		t.Fatalf("manga flags = favorite:%v initialized:%v", manga.Favorite, manga.Initialized)
	}
	if manga.ViewerFlags == nil || *manga.ViewerFlags != 2 {
		t.Fatalf("viewer flags = %v", manga.ViewerFlags)
	}
	if len(manga.Genre) != 2 || manga.Genre[1] != "Adventure" {
		t.Fatalf("genres = %v", manga.Genre)
	}
	if len(manga.Chapters) != 1 {
		t.Fatalf("chapters = %+v", manga.Chapters)
	}
	chapter := manga.Chapters[0]
	if chapter.Name != "Chapter 12" || !chapter.Read || chapter.ChapterNumber != 12 || chapter.LastPageRead != 4 {
		t.Fatalf("chapter = %+v", chapter)
	}
	if len(manga.History) != 1 || manga.History[0].ReadDuration != 90_000 {
		t.Fatalf("history = %+v", manga.History)
	}
	if len(manga.Tracking) != 1 || manga.Tracking[0].SyncID != 2 || manga.Tracking[0].MediaID != 99_999 {
		t.Fatalf("tracking = %+v", manga.Tracking)
	}
	if backup.Categories[0].Name != "Reading" || backup.Categories[0].Order != 3 {
		t.Fatalf("category = %+v", backup.Categories[0])
	}
	if backup.Sources[0].Name != "MangaDex" {
		t.Fatalf("source = %+v", backup.Sources[0])
	}
}

func TestDecodeGzippedBackup(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(testBackup()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := Decode(compressed.Bytes())
	if err != nil {
		t.Fatalf("decode gzip: %v", err)
	}
	if len(backup.Manga) != 1 || backup.Manga[0].Title != "Sample Title" {
		t.Fatalf("decoded = %+v", backup)
	}
}

func TestDecodeRejectsJSON(t *testing.T) {
	for _, payload := range []string{"{}", `{"version":1}`, "{\n  \"version\": 1}"} {
		if _, err := Decode([]byte(payload)); !errors.Is(err, ErrJSON) {
			t.Fatalf("decode %q: err = %v, want ErrJSON", payload, err)
		}
	}
}

func TestDecodeRejectsEmptyAndGarbage(t *testing.T) {
	if _, err := Decode(nil); err == nil {
		t.Fatal("empty payload decoded without error")
	}
	if _, err := Decode([]byte("not a backup at all")); err == nil {
		t.Fatal("garbage payload decoded without error")
	}
}

func TestDecodeAbsentFavoriteMeansInLibrary(t *testing.T) {
	// A writer that omits fields equal to their defaults never emits
	// favourite=true, so an absent flag must decode as favourited.
	manga := testConcat(
		testVarint(1, 1),
		testString(2, "url"),
		testString(3, "Title"),
	)
	root := testBytes(1, manga)
	backup, err := Decode(root)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !backup.Manga[0].Favorite {
		t.Fatal("absent favourite decoded as false")
	}
}
