package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/natefinch/lumberjack.v2"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"INFO":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"":      slog.LevelInfo,
	}
	for raw, want := range cases {
		got, err := ParseLevel(raw)
		if err != nil {
			t.Fatalf("ParseLevel(%q) returned %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseLevel(%q) = %v, want %v", raw, got, want)
		}
	}
}

// The level variable drives both sinks together. If they diverged, a debugging
// run would leave a file without the debug records that were being asked for,
// which is the failure this design exists to prevent.
func TestCurrentLevelFollowsTheSharedVariable(t *testing.T) {
	original := CurrentLevel()
	t.Cleanup(func() { levelVar.Set(original) })

	levelVar.Set(slog.LevelDebug)
	if CurrentLevel() != slog.LevelDebug {
		t.Fatal("expected the current level to follow the shared variable")
	}
	levelVar.Set(slog.LevelWarn)
	if CurrentLevel() != slog.LevelWarn {
		t.Fatal("expected the current level to follow the shared variable")
	}
}

// A record must reach every sink the level admits. One sink failing, such as a
// full disk, must not silence the other.
func TestFanoutWritesToEverySink(t *testing.T) {
	levelVar.Set(slog.LevelDebug)
	t.Cleanup(func() { levelVar.Set(slog.LevelInfo) })

	var stderr, file strings.Builder
	handler := &fanout{handlers: []slog.Handler{
		slog.NewTextHandler(&stderr, &slog.HandlerOptions{Level: levelVar}),
		slog.NewTextHandler(&file, &slog.HandlerOptions{Level: levelVar}),
	}}
	slog.New(handler).InfoContext(t.Context(), "banner", "version", "0.1.0")

	for name, sink := range map[string]string{"stderr": stderr.String(), "file": file.String()} {
		if !strings.Contains(sink, "banner") {
			t.Fatalf("the %s sink did not receive the record", name)
		}
		if !strings.Contains(sink, "0.1.0") {
			t.Fatalf("the %s sink lost the record fields", name)
		}
	}
}

// The level gates both sinks identically, so a debug record is absent from the
// file while the level excludes it.
func TestFanoutRespectsTheLevel(t *testing.T) {
	levelVar.Set(slog.LevelInfo)
	t.Cleanup(func() { levelVar.Set(slog.LevelInfo) })

	var file strings.Builder
	handler := &fanout{handlers: []slog.Handler{
		slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: levelVar}),
		slog.NewTextHandler(&file, &slog.HandlerOptions{Level: levelVar}),
	}}
	slog.New(handler).DebugContext(t.Context(), "per-request detail", "source", "example")

	if strings.Contains(file.String(), "per-request detail") {
		t.Fatal("a debug record reached the file while the level excluded it")
	}
}

// WithAttrs must reach every sink, since the daemon and the engine both log with
// pre-bound attributes.
func TestFanoutPropagatesAttrsToEverySink(t *testing.T) {
	levelVar.Set(slog.LevelDebug)
	t.Cleanup(func() { levelVar.Set(slog.LevelInfo) })

	var first, second strings.Builder
	handler := &fanout{handlers: []slog.Handler{
		slog.NewTextHandler(&first, &slog.HandlerOptions{Level: levelVar}),
		slog.NewTextHandler(&second, &slog.HandlerOptions{Level: levelVar}),
	}}
	slog.New(handler.WithAttrs([]slog.Attr{slog.String("source", "example")})).
		InfoContext(t.Context(), "record")

	for name, sink := range map[string]string{"first": first.String(), "second": second.String()} {
		if !strings.Contains(sink, "source=example") {
			t.Fatalf("the %s sink lost the bound attributes", name)
		}
	}
}

// The file is created beside the data directory, and the directory itself when
// it is missing, so a fresh install produces a log rather than none.
func TestOpenFileCreatesTheLogBesideTheDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "makidoku")
	if err := OpenFile(dir); err != nil {
		t.Fatalf("OpenFile returned %v", err)
	}
	t.Cleanup(func() { _ = CloseFile() })

	slog.Info("startup record", "version", "0.1.0")
	if err := CloseFile(); err != nil {
		t.Fatalf("CloseFile returned %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("reading the log file: %v", err)
	}
	if !strings.Contains(string(contents), "startup record") {
		t.Fatal("the startup record did not reach the file")
	}
}

// Rotation is the property that bounds the whole directory, so it is exercised
// rather than assumed. The cap is lowered for the test so the check does not have
// to write five megabytes.
//
// The backup count is deliberately not asserted. On Windows the previous file
// cannot be renamed while a reader holds it open, and lumberjack tolerates that
// rather than failing, so asserting a count would make the test depend on the
// machine rather than on the behaviour.
func TestFileRotatesAtTheCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rotating.log")

	sink := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    1,
		MaxBackups: 3,
	}
	// Lumberjack rotates when a write would take the file past the cap, so the
	// volume has to exceed the cap. The cap is lowered to one megabyte to keep
	// this quick, which means writing well over that.
	chunk := []byte(strings.Repeat("x", 64*1024))
	for i := 0; i < 80; i++ {
		if _, err := sink.Write(chunk); err != nil {
			t.Fatalf("write %d returned %v", i, err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}

	backups, err := filepath.Glob(filepath.Join(dir, "rotating-*.log"))
	if err != nil {
		t.Fatalf("Glob returned %v", err)
	}
	if len(backups) == 0 {
		t.Fatal("no rotated file was produced after exceeding the cap")
	}
	if len(backups) > 3 {
		t.Fatalf("rotation kept %d backups, want at most the configured 3", len(backups))
	}
}

// discard swallows writes. It keeps the level tests off the terminal.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
