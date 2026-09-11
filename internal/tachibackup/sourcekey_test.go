package tachibackup

import "testing"

// TestSourceKeyTranslation covers the locator shapes the writing application
// records for the sources MakiDoku ships today.
func TestSourceKeyTranslation(t *testing.T) {
	const mangadexBase = "https://mangadex.org"
	const asuraBase = "https://asurascans.com"
	series := []struct {
		name     string
		kind     keyKind
		raw      string
		wantKey  string
		wantPage string
	}{
		{"mangadex relative", keyMangaDex, "/manga/7c062006-5ea7-4fbc-a39d-23e3f66420e4", "7c062006-5ea7-4fbc-a39d-23e3f66420e4", mangadexBase + "/title/7c062006-5ea7-4fbc-a39d-23e3f66420e4"},
		{"mangadex title path", keyMangaDex, "https://mangadex.org/title/abc", "abc", mangadexBase + "/title/abc"},
		{"asura series path", keyAsuraScans, "/series/nano-machine", "nano-machine", asuraBase + "/comics/nano-machine"},
		{"asura comics path", keyAsuraScans, "https://asurascans.com/comics/nano-machine", "nano-machine", asuraBase + "/comics/nano-machine"},
		{"default opaque id", keyDefault, "34CF9JIVL41V46D32DDAR1NU25", "34CF9JIVL41V46D32DDAR1NU25", "34CF9JIVL41V46D32DDAR1NU25"},
		{"default short path", keyDefault, "/5yrz", "5yrz", "https://mangapark.net/5yrz"},
		{"default absolute", keyDefault, "https://weebcentral.com/series/01JX/Return-of-the-Hound", "Return-of-the-Hound", "https://weebcentral.com/series/01JX/Return-of-the-Hound"},
		{"default strips fragment", keyDefault, "/title/94073-en-name#94073", "94073-en-name", "https://mangapark.net/title/94073-en-name"},
	}
	base := map[keyKind]string{keyMangaDex: mangadexBase, keyAsuraScans: asuraBase, keyDefault: "https://mangapark.net"}
	for _, test := range series {
		t.Run(test.name, func(t *testing.T) {
			key := seriesKey(test.kind, test.raw)
			if key != test.wantKey {
				t.Fatalf("seriesKey = %q, want %q", key, test.wantKey)
			}
			if got := seriesPageURL(test.kind, test.raw, key, base[test.kind]); got != test.wantPage {
				t.Fatalf("seriesPageURL = %q, want %q", got, test.wantPage)
			}
		})
	}

	chapters := []struct {
		name    string
		kind    keyKind
		raw     string
		series  string
		wantKey string
	}{
		{"mangadex relative", keyMangaDex, "/chapter/69f53703-541c-436f-a3f7-d00482a2bc5e", "series-id", "69f53703-541c-436f-a3f7-d00482a2bc5e"},
		{"mangadex absolute", keyMangaDex, "https://mangadex.org/chapter/aaa", "series-id", "aaa"},
		{"asura rebuilds the page url", keyAsuraScans, "/series/nano-machine/chapter/304", "nano-machine", asuraBase + "/comics/nano-machine/chapter/304"},
		{"default last segment", keyDefault, "title/5yrz-jungle-juice/11301306-chapter-222", "5yrz", "11301306-chapter-222"},
	}
	for _, test := range chapters {
		t.Run(test.name, func(t *testing.T) {
			if got := chapterKey(test.kind, test.raw, test.series, base[test.kind]); got != test.wantKey {
				t.Fatalf("chapterKey = %q, want %q", got, test.wantKey)
			}
		})
	}
}

// TestDetectSite covers the two unnamed sources a real backup carries.
func TestDetectSite(t *testing.T) {
	comick := detectSite([]string{"/comic/i7Lm0zt2#", "https://meo.comick.pictures/2NRGrm.jpg#18"})
	if comick.Name != "Comick" || comick.Host != "meo.comick.pictures" {
		t.Fatalf("comick detection = %+v", comick)
	}
	disaster := detectSite([]string{"/comics/11422-martial-peak", "https://disasterscans.com/_next/image?url=x"})
	if disaster.Name != "Disaster Scans" || disaster.Host != "disasterscans.com" {
		t.Fatalf("disaster detection = %+v", disaster)
	}
	pathOnly := detectSite([]string{"/comic/abc#1"})
	if pathOnly.Name != "Comick" || pathOnly.Host != "" {
		t.Fatalf("path detection = %+v", pathOnly)
	}
	unknown := detectSite([]string{"https://unknown.example/series/x"})
	if unknown.Name != "" || unknown.Host != "unknown.example" {
		t.Fatalf("unknown detection = %+v", unknown)
	}
}

