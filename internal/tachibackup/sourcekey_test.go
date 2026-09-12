package tachibackup

import "testing"

// TestRecordedLocatorsAreStoredVerbatim covers the locator shapes a backup
// records. Every shape is stored as written, because only the source that
// recorded a locator knows how to read it, and truncating one to its last path
// segment loses the part that identifies the series.
func TestRecordedLocatorsAreStoredVerbatim(t *testing.T) {
	locators := []struct {
		name string
		raw  string
		want string
	}{
		{"plugin native pair", "nh6Ii/jWohx", "nh6Ii/jWohx"},
		{"root relative path", "/series/nano-machine/chapter/304", "/series/nano-machine/chapter/304"},
		{"relative path", "title/5yrz-jungle-juice/11301306-chapter-222", "title/5yrz-jungle-juice/11301306-chapter-222"},
		{"opaque identifier", "69a873c0e8ded0ca88fc5498", "69a873c0e8ded0ca88fc5498"},
		{"json identifier", `{"id":"46645","source":"scraper","isVolume":false}`, `{"id":"46645","source":"scraper","isVolume":false}`},
		{"paired identifier", "34CF9JIVL41V46D32DDAR1NU25;3ACI98IVLC1X46F3KDDA61QUI5;12", "34CF9JIVL41V46D32DDAR1NU25;3ACI98IVLC1X46F3KDDA61QUI5;12"},
		{"fragment", "/series/confinement-king/chapter-33#13559", "/series/confinement-king/chapter-33#13559"},
		{"query", "/Comic/Invincible/Issue-144?id=130552", "/Comic/Invincible/Issue-144?id=130552"},
		{"surrounding space", "  /chapter/1516875  ", "/chapter/1516875"},
		{"empty", "   ", ""},
	}
	for _, test := range locators {
		t.Run(test.name, func(t *testing.T) {
			if got := seriesLocator(test.raw); got != test.want {
				t.Fatalf("seriesLocator = %q, want %q", got, test.want)
			}
			if got := chapterLocator(test.raw); got != test.want {
				t.Fatalf("chapterLocator = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSeriesPageURLCarriesOnlyAnAbsoluteURL covers the page a title opens on
// the source site. A relative path or an opaque identifier is not a page of
// its own, so the page is left unset and resolved from the source when the
// title is first opened.
func TestSeriesPageURLCarriesOnlyAnAbsoluteURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"absolute", "https://weebcentral.com/series/01JX/Return-of-the-Hound", "https://weebcentral.com/series/01JX/Return-of-the-Hound"},
		{"root relative path", "/series/nano-machine", ""},
		{"relative path", "title/5yrz-jungle-juice", ""},
		{"opaque identifier", "nh6Ii", ""},
		{"empty", "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := seriesPageURL(test.raw); got != test.want {
				t.Fatalf("seriesPageURL = %q, want %q", got, test.want)
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
	if err := repo.DB().Get(&key, `SELECT source_manga_id FROM manga_sources WHERE source_id='mangadex' AND source_manga_id LIKE '%library-uuid'`); err != nil {
		t.Fatal(err)
	}
	if key != "/manga/library-uuid" {
		t.Fatalf("series key = %q", key)
	}
	if err := repo.DB().Get(&page, `SELECT COALESCE(url,'') FROM manga_sources WHERE source_manga_id='/manga/library-uuid'`); err != nil {
		t.Fatal(err)
	}
	if page != "" {
		t.Fatalf("series page = %q, want empty for a relative recorded locator", page)
	}
	var chapterKey string
	if err := repo.DB().Get(&chapterKey, `SELECT source_chapter_id FROM chapter_sources WHERE source_id='mangadex'`); err != nil {
		t.Fatal(err)
	}
	if chapterKey != "/chapter/lib-chapter" && chapterKey != "/chapter/loose-chapter" {
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
