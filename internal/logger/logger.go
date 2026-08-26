package logger

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	levelVar = new(slog.LevelVar)
	once     sync.Once
)

func init() {
	once.Do(func() {
		levelVar.Set(slog.LevelInfo)
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: levelVar})
		slog.SetDefault(slog.New(handler))
	})
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
