package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// Options configures the engine.
type Options struct {
	// DataDir holds the plugin cache under its wasm subdirectory.
	DataDir string
	// RegistryURL is the catalog location. An http or https URL reads the
	// public catalog; a filesystem path reads a local mirror.
	RegistryURL string
	// ChallengeWait is how long a blocked GET or HEAD waits for anti-bot
	// clearance to be submitted before it fails. Zero fails immediately.
	ChallengeWait time.Duration
}

// Engine hosts installed sources and exposes their exports to the rest of the
// application. Plugins are compiled on first use and kept loaded afterwards.
type Engine struct {
	db        *sqlx.DB
	dataDir   string
	storage   Storage
	fetcher   *Fetcher
	clearance *ClearanceBroker
	registry  *Registry

	mu      sync.Mutex
	plugins map[string]*loadedPlugin
	loading map[string]chan struct{}
}

// InstalledSource describes an installed source for the local API.
type InstalledSource struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	ABIVersion   int      `json:"abiVersion"`
	Lang         string   `json:"lang"`
	BaseURL      string   `json:"baseUrl"`
	IconURL      string   `json:"iconUrl"`
	NSFW         bool     `json:"nsfw"`
	InstalledAt  int64    `json:"installedAt"`
	Loaded       bool     `json:"loaded"`
	HasClearance bool     `json:"hasClearance"`
	AllowedHosts []string `json:"allowedHosts,omitempty"`
}

// CatalogEntry is a registry entry annotated with local state.
type CatalogEntry struct {
	RegistryEntry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	Compatible       bool   `json:"compatible"`
	Incompatibility  string `json:"incompatibility,omitempty"`
}

func New(db *sqlx.DB, opts Options) *Engine {
	storage := NewSQLStorage(db)
	clearance := NewClearanceBroker(storage, opts.ChallengeWait)
	return &Engine{
		db:        db,
		dataDir:   opts.DataDir,
		storage:   storage,
		fetcher:   NewFetcher(storage, clearance),
		clearance: clearance,
		registry:  NewRegistry(opts.RegistryURL, opts.DataDir),
		plugins:   map[string]*loadedPlugin{},
		loading:   map[string]chan struct{}{},
	}
}

// Registry exposes the catalog client.
func (e *Engine) Registry() *Registry { return e.registry }

// Close releases every loaded plugin.
func (e *Engine) Close(ctx context.Context) {
	e.mu.Lock()
	loaded := make([]*loadedPlugin, 0, len(e.plugins))
	for id, p := range e.plugins {
		loaded = append(loaded, p)
		delete(e.plugins, id)
	}
	e.mu.Unlock()
	for _, p := range loaded {
		if err := p.close(ctx); err != nil {
			log.Printf("engine: releasing %s failed: %v", p.id, err)
		}
	}
}

