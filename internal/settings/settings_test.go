package settings

import (
	"path/filepath"
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
