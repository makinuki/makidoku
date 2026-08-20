package db

import "github.com/jmoiron/sqlx"

// Repository groups typed queries. Currently it provides only health helpers;
// domain queries are added as the matching subsystems land.

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

// DB exposes the underlying handle for callers that need raw sqlx.
func (r *Repository) DB() *sqlx.DB { return r.db }

// Ping verifies the connection.
func (r *Repository) Ping() error { return r.db.Ping() }

// Category helpers (used by the library API).

func (r *Repository) ListCategories() ([]Category, error) {
	var out []Category
	err := r.db.Select(&out, `SELECT id, name, sort_order FROM categories ORDER BY sort_order, name`)
	return out, err
}

func (r *Repository) CreateCategory(name string, sortOrder int) (Category, error) {
	res, err := r.db.Exec(`INSERT INTO categories(name, sort_order) VALUES(?, ?)`, name, sortOrder)
	if err != nil {
		return Category{}, err
	}
	id, _ := res.LastInsertId()
	return Category{ID: id, Name: name, SortOrder: sortOrder}, nil
}
