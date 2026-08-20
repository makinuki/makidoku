package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// Mount registers API routes on r. Currently it exposes only health and a
// categories listing so the DB wiring can be smoke-tested.
func Mount(r chi.Router, db *sqlx.DB) {
	r.Get("/api/health", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	r.Get("/api/categories", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var out []map[string]any
		rows, err := db.Queryx(`SELECT id, name, sort_order FROM categories ORDER BY sort_order, name`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			m := map[string]any{}
			if err := rows.MapScan(m); err != nil {
				continue
			}
			for k, v := range m {
				if b, ok := v.([]byte); ok {
					m[k] = string(b)
				}
			}
			out = append(out, m)
		}
		if out == nil {
			out = []map[string]any{}
		}
		// Minimal JSON without importing encoding/json streaming helpers.
		w.Header().Set("Content-Type", "application/json")
		// Use standard encoder.
		writeJSON(w, out)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// Keep import local to avoid cycle.
	type encodable = any
	// Inline encode to avoid an extra helper file.
	// We deliberately use encoding/json here.
	// Note: not importing at top to keep file self-contained for go vet demo
	// but Go requires the import - add it.
	// To satisfy vet, we do the real work via a helper that does import.
	encodeJSON(w, v)
}
