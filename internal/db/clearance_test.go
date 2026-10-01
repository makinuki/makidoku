package db

import "testing"

// putBundle stores a bundle for a source and origin, failing the test on error.
func putBundle(t *testing.T, repo *Repository, bundle ClearanceBundle) {
	t.Helper()
	if err := repo.PutClearanceBundle(bundle); err != nil {
		t.Fatalf("put bundle for %s: %v", bundle.Origin, err)
	}
}

func testBundle(origin string, cookies map[string]string) ClearanceBundle {
	return ClearanceBundle{
		SourceID:  "mangadex",
		Origin:    origin,
		Cookies:   cookies,
		UserAgent: "Mozilla/5.0 test",
	}
}

// TestClearanceBundleRoundTrip covers storing a bundle and reading it back,
// including the companion cookies that must travel with the clearance cookie.
func TestClearanceBundleRoundTrip(t *testing.T) {
	repo := testRepository(t)

	absent, found, err := repo.GetClearanceBundle("mangadex", "mangadex.org")
	if err != nil {
		t.Fatalf("read missing bundle: %v", err)
	}
	if found || absent != nil {
		t.Fatal("an absent bundle must report found as false and return no value")
	}

	hint := int64(1780000000)
	stored := testBundle("mangadex.org", map[string]string{"cf_clearance": "abc", "__cf_bm": "def"})
	stored.SecChUa = map[string]string{"sec-ch-ua": `"Chromium";v="154"`}
	stored.BrowserProfile = "chrome_154"
	stored.ExpiresHint = &hint
	stored.Generation = 1
	stored.Status = ClearanceUnknown
	putBundle(t, repo, stored)

	got, found, err := repo.GetClearanceBundle("mangadex", "mangadex.org")
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if !found {
		t.Fatal("stored bundle was not found")
	}
	if !got.HasClearance() {
		t.Fatal("bundle must report a clearance cookie")
	}
	if got.Cookies["__cf_bm"] != "def" {
		t.Fatalf("companion cookie lost: %+v", got.Cookies)
	}
	if got.UserAgent != "Mozilla/5.0 test" {
		t.Fatalf("user agent = %q", got.UserAgent)
	}
	if got.SecChUa["sec-ch-ua"] == "" {
		t.Fatal("client hints lost")
	}
	if got.BrowserProfile != "chrome_154" {
		t.Fatalf("browser profile = %q", got.BrowserProfile)
	}
	if got.ExpiresHint == nil || *got.ExpiresHint != hint {
		t.Fatalf("expires hint = %v", got.ExpiresHint)
	}
	// The rendered header keeps both cookies so a replay does not drop the
	// companion cookie the working session depends on.
	if header := got.CookieHeader(); header != "__cf_bm=def; cf_clearance=abc" {
		t.Fatalf("cookie header = %q", header)
	}
}

// TestClearanceBundlesAreSeparatePerOrigin checks that two origins on one
// source do not overwrite each other, which is the case the per-origin key
// exists for.
func TestClearanceBundlesAreSeparatePerOrigin(t *testing.T) {
	repo := testRepository(t)

	want := map[string]string{"mangafire.to": "page-cookie", "mfcdn.nl": "image-cookie"}
	for origin, cookie := range want {
		putBundle(t, repo, testBundle(origin, map[string]string{"cf_clearance": cookie}))
	}

	bundles, err := repo.ListClearanceBundles("mangadex")
	if err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2", len(bundles))
	}
	for _, bundle := range bundles {
		if bundle.Cookies["cf_clearance"] != want[bundle.Origin] {
			t.Fatalf("origin %s cookie = %q, want %q",
				bundle.Origin, bundle.Cookies["cf_clearance"], want[bundle.Origin])
		}
	}
}

