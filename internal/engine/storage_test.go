package engine

import (
	"strings"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

func TestMemoryStorageRejectsOverCapWrites(t *testing.T) {
	store := NewMemoryStorage()

	if err := store.Set("mangadex", "token", strings.Repeat("a", StorageValueCap)); err != nil {
		t.Fatalf("write at the cap must be accepted: %v", err)
	}
	err := store.Set("mangadex", "token", strings.Repeat("a", StorageValueCap+1))
	if err == nil {
		t.Fatal("write above the cap must be rejected")
	}
	if got := CodeOf(err); got != CodeMemoryLimitExceeded {
		t.Fatalf("code = %s, want %s", got, CodeMemoryLimitExceeded)
	}

	// The rejected write must not replace the stored value.
	value, found, err := store.Get("mangadex", "token")
	if err != nil || !found {
		t.Fatalf("value must survive a rejected write: found=%v err=%v", found, err)
	}
	if len(value) != StorageValueCap {
		t.Fatalf("stored length = %d, want %d", len(value), StorageValueCap)
	}
}

func TestMemoryStorageDistinguishesMissingFromEmpty(t *testing.T) {
	store := NewMemoryStorage()

	if _, found, _ := store.Get("mangadex", "absent"); found {
		t.Fatal("an unwritten key must report as missing")
	}
	if err := store.Set("mangadex", "present", ""); err != nil {
		t.Fatalf("storing an empty value: %v", err)
	}
	value, found, err := store.Get("mangadex", "present")
	if err != nil || !found {
		t.Fatalf("an empty value must report as found: found=%v err=%v", found, err)
	}
	if value != "" {
		t.Fatalf("value = %q, want empty", value)
	}
}

func TestStorageNamespacesPerSource(t *testing.T) {
	store := NewMemoryStorage()
	if err := store.Set("mangadex", "token", "one"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("asurascans", "token", "two"); err != nil {
		t.Fatal(err)
	}

	for source, want := range map[string]string{"mangadex": "one", "asurascans": "two"} {
		value, found, err := store.Get(source, "token")
		if err != nil || !found {
			t.Fatalf("%s: found=%v err=%v", source, found, err)
		}
		if value != want {
			t.Fatalf("%s: value = %q, want %q", source, value, want)
		}
	}
}

func TestTruncateToCapKeepsValidUTF8(t *testing.T) {
	// A multi byte rune straddling the cap boundary must not be split.
	value := strings.Repeat("a", StorageValueCap-1) + "é"
	got := truncateToCap(value)
	if len(got) != StorageValueCap-1 {
		t.Fatalf("length = %d, want %d", len(got), StorageValueCap-1)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatal("truncation produced an invalid rune")
	}
}

func TestSQLStorageRoundTrip(t *testing.T) {
	handle, err := db.Open(t.TempDir() + "/makidoku.db")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer handle.Close()

	// plugin_storage references sources, so the source row must exist first.
	if _, err := handle.Exec(
		`INSERT INTO sources(id, name, version, abi_version, lang, base_url, wasm_path, installed_at)
		 VALUES('mangadex', 'MangaDex', '1.0.0', 1, 'multi', 'https://mangadex.org', 'mangadex.wasm', 0)`); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	store := NewSQLStorage(handle)
	if _, found, err := store.Get("mangadex", "session"); err != nil || found {
		t.Fatalf("unwritten key: found=%v err=%v", found, err)
	}
	if err := store.Set("mangadex", "session", "first"); err != nil {
		t.Fatalf("insert value: %v", err)
	}
	if err := store.Set("mangadex", "session", "second"); err != nil {
		t.Fatalf("update value: %v", err)
	}

	value, found, err := store.Get("mangadex", "session")
	if err != nil || !found {
		t.Fatalf("read value: found=%v err=%v", found, err)
	}
	if value != "second" {
		t.Fatalf("value = %q, want %q", value, "second")
	}

	if err := store.Set("mangadex", "session", strings.Repeat("a", StorageValueCap+1)); CodeOf(err) != CodeMemoryLimitExceeded {
		t.Fatalf("over cap write: err = %v", err)
	}
}
