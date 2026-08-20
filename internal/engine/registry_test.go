package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeCatalog serves an index.json and the binaries it lists.
func fakeCatalog(t *testing.T, entries []RegistryEntry, binaries map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	for i := range entries {
		entries[i].WasmURL = server.URL + "/wasm/" + entries[i].ID + ".wasm"
	}
	index, err := json.Marshal(RegistryIndex{Version: 1, UpdatedAt: 1, Sources: entries})
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(index)
	})
	mux.HandleFunc("/wasm/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := binaries[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	})
	return server
}

func TestRegistryFetchVerifiesDigest(t *testing.T) {
	wasm := []byte("\x00asm\x01\x00\x00\x00trusted")
	entries := []RegistryEntry{{
		ID: "mangadex", Name: "MangaDex", Version: "1.0.0", ABIVersion: 1,
		Lang: "multi", BaseURL: "https://mangadex.org", SHA256: digest(wasm),
	}}
	server := fakeCatalog(t, entries, map[string][]byte{"mangadex.wasm": wasm})

	cacheDir := t.TempDir()
	registry := NewRegistry(server.URL+"/index.json", cacheDir)
	entry, err := registry.Find(context.Background(), "mangadex")
	if err != nil {
		t.Fatalf("find entry: %v", err)
	}

	path, got, err := registry.Fetch(context.Background(), entry)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(got) != string(wasm) {
		t.Fatal("fetched bytes differ from the served binary")
	}
	if want := filepath.Join(cacheDir, "wasm", "mangadex-v1.0.0.wasm"); path != want {
		t.Fatalf("cache path = %s, want %s", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("binary must be cached: %v", err)
	}
}

func TestRegistryFetchRejectsDigestMismatch(t *testing.T) {
	wasm := []byte("\x00asm\x01\x00\x00\x00tampered")
	entries := []RegistryEntry{{
		ID: "mangadex", Name: "MangaDex", Version: "1.0.0", ABIVersion: 1,
		SHA256: digest([]byte("expected different bytes")),
	}}
	server := fakeCatalog(t, entries, map[string][]byte{"mangadex.wasm": wasm})

	cacheDir := t.TempDir()
	registry := NewRegistry(server.URL+"/index.json", cacheDir)
	entry, err := registry.Find(context.Background(), "mangadex")
	if err != nil {
		t.Fatalf("find entry: %v", err)
	}

	if _, _, err := registry.Fetch(context.Background(), entry); err == nil {
		t.Fatal("a digest mismatch must be rejected")
	}
	// A rejected download must never reach the cache.
	if _, err := os.Stat(filepath.Join(cacheDir, "wasm", "mangadex-v1.0.0.wasm")); !os.IsNotExist(err) {
		t.Fatalf("unverified binary was cached: %v", err)
	}
}

func TestRegistryRejectsStaleCachedBinary(t *testing.T) {
	wasm := []byte("\x00asm\x01\x00\x00\x00current")
	entries := []RegistryEntry{{
		ID: "mangadex", Name: "MangaDex", Version: "1.0.0", ABIVersion: 1, SHA256: digest(wasm),
	}}
	server := fakeCatalog(t, entries, map[string][]byte{"mangadex.wasm": wasm})

	cacheDir := t.TempDir()
	registry := NewRegistry(server.URL+"/index.json", cacheDir)
	entry, err := registry.Find(context.Background(), "mangadex")
	if err != nil {
		t.Fatal(err)
	}

	// Seed the cache with a binary the catalog no longer describes.
	stale := filepath.Join(cacheDir, "wasm", "mangadex-v1.0.0.wasm")
	if err := writeFileAtomic(stale, []byte("stale")); err != nil {
		t.Fatal(err)
	}

	_, got, err := registry.Fetch(context.Background(), entry)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(got) != string(wasm) {
		t.Fatal("a cached binary whose digest no longer matches must be replaced")
	}
}

func TestValidateEntryRejectsForeignABIVersion(t *testing.T) {
	entry := RegistryEntry{
		ID: "future", Version: "2.0.0", ABIVersion: ABIVersion + 1,
		WasmURL: "https://example.test/future.wasm", SHA256: digest([]byte("x")),
	}
	err := validateEntry(entry)
	if err == nil {
		t.Fatal("an entry targeting another ABI version must be rejected")
	}
	if got := CodeOf(err); got != CodeUnsupportedMedia {
		t.Fatalf("code = %s, want %s", got, CodeUnsupportedMedia)
	}
}

func TestValidateEntryRequiresDigest(t *testing.T) {
	entry := RegistryEntry{ID: "nodigest", ABIVersion: ABIVersion, WasmURL: "https://example.test/x.wasm"}
	if err := validateEntry(entry); err == nil {
		t.Fatal("an entry without a sha256 digest must be rejected")
	}
	entry.SHA256 = "not-a-digest"
	if err := validateEntry(entry); err == nil {
		t.Fatal("a malformed digest must be rejected")
	}
}

func TestRuntimeFloorIsIndependentOfABIVersion(t *testing.T) {
	original := RuntimeVersion
	t.Cleanup(func() { RuntimeVersion = original })

	entry := RegistryEntry{
		ID: "needs-newer", ABIVersion: ABIVersion, MinRuntimeVersion: "2.1.0",
		WasmURL: "https://example.test/x.wasm", SHA256: digest([]byte("x")),
	}

	// A development build has no released version to compare against.
	RuntimeVersion = ""
	if err := validateEntry(entry); err != nil {
		t.Fatalf("floor must not apply to a development build: %v", err)
	}

	RuntimeVersion = "2.0.9"
	if err := validateEntry(entry); err == nil {
		t.Fatal("a runtime below the floor must be rejected")
	}

	RuntimeVersion = "2.1.0"
	if err := validateEntry(entry); err != nil {
		t.Fatalf("a runtime at the floor must be accepted: %v", err)
	}

	RuntimeVersion = "10.0.0"
	if err := validateEntry(entry); err != nil {
		t.Fatalf("version parts must compare numerically: %v", err)
	}
}

func TestRegistryIndexRejectsUnknownFormatVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":99,"updatedAt":1,"sources":[]}`)
	}))
	defer server.Close()

	registry := NewRegistry(server.URL+"/index.json", t.TempDir())
	if _, err := registry.Index(context.Background(), true); err == nil {
		t.Fatal("an unsupported index format version must be rejected")
	}
}

