package backup

import (
	"encoding/json"
	"fmt"
	"strings"

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

	categoryIDs := map[int64]int64{}
	for _, raw := range doc.Sources {
		m, _ := raw.(map[string]any)
		if _, err := tx.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,icon_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,version=excluded.version,abi_version=excluded.abi_version,lang=excluded.lang,base_url=excluded.base_url,icon_url=excluded.icon_url,wasm_path=excluded.wasm_path`, stringValue(m["id"]), stringValue(m["name"]), stringValue(m["version"]), intValue(m["abi_version"]), stringValue(m["lang"]), stringValue(m["base_url"]), nullableString(m["icon_url"]), stringValue(m["wasm_path"]), int64Value(m["installed_at"])); err != nil {
			return fmt.Errorf("import source %q: %w", stringValue(m["id"]), err)
		}
	}
	// Categories: preserve the exported ID where possible and map it when a
	// name already exists in the destination database.
	for _, raw := range doc.Categories {
		m, _ := raw.(map[string]any)
		name := stringValue(m["name"])
		sortOrder := toInt(m["sort_order"])
		oldID := int64Value(m["id"])
		if _, err := tx.Exec(`INSERT INTO categories(name, sort_order) VALUES(?, ?)
			ON CONFLICT(name) DO UPDATE SET sort_order=excluded.sort_order`, name, sortOrder); err != nil {
			return fmt.Errorf("import category %q: %w", name, err)
		}
		var id int64
		if err := tx.Get(&id, `SELECT id FROM categories WHERE name=?`, name); err != nil {
			return err
		}
		categoryIDs[oldID] = id
	}
	for _, raw := range doc.Manga {
		m, _ := raw.(map[string]any)
		if _, err := tx.Exec(`INSERT INTO manga(id,source_id,source_manga_id,title,alt_titles,description,authors,artists,genres,status,cover_url,in_library,download_format,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,alt_titles=excluded.alt_titles,description=excluded.description,authors=excluded.authors,artists=excluded.artists,genres=excluded.genres,status=excluded.status,cover_url=excluded.cover_url,in_library=excluded.in_library,download_format=excluded.download_format,updated_at=excluded.updated_at`,
			stringValue(m["id"]), stringValue(m["source_id"]), stringValue(m["source_manga_id"]), stringValue(m["title"]), nullableString(m["alt_titles"]), nullableString(m["description"]), nullableString(m["authors"]), nullableString(m["artists"]), nullableString(m["genres"]), stringValue(m["status"]), stringValue(m["cover_url"]), boolValue(m["in_library"]), stringValueDefault(m["download_format"], "cbz"), int64Value(m["created_at"]), int64Value(m["updated_at"])); err != nil {
			return fmt.Errorf("import manga %q: %w", stringValue(m["id"]), err)
		}
	}
	for _, raw := range doc.Chapters {
		m, _ := raw.(map[string]any)
		if _, err := tx.Exec(`INSERT INTO chapters(id,manga_id,source_chapter_id,chapter_number,title,language,uploaded_at,scanlator,downloaded,download_path)
			VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET chapter_number=excluded.chapter_number,title=excluded.title,language=excluded.language,uploaded_at=excluded.uploaded_at,scanlator=excluded.scanlator,downloaded=excluded.downloaded,download_path=excluded.download_path`,
			stringValue(m["id"]), stringValue(m["manga_id"]), stringValue(m["source_chapter_id"]), nullableFloat(m["chapter_number"]), nullableString(m["title"]), nullableString(m["language"]), nullableInt(m["uploaded_at"]), nullableString(m["scanlator"]), boolValue(m["downloaded"]), nullableString(m["download_path"])); err != nil {
			return fmt.Errorf("import chapter %q: %w", stringValue(m["id"]), err)
		}
	}
	for _, raw := range doc.MangaCategories {
		m, _ := raw.(map[string]any)
		if categoryID, ok := categoryIDs[int64Value(m["category_id"])]; ok {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO manga_categories(manga_id,category_id) VALUES(?,?)`, stringValue(m["manga_id"]), categoryID); err != nil {
				return err
			}
		}
	}
	for _, raw := range doc.Progress {
		m, _ := raw.(map[string]any)
		if _, err := tx.Exec(`INSERT INTO reading_progress(manga_id,last_read_chapter_id,last_read_page,total_pages,is_completed,last_read_at) VALUES(?,?,?,?,?,?) ON CONFLICT(manga_id) DO UPDATE SET last_read_chapter_id=excluded.last_read_chapter_id,last_read_page=excluded.last_read_page,total_pages=excluded.total_pages,is_completed=excluded.is_completed,last_read_at=excluded.last_read_at`, stringValue(m["manga_id"]), stringValue(m["last_read_chapter_id"]), intValue(m["last_read_page"]), intValue(m["total_pages"]), boolValue(m["is_completed"]), int64Value(m["last_read_at"])); err != nil {
			return fmt.Errorf("import progress for %q: %w", stringValue(m["manga_id"]), err)
		}
	}
	for _, raw := range doc.Trackers {
		m, _ := raw.(map[string]any)
		if _, err := tx.Exec(`INSERT INTO tracker_bindings(manga_id,tracker_type,remote_id,remote_title,remote_score,remote_status,last_synced_chapter,total_remote_chapters) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(manga_id,tracker_type) DO UPDATE SET remote_id=excluded.remote_id,remote_title=excluded.remote_title,remote_score=excluded.remote_score,remote_status=excluded.remote_status,last_synced_chapter=excluded.last_synced_chapter,total_remote_chapters=excluded.total_remote_chapters`, stringValue(m["manga_id"]), stringValue(m["tracker_type"]), stringValue(m["remote_id"]), stringValue(m["remote_title"]), nullableFloat(m["remote_score"]), nullableString(m["remote_status"]), floatValue(m["last_synced_chapter"]), nullableInt(m["total_remote_chapters"])); err != nil {
			return fmt.Errorf("import tracker binding: %w", err)
		}
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

func stringValue(v any) string { value, _ := v.(string); return value }
func stringValueDefault(v any, fallback string) string {
	value := stringValue(v)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
func nullableString(v any) any {
	if value, ok := v.(string); ok && value != "" {
		return value
	}
	return nil
}
func int64Value(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
func intValue(v any) int { return int(int64Value(v)) }
func boolValue(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case float64:
		return value != 0
	case int64:
		return value != 0
	case int:
		return value != 0
	case string:
		return value == "1" || strings.EqualFold(value, "true")
	default:
		return false
	}
}
func floatValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}
func nullableFloat(v any) any {
	if v == nil {
		return nil
	}
	return floatValue(v)
}
func nullableInt(v any) any {
	if v == nil {
		return nil
	}
	return int64Value(v)
}
