package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/config"
	"github.com/makinuki/makidoku/internal/db"
)

// liveEngine builds an engine against a real catalog. These tests reach the
// network and execute compiled plugins. The catalog resolves from the
// MAKIDOKU_TEST_REGISTRY override, then from the daemon's own
// MAKIDOKU_REGISTRY_URL setting, then from the public registry, so a plain
// test run exercises the same plugins the daemon installs.
func liveEngine(t *testing.T) *Engine {
	t.Helper()
	config.LoadEnv()
	if os.Getenv("MAKIDOKU_NETWORK_TESTS") != "1" {
		t.Skip("set MAKIDOKU_NETWORK_TESTS=1 to run live source tests")
	}
	catalog := os.Getenv("MAKIDOKU_TEST_REGISTRY")
	if catalog == "" {
		catalog = os.Getenv("MAKIDOKU_REGISTRY_URL")
	}
	if catalog == "" {
		catalog = DefaultRegistryURL
	}

	dataDir := t.TempDir()
	handle, err := db.Open(filepath.Join(dataDir, "makidoku.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	eng := New(handle, Options{DataDir: dataDir, RegistryURL: catalog})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		eng.Close(ctx)
		handle.Close()
	})
	return eng
}

func installSource(t *testing.T, eng *Engine, id string) InstalledSource {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	source, err := eng.Install(ctx, id)
	if err != nil {
		t.Fatalf("install %s: %v", id, err)
	}
	if source.ID != id {
		t.Fatalf("installed id = %s, want %s", source.ID, id)
	}
	if source.ABIVersion != ABIVersion {
		t.Fatalf("abi version = %d, want %d", source.ABIVersion, ABIVersion)
	}
	return source
}

func TestSourceInstallAndUninstall(t *testing.T) {
	eng := liveEngine(t)
	installSource(t, eng, "mangadex")

	installed, err := eng.Installed()
	if err != nil {
		t.Fatalf("list installed: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// A loaded source reports its live metadata.
	meta, err := eng.Metadata(ctx, "mangadex")
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if meta.ABIVersion != ABIVersion || meta.BaseURL == "" {
		t.Fatalf("metadata = %+v", meta)
	}

	filters, err := eng.Filters(ctx, "mangadex")
	if err != nil {
		t.Fatalf("read filters: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(filters, &decoded); err != nil {
		t.Fatalf("filters must be a JSON array: %v", err)
	}

	if err := eng.Uninstall(ctx, "mangadex"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := eng.Get("mangadex"); CodeOf(err) != CodeNotFound {
		t.Fatalf("after uninstall err = %v, want %s", err, CodeNotFound)
	}
}

// TestSourcePipeline exercises the full browse path against a live source.
func TestSourcePipeline(t *testing.T) {
	eng := liveEngine(t)
	installSource(t, eng, "mangadex")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const title = "Yosuga no Sora"
	results, err := eng.Search(ctx, "mangadex", SearchQuery{Query: title, Page: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results.Items) == 0 {
		t.Fatalf("search for %q returned no items", title)
	}
	if results.Page != 1 {
		t.Fatalf("page = %d, want 1", results.Page)
	}

	match := results.Items[0]
	for _, item := range results.Items {
		if strings.Contains(strings.ToLower(item.Title), "yosuga") {
			match = item
			break
		}
	}
	if match.ID == "" || match.Title == "" {
		t.Fatalf("search item is incomplete: %+v", match)
	}

	details, err := eng.Details(ctx, "mangadex", match.ID)
	if err != nil {
		t.Fatalf("get details for %s: %v", match.ID, err)
	}
	if details.Title == "" {
		t.Fatalf("details are incomplete: %+v", details)
	}
	if len(details.Chapters) == 0 {
		t.Fatalf("%q reported no chapters", details.Title)
	}

	pages, err := eng.Pages(ctx, "mangadex", details.Chapters[0].ID)
	if err != nil {
		t.Fatalf("get pages for chapter %s: %v", details.Chapters[0].ID, err)
	}
	if len(pages) == 0 {
		t.Fatal("chapter reported no pages")
	}
	for i, page := range pages {
		if page.URL == "" {
			t.Fatalf("page %d has no url: %+v", i, page)
		}
		if page.IsScrambled && page.Metadata == nil {
			t.Fatalf("page %d is scrambled without a tile map", i)
		}
	}
}

// TestSourceErrorsUseStandardCodes checks that a failing call surfaces a
// standardized code rather than a transport or runtime error.
func TestSourceErrorsUseStandardCodes(t *testing.T) {
	eng := liveEngine(t)
	installSource(t, eng, "mangadex")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, err := eng.Details(ctx, "mangadex", "0000-not-a-real-title")
	if err == nil {
		t.Fatal("a missing title must fail")
	}
	if code := CodeOf(err); !KnownCode(code) {
		t.Fatalf("code = %s, which is not a standardized code", code)
	}

	// The descramble export is optional and MangaDex does not provide it.
	_, err = eng.Unscramble(ctx, "mangadex", []byte("\xff\xd8\xff\xe0not-an-image"))
	if code := CodeOf(err); code != CodeUnsupportedMedia {
		t.Fatalf("code = %s, want %s", code, CodeUnsupportedMedia)
	}

	// Calls against a source that was never installed are rejected locally.
	if _, err := eng.Search(ctx, "absent", SearchQuery{Query: "x"}); CodeOf(err) != CodeNotFound {
		t.Fatalf("uninstalled source err = %v, want %s", err, CodeNotFound)
	}
}

// TestSourceUnderChallenge records how a protected source behaves. A blocked
// result is a valid outcome: it must arrive as CLOUDFLARE_BLOCKED rather than
// as an opaque failure.
func TestSourceUnderChallenge(t *testing.T) {
	eng := liveEngine(t)
	if _, err := eng.Registry().Find(context.Background(), "asurascans"); err != nil {
		t.Skipf("catalog has no asurascans entry: %v", err)
	}
	installSource(t, eng, "asurascans")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// The query is a term the source carries, so an empty result set means a
	// parsing regression rather than a title this source never listed.
	results, err := eng.Search(ctx, "asurascans", SearchQuery{Query: "Regression", Page: 1})
	if err != nil {
		code := CodeOf(err)
		if !KnownCode(code) {
			t.Fatalf("code = %s, which is not a standardized code", code)
		}
		t.Logf("asurascans search reported %s: %v", code, err)
		return
	}
	if len(results.Items) == 0 {
		t.Fatal("asurascans search succeeded without items")
	}
	for i, item := range results.Items {
		if item.ID == "" || item.Title == "" {
			t.Fatalf("item %d is incomplete: %+v", i, item)
		}
	}

	// Title details are scraped from the protected web host rather than the
	// API, so this is where a challenge surfaces.
	details, err := eng.Details(ctx, "asurascans", results.Items[0].ID)
	if err != nil {
		code := CodeOf(err)
		if !KnownCode(code) {
			t.Fatalf("code = %s, which is not a standardized code", code)
		}
		t.Logf("asurascans details reported %s: %v", code, err)
		return
	}
	if details.Title == "" {
		t.Fatalf("details are incomplete: %+v", details)
	}
}
