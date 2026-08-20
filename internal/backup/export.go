package backup

import (
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"
)

// Export collects the library into a portable JSON document.
type Document struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	Categories []any     `json:"categories"`
	Manga      []any     `json:"manga"`
	Chapters   []any     `json:"chapters"`
	Progress   []any     `json:"progress"`
	Trackers   []any     `json:"trackers"`
}

func Export(db *sqlx.DB) ([]byte, error) {
	doc := Document{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Categories: []any{},
		Manga:      []any{},
		Chapters:   []any{},
		Progress:   []any{},
		Trackers:   []any{},
	}

	// Helper to select into []map[string]any for forward-compatible export.
	query := func(q string, dest *[]any) error {
		rows, err := db.Queryx(q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m := map[string]any{}
			if err := rows.MapScan(m); err != nil {
				return err
			}
			// MapScan returns []byte for TEXT; normalize to string for JSON.
			for k, v := range m {
				if b, ok := v.([]byte); ok {
					m[k] = string(b)
				}
			}
			*dest = append(*dest, m)
		}
		return rows.Err()
	}

	if err := query(`SELECT * FROM categories ORDER BY sort_order`, &doc.Categories); err != nil {
		return nil, err
	}
	if err := query(`SELECT * FROM manga ORDER BY updated_at DESC`, &doc.Manga); err != nil {
		return nil, err
	}
	if err := query(`SELECT * FROM chapters ORDER BY manga_id, chapter_number`, &doc.Chapters); err != nil {
		return nil, err
	}
	if err := query(`SELECT * FROM reading_progress`, &doc.Progress); err != nil {
		return nil, err
	}
	if err := query(`SELECT * FROM tracker_bindings`, &doc.Trackers); err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}
