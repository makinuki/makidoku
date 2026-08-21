package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/makinuki/makidoku/internal/engine"
)

type fakeInstallRunner struct {
	installedID   string
	installedPath string
	source        engine.InstalledSource
	err           error
}

func (f *fakeInstallRunner) Install(_ context.Context, id string) (engine.InstalledSource, error) {
	f.installedID = id
	return f.source, f.err
}

func (f *fakeInstallRunner) InstallFile(_ context.Context, path string) (engine.InstalledSource, error) {
	f.installedPath = path
	return f.source, f.err
}

func TestParseInstallArgsRequiresOneTarget(t *testing.T) {
	if _, _, err := parseInstallArgs("", ""); err == nil {
		t.Fatal("expected error when neither id nor path is set")
	}
	if _, _, err := parseInstallArgs("mangadex", "./mangadex.wasm"); err == nil {
		t.Fatal("expected error when both id and path are set")
	}
}

func TestParseInstallArgsNormalizesAndSelects(t *testing.T) {
	id, path, err := parseInstallArgs("  mangadex  ", "")
	if err != nil {
		t.Fatalf("parseInstallArgs: %v", err)
	}
	if id != "mangadex" || path != "" {
		t.Fatalf("got id %q path %q, want mangadex and empty", id, path)
	}
	_, path, err = parseInstallArgs("", "  ./custom.wasm  ")
	if err != nil {
		t.Fatalf("parseInstallArgs: %v", err)
	}
	if path != "./custom.wasm" {
		t.Fatalf("path = %q, want ./custom.wasm", path)
	}
}

func TestExecuteInstallUsesCatalogID(t *testing.T) {
	runner := &fakeInstallRunner{source: engine.InstalledSource{ID: "mangadex"}}
	if _, err := executeInstall(context.Background(), runner, "mangadex", ""); err != nil {
		t.Fatalf("executeInstall: %v", err)
	}
	if runner.installedID != "mangadex" {
		t.Fatalf("installed id = %q, want mangadex", runner.installedID)
	}
	if runner.installedPath != "" {
		t.Fatalf("unexpected path install %q", runner.installedPath)
	}
}

func TestExecuteInstallPrefersLocalPath(t *testing.T) {
	runner := &fakeInstallRunner{source: engine.InstalledSource{ID: "custom"}}
	if _, err := executeInstall(context.Background(), runner, "ignored", "./custom.wasm"); err != nil {
		t.Fatalf("executeInstall: %v", err)
	}
	if runner.installedPath != "./custom.wasm" {
		t.Fatalf("installed path = %q, want ./custom.wasm", runner.installedPath)
	}
	if runner.installedID != "" {
		t.Fatalf("unexpected catalog install %q", runner.installedID)
	}
}

func TestExecuteInstallPropagatesError(t *testing.T) {
	want := errors.New("digest mismatch")
	runner := &fakeInstallRunner{err: want}
	if _, err := executeInstall(context.Background(), runner, "mangadex", ""); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
