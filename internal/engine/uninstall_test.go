package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/identity"
)

func offlineEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "engine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	sourceID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`INSERT INTO sources(id,plugin_key,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,'Demo','1',1,'en','https://demo.test','',?)`, sourceID, "demo", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	return New(handle, Options{DataDir: t.TempDir()}), sourceID
}

// Uninstalling through the plugin key must still evict the cached instance,
// which is keyed by the canonical source id.
func TestUninstallEvictsCachedPluginByCanonicalID(t *testing.T) {
	engine, sourceID := offlineEngine(t)
	engine.plugins[sourceID] = &loadedPlugin{id: sourceID}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Uninstall(ctx, "demo"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	engine.mu.Lock()
	_, loaded := engine.plugins[sourceID]
	engine.mu.Unlock()
	if loaded {
		t.Fatal("uninstall left the cached instance loaded")
	}
}

// A removed source must stop answering even if one of its instances is still
// cached.
func TestPluginRefusesUninstalledSource(t *testing.T) {
	engine, sourceID := offlineEngine(t)
	engine.plugins[sourceID] = &loadedPlugin{id: sourceID}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Uninstall(ctx, sourceID); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	_, err := engine.plugin(ctx, sourceID)
	if err == nil {
		t.Fatal("served a removed source from the cache")
	}
	if CodeOf(err) != CodeNotFound {
		t.Fatalf("plugin after uninstall = %v, want not found", err)
	}
}
