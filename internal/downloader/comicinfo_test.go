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