func TestLocalCatalogResolvesBinaryPaths(t *testing.T) {
	mirror := t.TempDir()
	wasm := []byte("\x00asm\x01\x00\x00\x00local")
	if err := os.MkdirAll(filepath.Join(mirror, "wasm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mirror, "wasm", "mangadex-v1.0.0.wasm"), wasm, 0o644); err != nil {
		t.Fatal(err)
	}
	index, err := json.Marshal(RegistryIndex{Version: 1, UpdatedAt: 1, Sources: []RegistryEntry{{
		ID: "mangadex", Name: "MangaDex", Version: "1.0.0", ABIVersion: 1,
		WasmURL: "https://makinuki.github.io/wasm/mangadex-v1.0.0.wasm", SHA256: digest(wasm),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(mirror, "index.json")
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry(indexPath, t.TempDir())
	entry, err := registry.Find(context.Background(), "mangadex")
	if err != nil {
		t.Fatalf("find entry: %v", err)
	}
	_, got, err := registry.Fetch(context.Background(), entry)
	if err != nil {
		t.Fatalf("fetch from a local mirror: %v", err)
	}
	if string(got) != string(wasm) {
		t.Fatal("bytes differ from the mirrored binary")
	}
}

func TestCachePathContainsPathTraversal(t *testing.T) {
	cacheDir := t.TempDir()
	registry := NewRegistry(DefaultRegistryURL, cacheDir)
	path := registry.CachePath(RegistryEntry{ID: "../../escape", Version: "1.0.0/../.."})

	dir, name := filepath.Split(path)
	if want := filepath.Join(cacheDir, "wasm") + string(filepath.Separator); dir != want {
		t.Fatalf("directory = %s, want %s", dir, want)
	}
	if filepath.Base(name) != name || name == "" {
		t.Fatalf("file name %q must not contain path separators", name)
	}
}
