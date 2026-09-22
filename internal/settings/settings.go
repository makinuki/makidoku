package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

type Definition struct {
	Key         string
	Type        string
	Default     string
	Description string
	Validate    func(any) error
	// Hidden keeps view state that a screen owns out of the settings list
	// while the value is still stored, validated and returned to clients.
	Hidden bool
}

type Entry struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Default     string `json:"default"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Hidden      bool   `json:"hidden"`
}

type Service struct {
	repo  *db.Repository
	mu    sync.RWMutex
	cache map[string]string
}

func New(repo *db.Repository) *Service { return &Service{repo: repo} }

func (s *Service) Get(key string) (string, error) {
	definition, ok := definitions[key]
	if !ok {
		return "", fmt.Errorf("unknown setting %q", key)
	}
	s.mu.RLock()
	value, cached := s.cache[key]
	s.mu.RUnlock()
	if cached {
		return value, nil
	}
	setting, err := s.repo.GetSetting(key)
	if err != nil {
		value = definition.Default
	} else {
		value = setting.Value
	}
	value = migrateValue(key, value)
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]string{}
	}
	s.cache[key] = value
	s.mu.Unlock()
	return value, nil
}

func (s *Service) Set(key, raw string) error {
	definition, ok := definitions[key]
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fmt.Errorf("setting %s must be valid JSON: %w", key, err)
	}
	if definition.Validate != nil {
		if err := definition.Validate(value); err != nil {
			return fmt.Errorf("invalid setting %s: %w", key, err)
		}
	}
	if err := s.repo.SetSetting(key, raw); err != nil {
		return err
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]string{}
	}
	s.cache[key] = raw
	s.mu.Unlock()
	return nil
}

func (s *Service) List() ([]Entry, error) {
	entries := make([]Entry, 0, len(definitions))
	for _, definition := range definitionList {
		value, err := s.Get(definition.Key)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{Key: definition.Key, Value: value, Default: definition.Default, Type: definition.Type, Description: definition.Description, Hidden: definition.Hidden})
	}
	return entries, nil
}

func (s *Service) Reset(keys []string, all bool) error {
	if all {
		keys = make([]string, 0, len(definitions))
		for key := range definitions {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		definition, ok := definitions[key]
		if !ok {
			return fmt.Errorf("unknown setting %q", key)
		}
		if err := s.repo.SetSetting(key, definition.Default); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.cache = map[string]string{}
	s.mu.Unlock()
	return nil
}

func (s *Service) Duration(key string) (time.Duration, error) {
	raw, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	var value int64
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return 0, err
	}
	return time.Duration(value), nil
}

func (s *Service) Bool(key string) (bool, error) {
	raw, err := s.Get(key)
	if err != nil {
		return false, err
	}
	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return false, err
	}
	return value, nil
}

func (s *Service) Int(key string) (int, error) {
	raw, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	var value int
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return 0, err
	}
	return value, nil
}

func (s *Service) String(key string) (string, error) {
	raw, err := s.Get(key)
	if err != nil {
		return "", err
	}
	var value string
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "", err
	}
	return value, nil
}

func number(min, max float64) func(any) error {
	return func(value any) error {
		n, ok := value.(float64)
		if !ok || n < min || n > max {
			return errors.New("number is outside the allowed range")
		}
		return nil
	}
}

func enum(values ...string) func(any) error {
	return func(value any) error {
		text, ok := value.(string)
		if !ok {
			return errors.New("value must be a string")
		}
		for _, item := range values {
			if text == item {
				return nil
			}
		}
		return errors.New("value is not supported")
	}
}

func boolean(value any) error {
	if _, ok := value.(bool); !ok {
		return errors.New("value must be boolean")
	}
	return nil
}

// navigationPresetRenames maps retired tap zone preset names onto their
// replacements. Stored values predate the rename and keep working by mapping
// on read; writes validate against the new vocabulary only.
var navigationPresetRenames = map[string]string{
	"default": "default-manga",
	"l":       "l-shaped",
	"edge":    "edge-only",
}

func migrateValue(key, value string) string {
	if key != "reader.navigation" {
		return value
	}
	var name string
	if err := json.Unmarshal([]byte(value), &name); err != nil {
		return value
	}
	renamed, ok := navigationPresetRenames[name]
	if !ok {
		return value
	}
	migrated, err := json.Marshal(renamed)
	if err != nil {
		return value
	}
	return string(migrated)
}

// listOf accepts a comma-separated selection drawn from values. An empty
// string is a valid selection and means nothing is selected.
func listOf(values ...string) func(any) error {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	return func(value any) error {
		text, ok := value.(string)
		if !ok {
			return errors.New("value must be a string")
		}
		for _, item := range strings.Split(text, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := allowed[item]; !ok {
				return errors.New("value is not supported")
			}
		}
		return nil
	}
}

// identifierList accepts a comma-separated selection of record identifiers,
// which are opaque to the settings layer and cannot be checked against a fixed
// vocabulary.
func identifierList() func(any) error {
	return func(value any) error {
		text, ok := value.(string)
		if !ok {
			return errors.New("value must be a string")
		}
		for _, item := range strings.Split(text, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			for _, char := range item {
				valid := char == '-' || char == '_' ||
					(char >= '0' && char <= '9') ||
					(char >= 'a' && char <= 'z') ||
					(char >= 'A' && char <= 'Z')
				if !valid {
					return errors.New("value must be a list of identifiers")
				}
			}
		}
		return nil
	}
}

var definitionList = []Definition{
	{Key: "appearance.date_format", Type: "string", Default: `"relative"`, Description: "How dates and times are shown", Validate: enum("relative", "absolute")},
	{Key: "library.update_interval", Type: "duration", Default: "86400000000000", Description: "How often the library checks for new chapters", Validate: number(0, 30*24*60*60*1e9)},
	{Key: "library.update_on_launch", Type: "boolean", Default: "false", Description: "Check for new chapters when the app starts", Validate: func(v any) error {
		if _, ok := v.(bool); !ok {
			return errors.New("value must be boolean")
		}
		return nil
	}},
	{Key: "reader.default_mode", Type: "string", Default: `"single"`, Description: "Reading mode used when a chapter opens", Validate: enum("single", "double", "webtoon")},
	{Key: "reader.direction", Type: "string", Default: `"ltr"`, Description: "Reader page direction", Validate: enum("ltr", "rtl")},
	{Key: "reader.fit", Type: "string", Default: `"width"`, Description: "How pages fit the screen", Validate: enum("width", "height", "screen", "original")},
	{Key: "reader.navigation", Type: "string", Default: `"default-manga"`, Description: "Tap and click zones in paged reading", Validate: enum("default-manga", "l-shaped", "edge-only", "disabled")},
	{Key: "reader.webtoon_gap", Type: "number", Default: "8", Description: "Space between pages in webtoon mode, in pixels", Validate: number(0, 48)},
	{Key: "reader.theme", Type: "string", Default: `"dark"`, Description: "Reader color theme", Validate: enum("dark", "amoled", "paper", "light")},
	{Key: "reader.brightness", Type: "number", Default: "100", Description: "Reader image brightness in percent", Validate: number(50, 150)},
	{Key: "reader.grayscale", Type: "boolean", Default: "false", Description: "Show reader images in grayscale", Validate: boolean},
	{Key: "reader.invert", Type: "boolean", Default: "false", Description: "Invert reader image colors", Validate: boolean},
	{Key: "downloads.auto_download", Type: "boolean", Default: "false", Description: "Automatically download new chapters", Validate: func(v any) error {
		if _, ok := v.(bool); !ok {
			return errors.New("value must be boolean")
		}
		return nil
	}},
	{Key: "downloads.download_ahead", Type: "number", Default: "0", Description: "Upcoming chapters to keep downloaded while you read", Validate: number(0, 10)},
	{Key: "downloads.concurrent", Type: "number", Default: "2", Description: "How many chapters download at the same time", Validate: number(1, 16)},
	{Key: "tracking.auto_sync", Type: "boolean", Default: "true", Description: "Sync progress with trackers in the background", Validate: func(v any) error {
		if _, ok := v.(bool); !ok {
			return errors.New("value must be boolean")
		}
		return nil
	}},
	{Key: "backup.auto_interval", Type: "duration", Default: "0", Description: "How often automatic backups are created", Validate: number(0, 30*24*60*60*1e9)},
	{Key: "backup.auto_keep", Type: "number", Default: "5", Description: "How many automatic backups to keep", Validate: number(1, 100)},
	{Key: "browse.hide_nsfw", Type: "boolean", Default: "false", Description: "Adult (NSFW) sources do not appear in Browse", Validate: func(v any) error {
		if _, ok := v.(bool); !ok {
			return errors.New("value must be boolean")
		}
		return nil
	}},
	{Key: "privacy.incognito", Type: "boolean", Default: "false", Description: "Start in incognito mode, which does not record reading activity", Validate: boolean},
	{Key: "advanced.log_level", Type: "string", Default: `"info"`, Description: "How much detail is written to the log", Validate: enum("debug", "info", "warn", "error")},
	{Key: "advanced.image_cache_days", Type: "number", Default: "30", Description: "Days to keep processed page images", Validate: number(1, 3650)},
	{Key: "library.view.sort", Type: "string", Default: `"recent"`, Description: "Library ordering", Validate: enum("recent", "title", "added", "last_read", "unread"), Hidden: true},
	{Key: "library.view.sort_direction", Type: "string", Default: `"desc"`, Description: "Library ordering direction", Validate: enum("asc", "desc"), Hidden: true},
	{Key: "library.view.card_size", Type: "string", Default: `"medium"`, Description: "Library card size", Validate: enum("small", "medium", "large", "list", "cover-only"), Hidden: true},
	{Key: "library.view.unread_badge", Type: "boolean", Default: "true", Description: "Show unread counts on library cards", Validate: boolean, Hidden: true},
	{Key: "library.view.progress_bar", Type: "boolean", Default: "true", Description: "Show reading progress on library cards", Validate: boolean, Hidden: true},
	{Key: "library.view.continue_button", Type: "boolean", Default: "true", Description: "Show the continue action on library cards", Validate: boolean, Hidden: true},
	{Key: "library.view.category", Type: "number", Default: "0", Description: "Selected library category", Validate: number(0, 1e9), Hidden: true},
	{Key: "library.view.filter_read_state", Type: "string", Default: `""`, Description: "Library read state filter", Validate: listOf("unread", "in_progress", "completed"), Hidden: true},
	{Key: "library.view.filter_status", Type: "string", Default: `""`, Description: "Library publication status filter", Validate: listOf("ongoing", "completed", "hiatus", "cancelled", "unknown"), Hidden: true},
	{Key: "library.view.filter_sources", Type: "string", Default: `""`, Description: "Library source filter", Validate: identifierList(), Hidden: true},
	{Key: "library.view.filter_downloaded", Type: "boolean", Default: "false", Description: "Library shows only titles with downloads", Validate: boolean, Hidden: true},
	{Key: "library.view.filter_started", Type: "boolean", Default: "false", Description: "Library shows only started titles", Validate: boolean, Hidden: true},
	{Key: "library.view.filter_bookmarked", Type: "boolean", Default: "false", Description: "Library shows only titles with bookmarked chapters", Validate: boolean, Hidden: true},
	{Key: "library.view.columns", Type: "number", Default: "0", Description: "Library grid columns, 0 follows the card size", Validate: number(0, 10), Hidden: true},
	{Key: "library.view.group_by", Type: "string", Default: `"none"`, Description: "Library grouping", Validate: enum("none", "category", "source", "status"), Hidden: true},
}

var definitions = func() map[string]Definition {
	out := make(map[string]Definition, len(definitionList))
	for _, definition := range definitionList {
		out[definition.Key] = definition
	}
	return out
}()
