package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	levelVar = new(slog.LevelVar)
	once     sync.Once
	fileOnce sync.Once
	// fileSink is the rotating writer, kept so the daemon can close it on shutdown.
	fileSink *lumberjack.Logger
)

// Rotation policy. The cap bounds one file and the backup count bounds the whole
// directory, so an install cannot fill a disk through logging alone.
const (
	maxLogMegabytes = 5
	logBackups      = 3
)

// FileName is the log file inside the data directory.
const FileName = "makidoku.log"

func init() {
	once.Do(func() {
		levelVar.Set(slog.LevelInfo)
		slog.SetDefault(slog.New(newFanout(os.Stderr, nil)))
	})
}

// FilePath returns the log file path for a data directory.
func FilePath(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// OpenFile attaches a rotating log file beside the database, in addition to the
// existing stderr output. Both sinks then follow the one level the owner sets, so
// a run at debug leaves debug records on disk rather than only on the terminal.
//
// A failure to open the file is returned rather than swallowed. Silently losing the
// log is the one outcome that would make a later report unexplainable, and the
// directory is the daemon's own, so a failure here means something is already wrong.
func OpenFile(dataDir string) error {
	var err error
	fileOnce.Do(func() {
		if mkErr := os.MkdirAll(dataDir, 0o755); mkErr != nil {
			err = fmt.Errorf("create data directory for the log: %w", mkErr)
			return
		}
		fileSink = &lumberjack.Logger{
			Filename:   FilePath(dataDir),
			MaxSize:    maxLogMegabytes,
			MaxBackups: logBackups,
			// Age is left unset so the backup count alone governs retention. A
			// desktop install keeps a handful of recent files regardless of age,
			// which is what a person reading yesterday's failure wants.
			Compress: false,
		}
		slog.SetDefault(slog.New(newFanout(os.Stderr, fileSink)))
	})
	return err
}

// CloseFile releases the rotating writer. The daemon calls it on shutdown so the
// last record is flushed and the file handle is not left open.
func CloseFile() error {
	if fileSink == nil {
		return nil
	}
	return fileSink.Close()
}

// CurrentLevel reports the level in force for both sinks, so the startup banner
// can record it and a reader can tell which records the level admits.
func CurrentLevel() slog.Level { return levelVar.Level() }

// LogFilePath reports where records are being written, or an empty string when no
// file is open. The startup banner uses it so a person reading the terminal knows
// where the file is without having to look it up.
func LogFilePath() string {
	if fileSink == nil {
		return ""
	}
	return fileSink.Filename
}

// fanout writes one record to several handlers as if it were one.
type fanout struct {
	handlers []slog.Handler
}

// newFanout builds a handler writing to stderr and, when file is non-nil, to the
// rotating file as well.
func newFanout(stderr *os.File, file *lumberjack.Logger) slog.Handler {
	out := []slog.Handler{slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: levelVar})}
	if file != nil {
		out = append(out, slog.NewTextHandler(file, &slog.HandlerOptions{Level: levelVar}))
	}
	return &fanout{handlers: out}
}

func (f *fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f *fanout) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		// One failing sink must not stop the others, or a full disk would silence
		// the terminal as well as the file.
		if err := h.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f *fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &fanout{handlers: next}
}

func (f *fanout) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return &fanout{handlers: next}
}

// ParseLevel maps the persisted setting value to a slog level.
func ParseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, nil
	}
}

// SetLevel updates the global daemon log level. It is safe to call after init.
func SetLevel(raw string) error {
	level, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	levelVar.Set(level)
	return nil
}

// SetLevelFromRaw accepts the JSON-encoded setting value such as "\"info\"".
func SetLevelFromRaw(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	return SetLevel(trimmed)
}

// Level returns the current level.
func Level() slog.Level { return levelVar.Level() }
