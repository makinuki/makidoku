package engine

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// StorageValueCap is the per-value limit enforced on makinuki_storage_set.
// Writes above it are rejected at the call boundary; over-cap values written
// by older runtimes are truncated on read.
const StorageValueCap = 64 * 1024

// Reserved storage keys holding the anti-bot clearance material the host
// records after a challenge is solved. They share the plugin key namespace so
// a source can inspect its own clearance state.
const (
	ClearanceCookieKey    = "makinuki.cf_clearance"
	ClearanceUserAgentKey = "makinuki.user_agent"
)

// Storage is the persistence layer behind makinuki_storage_get and
// makinuki_storage_set. Keys are namespaced per source so they never collide.
type Storage interface {
	Get(sourceID, key string) (string, bool, error)
	Set(sourceID, key, value string) error
}

// SQLStorage backs plugin storage with the plugin_storage table.
type SQLStorage struct {
	db *sqlx.DB
}

func NewSQLStorage(db *sqlx.DB) *SQLStorage { return &SQLStorage{db: db} }

// Get returns the stored value and whether the key exists. A stored empty
// string is a legitimate value and is reported as found.
func (s *SQLStorage) Get(sourceID, key string) (string, bool, error) {
	var value string
	err := s.db.Get(&value,
		`SELECT value FROM plugin_storage WHERE source_id = ? AND "key" = ?`,
		sourceID, key)
	if err != nil {
		if isNoRows(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return truncateToCap(value), true, nil
}

// Set writes value, rejecting payloads above the 64 KB cap.
func (s *SQLStorage) Set(sourceID, key, value string) error {
	if len(value) > StorageValueCap {
		return CodedError(CodeMemoryLimitExceeded,
			"storage value for key %q is %d bytes, above the %d byte cap",
			key, len(value), StorageValueCap)
	}
	_, err := s.db.Exec(
		`INSERT INTO plugin_storage(source_id, "key", value, updated_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(source_id, "key") DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		sourceID, key, value, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("write plugin storage: %w", err)
	}
	return nil
}

// MemoryStorage is an in-process Storage used by tests and by short-lived
// plugin calls made before a source is installed.
type MemoryStorage struct {
	values map[string]string
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{values: map[string]string{}}
}

func (m *MemoryStorage) Get(sourceID, key string) (string, bool, error) {
	value, ok := m.values[sourceID+":"+key]
	return truncateToCap(value), ok, nil
}

func (m *MemoryStorage) Set(sourceID, key, value string) error {
	if len(value) > StorageValueCap {
		return CodedError(CodeMemoryLimitExceeded,
			"storage value for key %q is %d bytes, above the %d byte cap",
			key, len(value), StorageValueCap)
	}
	m.values[sourceID+":"+key] = value
	return nil
}

// truncateToCap trims an over-cap value to the cap on a rune boundary so the
// result stays valid UTF-8.
func truncateToCap(value string) string {
	if len(value) <= StorageValueCap {
		return value
	}
	cut := value[:StorageValueCap]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
