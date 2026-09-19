package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/makinuki/makidoku/internal/db"
)

func TestServiceUsesDefaultsAndValidatesWrites(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	repo := db.NewRepository(handle)
	service := New(repo)
	value, err := service.Get("library.update_interval")
	if err != nil || value != "86400000000000" {
		t.Fatalf("default interval = %q, err = %v", value, err)
	}
	if err := service.Set("reader.default_mode", `"double"`); err != nil {
		t.Fatal(err)
	}
	value, err = service.Get("reader.default_mode")
	if err != nil || value != `"double"` {
		t.Fatalf("stored mode = %q, err = %v", value, err)
	}
	if err := service.Set("reader.default_mode", `"invalid"`); err == nil {
		t.Fatal("accepted invalid reader mode")
	}
	if err := service.Set("unknown.key", `true`); err == nil {
		t.Fatal("accepted unknown setting")
	}
}

func TestReaderDisplaySettingsValidate(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "reader-display.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	service := New(db.NewRepository(handle))
	valid := map[string]string{
		"reader.fit":        `"screen"`,
		"reader.navigation": `"edge"`,
		"reader.webtoon_gap": "16",
		"reader.theme":      `"paper"`,
		"reader.brightness": "120",
		"reader.grayscale":  "true",
		"reader.invert":     "true",
	}
	for key, value := range valid {
		if err := service.Set(key, value); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	invalid := map[string]string{
		"reader.fit":        `"cover"`,
		"reader.navigation": `"corners"`,
		"reader.webtoon_gap": "64",
		"reader.theme":      `"sepia"`,
		"reader.brightness": "40",
	}
	for key, value := range invalid {
		if err := service.Set(key, value); err == nil {
			t.Fatalf("accepted invalid %s = %s", key, value)
		}
	}
}

// Library view state is stored and validated like any other setting, but the
// library screen owns it, so it stays out of the settings list.
func TestLibraryViewSettingsAreHiddenAndValidated(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "library-view.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	service := New(db.NewRepository(handle))

	if err := service.Set("library.view.filter_status", `"ongoing,completed"`); err != nil {
		t.Fatalf("store status filter: %v", err)
	}
	if err := service.Set("library.view.filter_status", `"ongoing,bogus"`); err == nil {
		t.Fatal("accepted an unsupported status filter")
	}
	if err := service.Set("library.view.filter_sources", `"source-a,source_b"`); err != nil {
		t.Fatalf("store source filter: %v", err)
	}
	if err := service.Set("library.view.filter_sources", `"source a"`); err == nil {
		t.Fatal("accepted an invalid source filter")
	}
	if err := service.Set("library.view.card_size", `"huge"`); err == nil {
		t.Fatal("accepted an unsupported card size")
	}

	entries, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	hidden := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Key, "library.view.") {
			if entry.Hidden {
				t.Fatalf("setting %s is hidden", entry.Key)
			}
			continue
		}
		hidden++
		if !entry.Hidden {
			t.Fatalf("view setting %s is not hidden", entry.Key)
		}
	}
	if hidden != 10 {
		t.Fatalf("hidden view settings = %d, want 10", hidden)
	}
}