// Installed lists the installed sources.
func (e *Engine) Installed() ([]InstalledSource, error) {
	rows, err := e.rows()
	if err != nil {
		return nil, err
	}
	out := make([]InstalledSource, 0, len(rows))
	for _, row := range rows {
		out = append(out, e.describe(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns one installed source.
func (e *Engine) Get(id string) (InstalledSource, error) {
	row, err := e.row(id)
	if err != nil {
		return InstalledSource{}, err
	}
	return e.describe(row), nil
}

// Catalog lists the registry entries annotated with local install state.
func (e *Engine) Catalog(ctx context.Context, refresh bool) ([]CatalogEntry, error) {
	index, err := e.registry.Index(ctx, refresh)
	if err != nil {
		return nil, err
	}
	installed := map[string]string{}
	if rows, err := e.rows(); err == nil {
		for _, row := range rows {
			installed[row.ID] = row.Version
		}
	}

	out := make([]CatalogEntry, 0, len(index.Sources))
	for _, entry := range index.Sources {
		item := CatalogEntry{RegistryEntry: entry, Compatible: true}
		if version, ok := installed[entry.ID]; ok {
			item.Installed = true
			item.InstalledVersion = version
		}
		if err := validateEntry(entry); err != nil {
			item.Compatible = false
			item.Incompatibility = err.Error()
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Install downloads, verifies and registers a catalog source. Reinstalling an
// existing source replaces its binary and keeps its plugin storage.
func (e *Engine) Install(ctx context.Context, id string) (InstalledSource, error) {
	entry, err := e.registry.Find(ctx, id)
	if err != nil {
		return InstalledSource{}, err
	}
	path, wasm, err := e.registry.Fetch(ctx, entry)
	if err != nil {
		return InstalledSource{}, err
	}

	meta, err := probeMetadata(ctx, wasm)
	if err != nil {
		return InstalledSource{}, err
	}
	// The digest is verified against the catalog, so a mismatch here means the
	// catalog and the binary disagree about which source this is.
	if meta.ID != entry.ID {
		return InstalledSource{}, CodedError(CodeParsingError,
			"catalog lists %q but the binary identifies as %q", entry.ID, meta.ID)
	}
	return e.register(ctx, meta, path)
}

// InstallFile registers a locally built binary. The file is copied into the
// plugin cache so the original may be moved or rebuilt afterwards.
func (e *Engine) InstallFile(ctx context.Context, path string) (InstalledSource, error) {
	wasm, err := os.ReadFile(path)
	if err != nil {
		return InstalledSource{}, CodedError(CodeNotFound, "reading %s failed: %v", path, err)
	}
	meta, err := probeMetadata(ctx, wasm)
	if err != nil {
		return InstalledSource{}, err
	}

	dest := e.registry.CachePath(RegistryEntry{ID: meta.ID, Version: meta.Version})
	if err := writeFileAtomic(dest, wasm); err != nil {
		return InstalledSource{}, err
	}
	return e.register(ctx, meta, dest)
}

// register records the source and drops any previously loaded instance so the
// next call compiles the new binary.
func (e *Engine) register(ctx context.Context, meta SourceMetadata, wasmPath string) (InstalledSource, error) {
	var icon *string
	if meta.IconURL != "" {
		icon = &meta.IconURL
	}
	_, err := e.db.Exec(
		`INSERT INTO sources(id, name, version, abi_version, lang, base_url, icon_url, wasm_path, installed_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   version = excluded.version,
		   abi_version = excluded.abi_version,
		   lang = excluded.lang,
		   base_url = excluded.base_url,
		   icon_url = excluded.icon_url,
		   wasm_path = excluded.wasm_path`,
		meta.ID, meta.Name, meta.Version, meta.ABIVersion, meta.Lang, meta.BaseURL, icon, wasmPath, time.Now().Unix())
	if err != nil {
		return InstalledSource{}, fmt.Errorf("record source %s: %w", meta.ID, err)
	}

	e.unload(ctx, meta.ID)
	return e.Get(meta.ID)
}

// Uninstall removes a source, its plugin storage and its cached binary.
func (e *Engine) Uninstall(ctx context.Context, id string) error {
	row, err := e.row(id)
	if err != nil {
		return err
	}
	e.unload(ctx, id)

	// Plugin storage is removed by the foreign key cascade on sources.
	if _, err := e.db.Exec(`DELETE FROM sources WHERE id = ?`, id); err != nil {
		return fmt.Errorf("remove source %s: %w", id, err)
	}
	if row.WasmPath != "" && filepath.Dir(row.WasmPath) == filepath.Join(e.dataDir, "wasm") {
		if err := os.Remove(row.WasmPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("engine: removing %s failed: %v", row.WasmPath, err)
		}
	}
	return nil
}

// Metadata returns the descriptor reported by the loaded binary.
func (e *Engine) Metadata(ctx context.Context, sourceID string) (SourceMetadata, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return SourceMetadata{}, err
	}
	return p.meta, nil
}

// Filters returns the source's search filter schemas.
func (e *Engine) Filters(ctx context.Context, sourceID string) (json.RawMessage, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return p.Filters(ctx)
}

// Search runs a source search.
func (e *Engine) Search(ctx context.Context, sourceID string, query SearchQuery) (PageResult, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return PageResult{}, err
	}
	result, err := p.Search(ctx, query)
	if err != nil {
		return PageResult{}, err
	}
	if result.Items == nil {
		result.Items = []MangaItem{}
	}
	return result, nil
}

// Details fetches a title's metadata and chapter list.
func (e *Engine) Details(ctx context.Context, sourceID, mangaID string) (MangaDetails, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return MangaDetails{}, err
	}
	details, err := p.Details(ctx, mangaID)
	if err != nil {
		return MangaDetails{}, err
	}
	if details.Chapters == nil {
		details.Chapters = []ChapterItem{}
	}
	return details, nil
}

// Pages fetches the ordered image pages of a chapter.
func (e *Engine) Pages(ctx context.Context, sourceID, chapterID string) ([]PageItem, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	pages, err := p.Pages(ctx, chapterID)
	if err != nil {
		return nil, err
	}
	if pages == nil {
		pages = []PageItem{}
	}
	return pages, nil
}

// Unscramble routes scrambled image bytes through the source. Sources without
// the optional export report UNSUPPORTED_MEDIA.
func (e *Engine) Unscramble(ctx context.Context, sourceID string, data []byte) ([]byte, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return p.Unscramble(ctx, data)
}

// SubmitClearance stores anti-bot clearance material for a source and releases
// any request waiting on it.
func (e *Engine) SubmitClearance(sourceID, cookie, userAgent string) error {
	if _, err := e.row(sourceID); err != nil {
		return err
	}
	return e.clearance.Submit(sourceID, cookie, userAgent)
}

// plugin returns the loaded plugin for sourceID, compiling it on first use.
// Concurrent callers for the same source share one compilation.
func (e *Engine) plugin(ctx context.Context, sourceID string) (*loadedPlugin, error) {
	for {
		e.mu.Lock()
		if p, ok := e.plugins[sourceID]; ok {
			e.mu.Unlock()
			return p, nil
		}
		if wait, ok := e.loading[sourceID]; ok {
			e.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, CodedError(CodeNetworkTimeout, "waiting for source %s to load: %v", sourceID, ctx.Err())
			}
		}
		done := make(chan struct{})
		e.loading[sourceID] = done
		e.mu.Unlock()

		p, err := e.load(ctx, sourceID)

		e.mu.Lock()
		delete(e.loading, sourceID)
		if err == nil {
			e.plugins[sourceID] = p
		}
		e.mu.Unlock()
		close(done)

		if err != nil {
			return nil, err
		}
		return p, nil
	}
}

// load compiles the installed binary for sourceID.
func (e *Engine) load(ctx context.Context, sourceID string) (*loadedPlugin, error) {
	row, err := e.row(sourceID)
	if err != nil {
		return nil, err
	}
	wasm, err := os.ReadFile(row.WasmPath)
	if err != nil {
		return nil, CodedError(CodeNotFound,
			"binary for source %s is missing at %s, reinstall the source", sourceID, row.WasmPath)
	}
	p, err := loadPlugin(ctx, sourceID, wasm, e.fetcher, e.storage)
	if err != nil {
		return nil, err
	}
	log.Printf("engine: loaded %s", p)
	return p, nil
}

// unload releases a loaded plugin without touching its installation record.
func (e *Engine) unload(ctx context.Context, sourceID string) {
	e.mu.Lock()
	p, ok := e.plugins[sourceID]
	delete(e.plugins, sourceID)
	e.mu.Unlock()
	if ok {
		if err := p.close(ctx); err != nil {
			log.Printf("engine: releasing %s failed: %v", sourceID, err)
		}
	}
}

// sourceRow is the installation record.
type sourceRow struct {
	ID          string  `db:"id"`
	Name        string  `db:"name"`
	Version     string  `db:"version"`
	ABIVersion  int     `db:"abi_version"`
	Lang        string  `db:"lang"`
	BaseURL     string  `db:"base_url"`
	IconURL     *string `db:"icon_url"`
	WasmPath    string  `db:"wasm_path"`
	InstalledAt int64   `db:"installed_at"`
}

const sourceColumns = `id, name, version, abi_version, lang, base_url, icon_url, wasm_path, installed_at`

func (e *Engine) rows() ([]sourceRow, error) {
	var out []sourceRow
	if err := e.db.Select(&out, `SELECT `+sourceColumns+` FROM sources ORDER BY name`); err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return out, nil
}

func (e *Engine) row(id string) (sourceRow, error) {
	var row sourceRow
	err := e.db.Get(&row, `SELECT `+sourceColumns+` FROM sources WHERE id = ?`, id)
	if err != nil {
		if isNoRows(err) {
			return sourceRow{}, CodedError(CodeNotFound, "source %q is not installed", id)
		}
		return sourceRow{}, fmt.Errorf("read source %s: %w", id, err)
	}
	return row, nil
}

// describe merges the installation record with live plugin state.
func (e *Engine) describe(row sourceRow) InstalledSource {
	out := InstalledSource{
		ID:           row.ID,
		Name:         row.Name,
		Version:      row.Version,
		ABIVersion:   row.ABIVersion,
		Lang:         row.Lang,
		BaseURL:      row.BaseURL,
		InstalledAt:  row.InstalledAt,
		HasClearance: e.clearance.HasClearance(row.ID),
	}
	if row.IconURL != nil {
		out.IconURL = *row.IconURL
	}

	e.mu.Lock()
	p, loaded := e.plugins[row.ID]
	e.mu.Unlock()
	if loaded {
		out.Loaded = true
		out.NSFW = p.meta.NSFW
		out.AllowedHosts = p.meta.AllowedHosts
	}
	return out
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
