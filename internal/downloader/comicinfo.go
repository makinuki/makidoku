package downloader

import (
	"encoding/xml"
	"strings"
)

// ComicInfo contains the chapter metadata written into ComicInfo.xml.
type ComicInfo struct {
	Title       string
	Series      string
	Number      string
	Summary     string
	Writers     []string
	Pencillers  []string
	Genres      []string
	PageCount   int
	LanguageISO string
	SourceName  string
}

type comicInfoXML struct {
	XMLName         xml.Name `xml:"ComicInfo"`
	XMLNSXSI        string   `xml:"xmlns:xsi,attr"`
	XMLNSXSD        string   `xml:"xmlns:xsd,attr"`
	Title           string   `xml:"Title,omitempty"`
	Series          string   `xml:"Series,omitempty"`
	Number          string   `xml:"Number,omitempty"`
	Summary         string   `xml:"Summary,omitempty"`
	Writer          string   `xml:"Writer,omitempty"`
	Penciller       string   `xml:"Penciller,omitempty"`
	Genre           string   `xml:"Genre,omitempty"`
	PageCount       int      `xml:"PageCount"`
	LanguageISO     string   `xml:"LanguageISO,omitempty"`
	ScanInformation string   `xml:"ScanInformation,omitempty"`
	Manga           string   `xml:"Manga"`
}

// BuildComicInfo serializes metadata in the ComicInfo.xml format used by
// common comic readers and library servers.
func BuildComicInfo(info ComicInfo) ([]byte, error) {
	document := comicInfoXML{
		XMLNSXSI:        "http://www.w3.org/2001/XMLSchema-instance",
		XMLNSXSD:        "http://www.w3.org/2001/XMLSchema",
		Title:           info.Title,
		Series:          info.Series,
		Number:          info.Number,
		Summary:         info.Summary,
		Writer:          strings.Join(info.Writers, ", "),
		Penciller:       strings.Join(info.Pencillers, ", "),
		Genre:           strings.Join(info.Genres, ", "),
		PageCount:       info.PageCount,
		LanguageISO:     info.LanguageISO,
		ScanInformation: "MakiDoku/" + info.SourceName,
		Manga:           "YesAndRightToLeft",
	}
	raw, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), raw...), nil
}
