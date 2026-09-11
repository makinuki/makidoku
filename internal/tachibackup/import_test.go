package tachibackup

import (
	"path/filepath"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

// openTestDB returns a migrated database with one installed MangaDex source.
func openTestDB(t *testing.T) *db.Repository {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "tachibackup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed,installed_at)
		VALUES('mangadex','mangadex','MangaDex','1',1,'en','https://mangadex.org','mangadex.wasm',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed,installed_at)
		VALUES('asura','asurascans','Asura Scans','1',1,'en','https://asuracomic.net','asura.wasm',1,1)`); err != nil {
		t.Fatal(err)
	}
	return db.NewRepository(handle)
}

// fixture builds a one-title backup for the given source.
func fixture(sourceName string, sourceID int64, seriesURL, chapterURL, thumbnailURL string) []byte {
	chapter := testConcat(
		testString(1, chapterURL),
		testString(2, "Chapter 12"),
		testString(3, "Group"),
		testVarint(4, 1),
		testVarint(6, 4),
		testVarint(8, 1_600_000_000_000),
		testFloat(9, 12),
	)
	history := testConcat(
		testString(1, chapterURL),
		testVarint(2, 1_700_000_000_000),
		testVarint(3, 90_000),
	)
	tracking := testConcat(
		testVarint(1, 2),
		testString(5, "Sample"),
		testFloat(6, 12),
		testVarint(7, 40),
		testVarint(9, 1),
		testVarint(100, 99_999),
	)
	unmappedTracking := testConcat(
		testVarint(1, 5),
		testString(5, "Sample"),
		testVarint(100, 5),
	)
	manga := testConcat(
		testVarint(1, uint64(sourceID)),
		testString(2, seriesURL),
		testString(3, "Sample Title"),
		testString(5, "Author"),
		testString(6, "Description"),
		testString(7, "Action"),
		testVarint(8, 1),
		testString(9, thumbnailURL),
		testVarint(13, 1_600_000_000_000),
		testBytes(16, chapter),
		// The writer records the category order on the title, not the id.
		testVarint(17, 3),
		testBytes(18, tracking),
		testBytes(18, unmappedTracking),
		testVarint(103, 2),
		testBytes(104, history),
		testVarint(106, 1_650_000_000_000),
	)
	category := testConcat(
		testString(1, "Reading"),
		testVarint(2, 3),
		testVarint(3, 7),
	)
	source := testConcat(
		testString(1, sourceName),
		testVarint(2, uint64(sourceID)),
	)
	return testConcat(
		testBytes(1, manga),
		testBytes(2, category),
		testBytes(101, source),
	)
}

func installedRefs(t *testing.T, repo *db.Repository) []SourceRef {
	t.Helper()
	var rows []struct {
		ID        string `db:"id"`
		PluginKey string `db:"plugin_key"`
		Name      string `db:"name"`
		BaseURL   string `db:"base_url"`
	}
	if err := repo.DB().Select(&rows, `SELECT id,COALESCE(plugin_key,'') AS plugin_key,name,base_url FROM sources WHERE installed=1`); err != nil {
		t.Fatal(err)
	}
	refs := make([]SourceRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, SourceRef{ID: row.ID, PluginKey: row.PluginKey, Name: row.Name, BaseURL: row.BaseURL})
	}
	return refs
}

func decodeFixture(t *testing.T, payload []byte) *Backup {
	t.Helper()
	backup, err := Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	return backup
}

func TestBuildMatchesSourcesByNameAndHost(t *testing.T) {
	repo := openTestDB(t)
	installed := installedRefs(t, repo)

	plan := Build(decodeFixture(t, fixture("MangaDex", 42, "sample-uuid", "https://mangadex.org/chapter/aaa", "https://uploads.mangadex.org/covers/x.jpg")), installed, Options{})
	if len(plan.Sources) != 1 || plan.Sources[0].MatchedSourceID != "mangadex" || plan.Sources[0].Match != "name" {
		t.Fatalf("name match = %+v", plan.Sources)
	}
	if plan.UnmatchedTitles != 0 {
		t.Fatalf("unmatched titles = %d", plan.UnmatchedTitles)
	}
	if got := plan.CountsByTracker(); got != 1 {
		t.Fatalf("supported trackings = %d", got)
	}
	if plan.UnsupportedTrackings != 1 {
		t.Fatalf("unsupported trackings = %d", plan.UnsupportedTrackings)
	}

	// A renamed extension is matched through the series host instead.
	renamed := Build(decodeFixture(t, fixture("MD (EN)", 42, "sample-uuid", "https://mangadex.org/chapter/aaa", "https://uploads.mangadex.org/covers/x.jpg")), installed, Options{})
	if renamed.Sources[0].MatchedSourceID != "mangadex" || renamed.Sources[0].Match != "host" {
		t.Fatalf("host match = %+v", renamed.Sources)
	}
}

func TestBuildHonoursManualMappingAndSkipPolicy(t *testing.T) {
	repo := openTestDB(t)
	installed := installedRefs(t, repo)
	backup := decodeFixture(t, fixture("Other Site", 7, "https://othersite.test/series/x", "https://othersite.test/read/x/1", "https://cdn.othersite.test/cover.jpg"))

	deferred := Build(backup, installed, Options{})
	if !deferred.Sources[0].Deferred || deferred.UnmatchedTitles != 1 {
		t.Fatalf("deferred plan = %+v", deferred.Sources)
	}
	skipped := Build(backup, installed, Options{SkipUnmatched: true})
	if !skipped.Sources[0].Deferred || skipped.UnmatchedTitles != 1 {
		t.Fatalf("skip plan = %+v", skipped.Sources)
	}
	mapped := Build(backup, installed, Options{SourceMap: map[int64]string{7: "asura"}})
	if mapped.Sources[0].MatchedSourceID != "asura" || mapped.Sources[0].Deferred {
		t.Fatalf("manual mapping = %+v", mapped.Sources)
	}
}

func TestImportCreatesLibraryState(t *testing.T) {
	repo := openTestDB(t)
	backup := decodeFixture(t, fixture("MangaDex", 42, "sample-uuid", "https://mangadex.org/chapter/aaa", "https://uploads.mangadex.org/covers/x.jpg"))
	plan := Build(backup, installedRefs(t, repo), Options{})
	summary, err := Import(repo.DB(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Manga != 1 || summary.Chapters != 1 || summary.ReadChapters != 1 || summary.Tracking != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Categories != 1 || summary.History != 1 || summary.ReadingSessions != 1 || summary.SkippedTracking != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.DeferredManga != 0 || summary.MergedManga != 0 {
		t.Fatalf("summary = %+v", summary)
	}

	var stored db.Manga
	if err := repo.DB().Get(&stored, `SELECT * FROM manga`); err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Sample Title" || stored.SourceID != "mangadex" || !stored.InLibrary || stored.Status != "Ongoing" {
		t.Fatalf("stored manga = %+v", stored)
	}
	if stored.ReaderMode == nil || *stored.ReaderMode != "single" || stored.ReaderDirection == nil || *stored.ReaderDirection != "rtl" {
		t.Fatalf("reader overrides = %v/%v", stored.ReaderMode, stored.ReaderDirection)
	}

	var progress db.ReadingProgress
	if err := repo.DB().Get(&progress, `SELECT * FROM reading_progress`); err != nil {
		t.Fatal(err)
	}
	if progress.LastReadPage != 5 {
		t.Fatalf("resume page = %d, want 5", progress.LastReadPage)
	}
	var readState db.ChapterReadState
	if err := repo.DB().Get(&readState, `SELECT * FROM chapter_read_state`); err != nil {
		t.Fatal(err)
	}
	if !readState.Read {
		t.Fatal("chapter not marked read")
	}
	var seconds int64
	if err := repo.DB().Get(&seconds, `SELECT COALESCE(SUM(seconds),0) FROM reading_sessions`); err != nil {
		t.Fatal(err)
	}
	if seconds != 90 {
		t.Fatalf("reading seconds = %d, want 90", seconds)
	}
	var binding db.TrackerBinding
	if err := repo.DB().Get(&binding, `SELECT * FROM tracker_bindings`); err != nil {
		t.Fatal(err)
	}
	if binding.TrackerType != "anilist" || binding.RemoteID != "99999" {
		t.Fatalf("binding = %+v", binding)
	}
}

func TestImportIsIdempotent(t *testing.T) {
	repo := openTestDB(t)
	backup := decodeFixture(t, fixture("MangaDex", 42, "sample-uuid", "https://mangadex.org/chapter/aaa", "https://uploads.mangadex.org/covers/x.jpg"))
	plan := Build(backup, installedRefs(t, repo), Options{})
	if _, err := Import(repo.DB(), plan); err != nil {
		t.Fatal(err)
	}
	second, err := Import(repo.DB(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if second.Chapters != 0 || second.History != 0 || second.ReadingSessions != 0 || second.Categories != 0 {
		t.Fatalf("second import wrote new rows: %+v", second)
	}
	if second.MergedManga != 1 {
		t.Fatalf("second import did not merge: %+v", second)
	}
	var chapters, history, sessions, entries int
	for query, dest := range map[string]*int{
		`SELECT COUNT(*) FROM chapters`:         &chapters,
		`SELECT COUNT(*) FROM history_events`:   &history,
		`SELECT COUNT(*) FROM reading_sessions`: &sessions,
		`SELECT COUNT(*) FROM manga_categories`: &entries,
	} {
		if err := repo.DB().Get(dest, query); err != nil {
			t.Fatal(err)
		}
	}
	if chapters != 1 || history != 1 || sessions != 1 || entries != 1 {
		t.Fatalf("counts after re-import = chapters:%d history:%d sessions:%d categories:%d", chapters, history, sessions, entries)
	}
}

func TestImportDefersAndSkipsUnmatchedSources(t *testing.T) {
	repo := openTestDB(t)
	backup := decodeFixture(t, fixture("Other Site", 7, "https://othersite.test/series/x", "https://othersite.test/read/x/1", "https://cdn.othersite.test/cover.jpg"))

	plan := Build(backup, installedRefs(t, repo), Options{})
	summary, err := Import(repo.DB(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if summary.DeferredManga != 1 || summary.SkippedManga != 0 {
		t.Fatalf("deferred summary = %+v", summary)
	}
	var sourceID string
	if err := repo.DB().Get(&sourceID, `SELECT source_id FROM manga`); err != nil {
		t.Fatal(err)
	}
	if sourceID != "imported-7" {
		t.Fatalf("deferred source = %q", sourceID)
	}
	var installed int
	if err := repo.DB().Get(&installed, `SELECT installed FROM sources WHERE id='imported-7'`); err != nil {
		t.Fatal(err)
	}
	if installed != 0 {
		t.Fatal("placeholder source is marked installed")
	}
	other := openTestDB(t)
	skipped := Build(backup, installedRefs(t, other), Options{SkipUnmatched: true})
	if _, err := Import(other.DB(), skipped); err != nil {
		t.Fatal(err)
	}
	var manga int
	if err := other.DB().Get(&manga, `SELECT COUNT(*) FROM manga`); err != nil {
		t.Fatal(err)
	}
	if manga != 0 {
		t.Fatalf("skip policy imported %d titles", manga)
	}
	var sources int
	if err := other.DB().Get(&sources, `SELECT COUNT(*) FROM sources WHERE id LIKE 'imported-%'`); err != nil {
		t.Fatal(err)
	}
	if sources != 0 {
		t.Fatalf("skip policy created %d placeholder sources", sources)
	}
}

func TestImportMergesOntoExistingTitle(t *testing.T) {
	repo := openTestDB(t)
	existing, err := repo.UpsertManga(db.Manga{SourceID: "mangadex", SourceMangaID: "sample-uuid", Title: "Old Title", Status: "Unknown", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	backup := decodeFixture(t, fixture("MangaDex", 42, "sample-uuid", "https://mangadex.org/chapter/aaa", "https://uploads.mangadex.org/covers/x.jpg"))
	plan := Build(backup, installedRefs(t, repo), Options{})
	summary, err := Import(repo.DB(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if summary.MergedManga != 1 || summary.Manga != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	var count int
	if err := repo.DB().Get(&count, `SELECT COUNT(*) FROM manga`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("manga rows = %d", count)
	}
	var stored db.Manga
	if err := repo.DB().Get(&stored, `SELECT * FROM manga WHERE id=?`, existing.ID); err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Sample Title" || stored.Status != "Ongoing" {
		t.Fatalf("merged manga = %+v", stored)
	}
}
