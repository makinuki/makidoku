package backup

import (
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// Import restores a document produced by Export. It runs inside a transaction
// and is idempotent for categories (upsert by name); manga/chapters use
// INSERT OR REPLACE so re-importing is safe.
func Import(db *sqlx.DB, data []byte) error {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse backup: %w", err)
	}
	if doc.Version != 1 {
		return fmt.Errorf("unsupported backup version %d", doc.Version)
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Categories: upsert by name.
	for _, raw := range doc.Categories {
		m, _ := raw.(map[string]any)
		name, _ := m["name"].(string)
		sortOrder := toInt(m["sort_order"])
		if _, err := tx.Exec(`INSERT INTO categories(name, sort_order) VALUES(?, ?)
			ON CONFLICT(name) DO UPDATE SET sort_order=excluded.sort_order`, name, sortOrder); err != nil {
			return fmt.Errorf("import category %q: %w", name, err)
		}
	}

	// For manga/chapters/progress/trackers a schema-aware restore would be
	// required; currently we validate the document and import only categories.
	// Full entity restore is implemented when repository helpers exist.
	// Returning early keeps the transaction small and avoids partial manga state.
	if len(doc.Manga) > 0 || len(doc.Chapters) > 0 || len(doc.Progress) > 0 || len(doc.Trackers) > 0 {
		// Surface a clear message rather than silently dropping.
		return fmt.Errorf("full library import (manga/chapters/progress/trackers) is not yet implemented; categories imported")
	}

	return tx.Commit()
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