// TestTouchClearanceAdvancesGenerationOnlyForOlderSolve covers the rule that a
// burst of parallel failures cannot discard a solve that has since replaced
// theirs.
func TestTouchClearanceAdvancesGenerationOnlyForOlderSolve(t *testing.T) {
	repo := testRepository(t)
	stored := testBundle("mangadex.org", map[string]string{"cf_clearance": "first"})
	stored.Generation = 1
	putBundle(t, repo, stored)

	// A caller holding generation 1 marks the bundle challenged, which advances
	// the generation so the remaining parallel failures are one event.
	if err := repo.TouchClearanceChallenge("mangadex", "mangadex.org", 1); err != nil {
		t.Fatalf("touch challenge: %v", err)
	}
	bundle, _, err := repo.GetClearanceBundle("mangadex", "mangadex.org")
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if bundle.Status != ClearanceChallenged {
		t.Fatalf("status = %q, want %q", bundle.Status, ClearanceChallenged)
	}
	if bundle.Generation != 2 {
		t.Fatalf("generation = %d, want 2", bundle.Generation)
	}
	if bundle.LastChallengeAt == nil {
		t.Fatal("last challenge was not recorded")
	}

	// A later caller still holding generation 1 must not advance it again.
	if err := repo.TouchClearanceChallenge("mangadex", "mangadex.org", 1); err != nil {
		t.Fatalf("second touch: %v", err)
	}
	again, _, err := repo.GetClearanceBundle("mangadex", "mangadex.org")
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if again.Generation != 2 {
		t.Fatalf("generation advanced twice: %d", again.Generation)
	}
}

// TestTouchClearanceSuccessRecordsUsable covers the only reliable evidence that
// a bundle still works.
func TestTouchClearanceSuccessRecordsUsable(t *testing.T) {
	repo := testRepository(t)
	putBundle(t, repo, testBundle("mangadex.org", map[string]string{"cf_clearance": "abc"}))

	if err := repo.TouchClearanceSuccess("mangadex", "mangadex.org"); err != nil {
		t.Fatalf("touch success: %v", err)
	}
	bundle, _, err := repo.GetClearanceBundle("mangadex", "mangadex.org")
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if bundle.Status != ClearanceUsable {
		t.Fatalf("status = %q, want %q", bundle.Status, ClearanceUsable)
	}
	if bundle.LastSuccessAt == nil {
		t.Fatal("last success was not recorded")
	}
}

// TestDeleteClearanceBundle checks removal of a single origin and of a whole
// source.
func TestDeleteClearanceBundle(t *testing.T) {
	repo := testRepository(t)
	putBundle(t, repo, testBundle("a.example", map[string]string{"cf_clearance": "a"}))
	putBundle(t, repo, testBundle("b.example", map[string]string{"cf_clearance": "b"}))

	if err := repo.DeleteClearanceBundle("mangadex", "a.example"); err != nil {
		t.Fatalf("delete one: %v", err)
	}
	bundles, err := repo.ListClearanceBundles("mangadex")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bundles) != 1 || bundles[0].Origin != "b.example" {
		t.Fatalf("remaining = %+v", bundles)
	}

	// Deleting an absent row is not an error.
	if err := repo.DeleteClearanceBundle("mangadex", "missing.example"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}

	if err := repo.DeleteSourceClearanceBundles("mangadex"); err != nil {
		t.Fatalf("delete for source: %v", err)
	}
	bundles, err = repo.ListClearanceBundles("mangadex")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(bundles) != 0 {
		t.Fatalf("bundles remain after a source delete: %+v", bundles)
	}
}

// TestSourceHasClearanceIgnoresBundlesWithoutIt checks the reporting helper, so
// a bundle holding only companion cookies does not read as solved.
func TestSourceHasClearanceIgnoresBundlesWithoutIt(t *testing.T) {
	repo := testRepository(t)

	has, err := repo.SourceHasClearance("mangadex")
	if err != nil {
		t.Fatalf("check absent: %v", err)
	}
	if has {
		t.Fatal("a source with no bundle must not report clearance")
	}

	putBundle(t, repo, testBundle("mangadex.org", map[string]string{"session": "only"}))
	has, err = repo.SourceHasClearance("mangadex")
	if err != nil {
		t.Fatalf("check present: %v", err)
	}
	if has {
		t.Fatal("a bundle without a clearance cookie must not report clearance")
	}
}
