package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/identity"
)

func TestInstalledSourceUsesOpaqueMakidokuID(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	sourceID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?)`, sourceID, "mangadex", "MangaDex", "1", ABIVersion, "multi", "https://mangadex.org", "missing.wasm", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	eng := New(handle, Options{DataDir: t.TempDir()})
	items, err := eng.Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != sourceID {
		t.Fatalf("installed sources = %+v, want opaque ID %q", items, sourceID)
	}
}
