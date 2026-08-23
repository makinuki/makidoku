package downloader

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestComicInfoXMLFields(t *testing.T) {
	raw, err := BuildComicInfo(ComicInfo{
		Title:       "The Journey Begins",
		Series:      "Frieren: Beyond Journey's End",
		Number:      "1",
		Summary:     "The adventure is over.",
		Writers:     []string{"Kanehito Yamada"},
		Pencillers:  []string{"Tsukasa Abe"},
		Genres:      []string{"Adventure", "Fantasy"},
		PageCount:   45,
		LanguageISO: "en",
		SourceName:  "MangaDex",
	})
	if err != nil {
		t.Fatalf("build ComicInfo.xml: %v", err)
	}
	if !strings.HasPrefix(string(raw), xml.Header) {
		t.Fatalf("missing XML header: %s", raw)
	}

	var info comicInfoXML
	if err := xml.Unmarshal(raw, &info); err != nil {
		t.Fatalf("parse ComicInfo.xml: %v", err)
	}
	if info.Series != "Frieren: Beyond Journey's End" || info.Number != "1" {
		t.Fatalf("series fields = %+v", info)
	}
	if info.Writer != "Kanehito Yamada" || info.Penciller != "Tsukasa Abe" {
		t.Fatalf("credits = %+v", info)
	}
	if info.Genre != "Adventure, Fantasy" || info.PageCount != 45 || info.LanguageISO != "en" {
		t.Fatalf("metadata = %+v", info)
	}
	if info.ScanInformation != "MakiDoku/MangaDex" || info.Manga != "YesAndRightToLeft" {
		t.Fatalf("reader fields = %+v", info)
	}
}

func TestComicInfoWritesOptionalVolume(t *testing.T) {
	raw, err := BuildComicInfo(ComicInfo{Series: "Yosuga no Sora", Number: "10.5", Volume: 3, PageCount: 24})
	if err != nil {
		t.Fatalf("build ComicInfo.xml: %v", err)
	}
	if !strings.Contains(string(raw), "<Volume>3</Volume>") {
		t.Fatalf("missing volume element: %s", raw)
	}

	var info comicInfoXML
	if err := xml.Unmarshal(raw, &info); err != nil {
		t.Fatalf("parse ComicInfo.xml: %v", err)
	}
	if info.Volume != 3 {
		t.Fatalf("volume = %d, want 3", info.Volume)
	}

	withoutVolume, err := BuildComicInfo(ComicInfo{Series: "Yosuga no Sora", Number: "1", PageCount: 24})
	if err != nil {
		t.Fatalf("build ComicInfo.xml without volume: %v", err)
	}
	if strings.Contains(string(withoutVolume), "Volume") {
		t.Fatalf("volume element must be omitted when unset: %s", withoutVolume)
	}
}