func TestTrackerStatusDefaults(t *testing.T) {
	if got := trackerStatus("mangaupdates", 0); got == nil || *got != "reading" {
		t.Fatalf("mangaupdates zero = %v", got)
	}
	if got := trackerStatus("anilist", 0); got != nil {
		t.Fatalf("anilist zero = %v", got)
	}
	if got := trackerStatus("anilist", 2); got == nil || *got != "completed" {
		t.Fatalf("anilist completed = %v", got)
	}
	if got := trackerStatus("mangaupdates", 7); got != nil {
		t.Fatalf("unknown status = %v", got)
	}
	if name := TrackerDisplayName(60); name != "MdList" {
		t.Fatalf("display name = %q", name)
	}
}

// TestImportCategoryOrderAndOutOfLibrary writes two titles: one in the
// library, one carried only for its read state.
func TestImportCategoryOrderAndOutOfLibrary(t *testing.T) {
	repo := openTestDB(t)
	library := testConcat(
		testVarint(1, 42),
		testString(2, "/manga/library-uuid"),
		testString(3, "In Library"),
		testVarint(17, 0),
		testBytes(16, testConcat(testString(1, "/chapter/lib-chapter"), testString(2, "Ch. 1"))),
	)
	loose := testConcat(
		testVarint(1, 42),
		testString(2, "/manga/loose-uuid"),
		testString(3, "Not Favourited"),
		testVarint(17, 0),
		testVarint(100, 0),
		testBytes(16, testConcat(testString(1, "/chapter/loose-chapter"), testString(2, "Ch. 2"))),
	)
	category := testConcat(testString(1, "manga"), testVarint(2, 0), testVarint(3, 1))
	source := testConcat(testString(1, "MangaDex"), testVarint(2, 42))
	backup := decodeFixture(t, testConcat(testBytes(1, library), testBytes(1, loose), testBytes(2, category), testBytes(101, source)))

	summary, err := Import(repo.DB(), Build(backup, installedRefs(t, repo), Options{}))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Manga != 2 || summary.OutOfLibrary != 1 || summary.SkippedManga != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	var inLibrary bool
	if err := repo.DB().Get(&inLibrary, `SELECT in_library FROM manga WHERE title='Not Favourited'`); err != nil {
		t.Fatal(err)
	}
	if inLibrary {
		t.Fatal("a title outside the library was added to it")
	}
	var assigned int
	if err := repo.DB().Get(&assigned, `SELECT COUNT(*) FROM manga_categories mc JOIN categories c ON c.id=mc.category_id WHERE c.name='manga'`); err != nil {
		t.Fatal(err)
	}
	if assigned != 2 {
		t.Fatalf("category assignments = %d, want 2", assigned)
	}
	var key, page string
	if err := repo.DB().Get(&key, `SELECT source_manga_id FROM manga_sources WHERE source_id='mangadex' AND url LIKE '%library-uuid'`); err != nil {
		t.Fatal(err)
	}
	if key != "library-uuid" {
		t.Fatalf("series key = %q", key)
	}
	if err := repo.DB().Get(&page, `SELECT url FROM manga_sources WHERE source_manga_id='library-uuid'`); err != nil {
		t.Fatal(err)
	}
	if page != "https://mangadex.org/title/library-uuid" {
		t.Fatalf("series page = %q", page)
	}
	var chapterKey string
	if err := repo.DB().Get(&chapterKey, `SELECT source_chapter_id FROM chapter_sources WHERE source_id='mangadex'`); err != nil {
		t.Fatal(err)
	}
	if chapterKey != "lib-chapter" && chapterKey != "loose-chapter" {
		t.Fatalf("chapter key = %q", chapterKey)
	}

	skipped := openTestDB(t)
	second := Build(backup, installedRefs(t, skipped), Options{SkipOutOfLibrary: true})
	secondSummary, err := Import(skipped.DB(), second)
	if err != nil {
		t.Fatal(err)
	}
	if secondSummary.Manga != 1 || secondSummary.OutOfLibrary != 0 || secondSummary.SkippedManga != 1 {
		t.Fatalf("skip summary = %+v", secondSummary)
	}
}
