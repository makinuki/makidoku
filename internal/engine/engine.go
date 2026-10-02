package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	dbstore "github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/identity"
	"github.com/makinuki/makidoku/internal/languages"
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
	ID           string `json:"id"`
	PluginKey    string `json:"-"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	ABIVersion   int    `json:"abiVersion"`
	Lang         string `json:"lang"`
	BaseURL      string `json:"baseUrl"`
	IconURL      string `json:"iconUrl"`
	NSFW         bool   `json:"nsfw"`
	InstalledAt  int64  `json:"installedAt"`
	Loaded       bool   `json:"loaded"`
	HasClearance bool   `json:"hasClearance"`
	HasSettings  bool   `json:"hasSettings"`
	// Languages is the stored per-source chapter language selection. An empty
	// slice means the source inherits the global default.
	Languages []string `json:"languages"`
	// AvailableLanguages lists the distinct chapter language codes seen for the
	// source. It is filled by the API from stored chapters, not by the plugin.
	AvailableLanguages []string `json:"availableLanguages,omitempty"`
	AllowedHosts       []string `json:"allowedHosts,omitempty"`
	Pinned             bool     `json:"pinned"`
	LastUsedAt         *int64   `json:"lastUsedAt,omitempty"`
	// Challenge describes the anti-bot state of this source, or nil when the
	// source has not met a challenge. It replaces the boolean HasClearance in
	// the web UI, where "solved before" and "blocked right now" mean different
	// things to a reader.
	Challenge *SourceChallenge `json:"challenge,omitempty"`
}

// SourceChallenge is the per-source view of an outstanding challenge.
type SourceChallenge struct {
	// State is one of "challenged" or "blocked".
	State string `json:"state"`
	// Origins lists the registrable domains awaiting clearance. A source whose
	// pages and images sit on different domains reports both.
	Origins []string `json:"origins"`
	// Hits counts the requests that met the challenge.
	Hits int `json:"hits"`
	// LastSeen is a unix timestamp of the most recent encounter.
	LastSeen int64 `json:"lastSeen"`
	// Message explains the state in terms a reader can act on.
	Message string `json:"message"`
}

// CatalogEntry is a registry entry annotated with local state.
type CatalogEntry struct {
	RegistryEntry
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	Compatible       bool   `json:"compatible"`
	Incompatibility  string `json:"incompatibility,omitempty"`
	UpdateAvailable  bool   `json:"updateAvailable"`
}

func New(conn *sqlx.DB, opts Options) *Engine {
	storage := NewSQLStorage(conn)
	clearance := NewClearanceBroker(dbstore.NewRepository(conn), storage, opts.ChallengeWait)
	return &Engine{
		db:        conn,
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

// DataDir is the daemon-owned root for plugin and processed-image caches.
func (e *Engine) DataDir() string { return e.dataDir }

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
			slog.Warn("engine releasing failed", "plugin", p.id, "err", err)
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
	rows, err := e.rows()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		installed[row.PluginKey] = row.Version
	}

	out := make([]CatalogEntry, 0, len(index.Sources))
	for _, entry := range index.Sources {
		item := CatalogEntry{RegistryEntry: entry, Compatible: true}
		if version, ok := installed[entry.ID]; ok {
			item.Installed = true
			item.InstalledVersion = version
			if current, parseErr := parseSemver(version); parseErr == nil {
				if available, parseErr := parseSemver(entry.Version); parseErr == nil {
					item.UpdateAvailable = compareSemver(current, available) < 0
				}
			}
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

	meta, hasSettings, err := probeMetadata(ctx, wasm)
	if err != nil {
		return InstalledSource{}, err
	}
	// The digest is verified against the catalog, so a mismatch here means the
	// catalog and the binary disagree about which source this is.
	if meta.ID != entry.ID {
		return InstalledSource{}, CodedError(CodeParsingError,
			"catalog lists %q but the binary identifies as %q", entry.ID, meta.ID)
	}
	return e.register(ctx, meta, path, hasSettings)
}

// InstallFile registers a locally built binary. The file is copied into the
// plugin cache so the original may be moved or rebuilt afterwards.
func (e *Engine) InstallFile(ctx context.Context, path string) (InstalledSource, error) {
	wasm, err := os.ReadFile(path)
	if err != nil {
		return InstalledSource{}, CodedError(CodeNotFound, "reading %s failed: %v", path, err)
	}
	meta, hasSettings, err := probeMetadata(ctx, wasm)
	if err != nil {
		return InstalledSource{}, err
	}

	dest := e.registry.CachePath(RegistryEntry{ID: meta.ID, Version: meta.Version})
	if err := writeFileAtomic(dest, wasm); err != nil {
		return InstalledSource{}, err
	}
	return e.register(ctx, meta, dest, hasSettings)
}

// register records the source and drops any previously loaded instance so the
// next call compiles the new binary.
func (e *Engine) register(ctx context.Context, meta SourceMetadata, wasmPath string, hasSettings bool) (InstalledSource, error) {
	var icon *string
	if meta.IconURL != "" {
		icon = &meta.IconURL
	}
	var sourceID string
	if err := e.db.Get(&sourceID, `SELECT id FROM sources WHERE plugin_key=?`, meta.ID); err != nil {
		if !isNoRows(err) {
			return InstalledSource{}, fmt.Errorf("find source %s: %w", meta.ID, err)
		}
		var createErr error
		sourceID, createErr = identity.New()
		if createErr != nil {
			return InstalledSource{}, createErr
		}
	}
	_, err := e.db.Exec(
		`INSERT INTO sources(id, plugin_key, name, version, abi_version, lang, base_url, icon_url, wasm_path, installed, nsfw, has_settings, installed_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
		 ON CONFLICT(plugin_key) DO UPDATE SET
		   id = excluded.id,
		   name = excluded.name,
		   version = excluded.version,
		   abi_version = excluded.abi_version,
		   lang = excluded.lang,
		   base_url = excluded.base_url,
		   icon_url = excluded.icon_url,
		   wasm_path = excluded.wasm_path,
		   nsfw = excluded.nsfw,
		   has_settings = excluded.has_settings,
		   installed = 1`,
		sourceID, meta.ID, meta.Name, meta.Version, meta.ABIVersion, meta.Lang, meta.BaseURL, icon, wasmPath, meta.NSFW, hasSettings, time.Now().Unix())
	if err != nil {
		return InstalledSource{}, fmt.Errorf("record source %s: %w", meta.ID, err)
	}

	e.unload(ctx, sourceID)
	return e.Get(sourceID)
}

// Uninstall removes a source and its cached binary. Plugin storage rows are
// kept so reinstalling the same plugin recovers its state; they can be
// removed through the storage endpoints if desired.
func (e *Engine) Uninstall(ctx context.Context, id string) error {
	row, err := e.row(id)
	if err != nil {
		return err
	}
	// The cache is keyed by the canonical id, so the raw request reference
	// must be resolved first.
	e.unload(ctx, row.ID)

	if _, err := e.db.Exec(`UPDATE sources SET installed=0, wasm_path=NULL WHERE id = ?`, row.ID); err != nil {
		return fmt.Errorf("remove source %s: %w", id, err)
	}
	if row.WasmPath != "" && filepath.Dir(row.WasmPath) == filepath.Join(e.dataDir, "wasm") {
		if err := os.Remove(row.WasmPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("engine removing failed", "path", row.WasmPath, "err", err)
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

// TransferHints returns the pacing and retry policy a source suggests. A
// source that declares neither yields zero values, which the caller treats as
// the host default.
func (e *Engine) TransferHints(ctx context.Context, sourceID string) (RateLimitHints, RetryHints, error) {
	meta, err := e.Metadata(ctx, sourceID)
	if err != nil {
		return RateLimitHints{}, RetryHints{}, err
	}
	var rate RateLimitHints
	if meta.RateLimit != nil {
		rate = *meta.RateLimit
	}
	var retry RetryHints
	if meta.Retry != nil {
		retry = *meta.Retry
	}
	return rate, retry, nil
}

// Filters returns the source's search filter schemas.
func (e *Engine) Filters(ctx context.Context, sourceID string) (json.RawMessage, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return p.Filters(ctx)
}

// Settings returns the decoded setting schemas a source declares. A source
// without get_settings yields an empty slice.
func (e *Engine) Settings(ctx context.Context, sourceID string) ([]SettingSchema, error) {
	p, err := e.plugin(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	payload, err := p.Settings(ctx)
	if err != nil {
		return nil, err
	}
	var schemas []SettingSchema
	if err := json.Unmarshal(payload, &schemas); err != nil {
		return nil, CodedError(CodeParsingError, "%s.%s returned unexpected data: %v", sourceID, ExportGetSettings, err)
	}
	return schemas, nil
}

// SettingValue returns the raw stored value for a declared setting and
// whether a value is stored. A missing value means the schema default
// applies.
func (e *Engine) SettingValue(ctx context.Context, sourceID, key string) (string, bool, error) {
	row, err := e.row(sourceID)
	if err != nil {
		return "", false, err
	}
	return e.storage.Get(row.ID, key)
}

// SetSettingValue validates a decoded setting value against the declared
// schema, serializes it per the contract, and writes it into the plugin
// storage namespace. A nil value deletes the key, restoring the schema
// default.
func (e *Engine) SetSettingValue(ctx context.Context, sourceID, key string, value any) error {
	row, err := e.row(sourceID)
	if err != nil {
		return err
	}
	schemas, err := e.Settings(ctx, row.ID)
	if err != nil {
		return err
	}
	schema, ok := FindSetting(schemas, key)
	if !ok {
		return CodedError(CodeNotFound, "source %s declares no setting %q", sourceID, key)
	}
	if value == nil {
		return e.storage.Delete(row.ID, key)
	}
	serialized, err := SerializeSettingValue(schema, value)
	if err != nil {
		return err
	}
	return e.storage.Set(row.ID, key, serialized)
}

// FindSetting returns the declared schema for key.
func FindSetting(schemas []SettingSchema, key string) (SettingSchema, bool) {
	for _, schema := range schemas {
		if schema.ID == key {
			return schema, true
		}
	}
	return SettingSchema{}, false
}

// SerializeSettingValue validates a decoded value against schema and renders
// it into the storage form: "true"/"false" for a checkbox, the option value
// for a select, and the raw string for text.
func SerializeSettingValue(schema SettingSchema, value any) (string, error) {
	switch schema.Type {
	case SettingKindCheckbox:
		flag, ok := value.(bool)
		if !ok {
			return "", CodedError(CodeParsingError, "setting %q expects a boolean", schema.ID)
		}
		if flag {
			return "true", nil
		}
		return "false", nil
	case SettingKindSelect:
		text, ok := value.(string)
		if !ok {
			return "", CodedError(CodeParsingError, "setting %q expects a string", schema.ID)
		}
		for _, option := range schema.Options {
			if option.Value == text {
				return text, nil
			}
		}
		return "", CodedError(CodeParsingError, "setting %q has no option %q", schema.ID, text)
	case SettingKindText:
		text, ok := value.(string)
		if !ok {
			return "", CodedError(CodeParsingError, "setting %q expects a string", schema.ID)
		}
		if len(text) > StorageValueCap {
			return "", CodedError(CodeMemoryLimitExceeded,
				"setting %q is %d bytes, above the %d byte cap", schema.ID, len(text), StorageValueCap)
		}
		return text, nil
	default:
		return "", CodedError(CodeParsingError, "setting %q has an unsupported type %q", schema.ID, schema.Type)
	}
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

// FetchImage downloads one reader page through the source's shared HTTP
// client, including its cookie jar and stored anti-bot clearance.
func (e *Engine) FetchImage(ctx context.Context, sourceID, target string, headers map[string]string) ([]byte, error) {
	effectiveHeaders := make(map[string]string, len(headers)+1)
	hasReferer := false
	for name, value := range headers {
		effectiveHeaders[name] = value
		if strings.EqualFold(name, "Referer") && value != "" {
			hasReferer = true
		}
	}
	if !hasReferer && e.db != nil {
		if row, err := e.row(sourceID); err == nil && row.BaseURL != "" {
			effectiveHeaders["Referer"] = strings.TrimRight(row.BaseURL, "/") + "/"
		}
	}
	return e.fetcher.FetchImage(ctx, sourceID, target, effectiveHeaders)
}

// ImageURLAllowed restricts browser-facing image delivery to hosts declared by
// the installed source. It prevents the local daemon from becoming an open
// proxy while still allowing CDN hosts listed by a plugin.
func (e *Engine) ImageURLAllowed(ctx context.Context, sourceID, target string) (bool, error) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false, CodedError(CodeParsingError, "image URL must be absolute http or https")
	}
	row, err := e.row(sourceID)
	if err != nil {
		return false, err
	}
	host := strings.ToLower(u.Hostname())
	if base, parseErr := url.Parse(row.BaseURL); parseErr == nil && strings.EqualFold(base.Hostname(), host) {
		return true, nil
	}
	meta, err := e.Metadata(ctx, sourceID)
	if err != nil {
		if CodeOf(err) == CodeNotFound {
			return false, nil
		}
		return false, err
	}
	for _, allowed := range meta.AllowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		allowed = strings.TrimPrefix(allowed, "*.")
		if allowed != "" && (host == allowed || strings.HasSuffix(host, "."+allowed)) {
			return true, nil
		}
	}
	return false, nil
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
// SubmitClearance records clearance material an operator obtained in a browser.
// The origin defaults to the source's base host when the caller does not name
// one, and an empty cookie removes the stored material.
func (e *Engine) SubmitClearance(sourceID, origin, cookie, userAgent string) error {
	row, err := e.row(sourceID)
	if err != nil {
		return err
	}
	if origin == "" {
		origin = originOf(row.BaseURL)
	}
	// Persist under the canonical id: the fetcher always reads by id, while
	// callers may pass a plugin key or alias.
	return e.clearance.Submit(row.ID, origin, cookie, userAgent)
}

// SubmitClearanceBundle stores a full captured bundle for a source and origin.
//
// It is the lossless counterpart to SubmitClearance. A bundle carries the whole
// cookie jar together with the client hints, because a clearance cookie is
// commonly issued alongside others and a jar missing them is re-challenged
// immediately. Submitting a single cookie stays correct for a value pasted by
// hand, which is all that method is for.
func (e *Engine) SubmitClearanceBundle(sourceID string, bundle dbstore.ClearanceBundle) error {
	row, err := e.row(sourceID)
	if err != nil {
		return err
	}
	if bundle.Origin == "" {
		bundle.Origin = originOf(row.BaseURL)
	}
	// Persist under the canonical id, for the reason SubmitClearance does.
	return e.clearance.SubmitBundle(row.ID, bundle)
}

// ProbeClearance reports whether the stored clearance lets the fetcher reach an
// origin, and records the outcome against the bundle.
//
// A captured cookie proves nothing on its own. It can be stale, it can have been
// issued to an identity the fetcher no longer presents, and a site can serve one
// request while challenging the next. The only evidence that clearance works is
// a request that succeeds, so a caller is expected to ask rather than assume.
func (e *Engine) ProbeClearance(ctx context.Context, sourceID, origin string) (bool, error) {
	row, err := e.row(sourceID)
	if err != nil {
		return false, err
	}
	if origin == "" {
		origin = originOf(row.BaseURL)
	}

	// The probe uses the scheme the source declared, so a source registered over
	// plain HTTP is probed the way it is reached rather than being forced onto
	// TLS. Every registry source declares HTTPS, so this only differs where a
	// source says otherwise.
	scheme := "https"
	if declared, err := url.Parse(row.BaseURL); err == nil && declared.Scheme != "" {
		scheme = declared.Scheme
	}

	response, httpErr := e.fetcher.Do(ctx, row.ID, HttpRequest{
		URL:    scheme + "://" + origin + "/",
		Method: "GET",
	})
	if httpErr != nil {
		// A challenge here means the material did not help. It is recorded so the
		// button comes back rather than the bundle sitting there looking usable.
		_ = e.clearance.MarkChallenged(row.ID, origin)
		return false, nil
	}

	reachable := response.Status == http.StatusOK &&
		Classify(response.Status, response.Headers, []byte(response.Body)) == ClassNone
	if reachable {
		_ = e.clearance.MarkUsable(row.ID, origin)
	} else {
		_ = e.clearance.MarkChallenged(row.ID, origin)
	}
	return reachable, nil
}

// SourceBaseURL returns the base address a source is registered with.
//
// The record is read directly rather than asked of the plugin, so an address can
// be resolved for a source whose plugin has not been loaded. A source that is
// being challenged is exactly the case where loading its plugin is least
// reliable.
func (e *Engine) SourceBaseURL(sourceID string) (string, error) {
	row, err := e.row(sourceID)
	if err != nil {
		return "", err
	}
	return row.BaseURL, nil
}

// SetChallengeHook registers a host callback told when a request starts waiting
// for clearance. It is the seam an automatic solver uses, and it is optional.
func (e *Engine) SetChallengeHook(hook func(sourceID, origin, blockedURL string)) {
	e.clearance.SetChallengeHook(hook)
}

// ChallengeBroker exposes the clearance broker so a host can register a
// challenge hook. It is exported for wiring only; the broker's own methods
// remain the API.
func (e *Engine) ChallengeBroker() *ClearanceBroker {
	return e.clearance
}

// ClearanceBundles returns the stored clearance material for a source. Cookie
// values are never returned through the API.
func (e *Engine) ClearanceBundles(sourceID string) ([]ClearanceSummary, error) {
	row, err := e.row(sourceID)
	if err != nil {
		return nil, err
	}
	bundles, err := e.clearance.Bundles(row.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ClearanceSummary, 0, len(bundles))
	for _, bundle := range bundles {
		out = append(out, ClearanceSummary{
			Origin:          bundle.Origin,
			HasClearance:    bundle.HasClearance(),
			Cookies:         cookieNamesOf(bundle.Cookies),
			Status:          bundle.Status,
			BrowserProfile:  bundle.BrowserProfile,
			ObtainedAt:      bundle.ObtainedAt,
			ExpiresHint:     bundle.ExpiresHint,
			LastSuccessAt:   bundle.LastSuccessAt,
			LastChallengeAt: bundle.LastChallengeAt,
			Generation:      bundle.Generation,
			UserAgent:       bundle.UserAgent,
		})
	}
	return out, nil
}

// DeleteClearance removes stored material for one origin of a source.
func (e *Engine) DeleteClearance(sourceID, origin string) error {
	row, err := e.row(sourceID)
	if err != nil {
		return err
	}
	return e.clearance.Delete(row.ID, origin)
}

// ClearanceSummary describes stored clearance without exposing cookie values.
type ClearanceSummary struct {
	Origin          string   `json:"origin"`
	HasClearance    bool     `json:"hasClearance"`
	Cookies         []string `json:"cookies"`
	Status          string   `json:"status"`
	BrowserProfile  string   `json:"browserProfile"`
	ObtainedAt      int64    `json:"obtainedAt"`
	ExpiresHint     *int64   `json:"expiresHint,omitempty"`
	LastSuccessAt   *int64   `json:"lastSuccessAt,omitempty"`
	LastChallengeAt *int64   `json:"lastChallengeAt,omitempty"`
	Generation      int      `json:"generation"`
	// UserAgent is the identity the solve ran under. It is returned because a
	// mismatch between the cookie and the agent is the most common cause of a
	// replay failing, and an operator needs it to diagnose that.
	UserAgent string `json:"userAgent,omitempty"`
}

// cookieNamesOf returns the cookie names in a jar, sorted, with no values.
func cookieNamesOf(cookies map[string]string) []string {
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// plugin returns the loaded plugin for sourceID, compiling it on first use.
// Concurrent callers for the same source share one compilation. A source only
// serves while its installation record exists: a removed source stops
// answering even when an instance is still cached.
func (e *Engine) plugin(ctx context.Context, sourceID string) (*loadedPlugin, error) {
	row, err := e.row(sourceID)
	if err != nil {
		return nil, err
	}
	// The installation id is the canonical namespace for the instance cache,
	// the per-source storage, and the clearance record, so a caller that names
	// a source by its plugin key resolves to the same instance.
	sourceID = row.ID
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
	// Record the settings surface on first load so a source installed before
	// the flag existed reports it in the source list afterwards.
	if p.HasSettings() && !row.HasSettings {
		if _, err := e.db.Exec(`UPDATE sources SET has_settings=1 WHERE id=?`, sourceID); err != nil {
			slog.Warn("engine recording settings flag failed", "source", sourceID, "err", err)
		}
	}
	slog.Info("engine loaded", "plugin", p)
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
			slog.Warn("engine releasing failed", "plugin", sourceID, "err", err)
		}
	}
}

// sourceRow is the installation record.
type sourceRow struct {
	ID          string  `db:"id"`
	PluginKey   string  `db:"plugin_key"`
	Name        string  `db:"name"`
	Version     string  `db:"version"`
	ABIVersion  int     `db:"abi_version"`
	Lang        string  `db:"lang"`
	BaseURL     string  `db:"base_url"`
	IconURL     *string `db:"icon_url"`
	WasmPath    string  `db:"wasm_path"`
	Installed   bool    `db:"installed"`
	InstalledAt int64   `db:"installed_at"`
	Pinned      bool    `db:"pinned"`
	LastUsedAt  *int64  `db:"last_used_at"`
	NSFW        bool    `db:"nsfw"`
	HasSettings bool    `db:"has_settings"`
	Languages   string  `db:"languages"`
}

const sourceColumns = `id, COALESCE(plugin_key, '') AS plugin_key, name, version, abi_version, lang, base_url, icon_url, COALESCE(wasm_path, '') AS wasm_path, installed, installed_at, pinned, last_used_at, nsfw, has_settings, languages`

func (e *Engine) rows() ([]sourceRow, error) {
	var out []sourceRow
	if err := e.db.Select(&out, `SELECT `+sourceColumns+` FROM sources WHERE installed=1 ORDER BY name`); err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return out, nil
}

func (e *Engine) row(id string) (sourceRow, error) {
	var row sourceRow
	err := e.db.Get(&row, `SELECT `+sourceColumns+` FROM sources WHERE installed=1 AND (id = ? OR plugin_key = ?)`, id, id)
	if err != nil {
		if isNoRows(err) {
			return sourceRow{}, CodedError(CodeNotFound, "source %q is not installed", id)
		}
		return sourceRow{}, fmt.Errorf("read source %s: %w", id, err)
	}
	return row, nil
}

// sourceChallenge folds the per-origin challenge records into the single state
// shown for a source. It returns nil when no challenge is outstanding.
func (e *Engine) sourceChallenge(sourceID string) *SourceChallenge {
	states := e.fetcher.ChallengeStates()
	var matched []ChallengeState
	for _, state := range states {
		if state.SourceID == sourceID {
			matched = append(matched, state)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	sort.Slice(matched, func(i, j int) bool { return matched[i].LastSeen > matched[j].LastSeen })

	out := &SourceChallenge{
		State:    "challenged",
		Origins:  make([]string, 0, len(matched)),
		Hits:     0,
		LastSeen: matched[0].LastSeen,
		Message:  "This source is checking your browser. Solve it to continue.",
	}
	for _, state := range matched {
		out.Origins = append(out.Origins, state.Origin)
		out.Hits += state.Hits
	}
	return out
}

// ResolveID maps a caller-supplied source identifier to the installation id
// used as the storage and clearance namespace. A caller may pass either the
// installation id or the plugin key.
func (e *Engine) ResolveID(id string) (string, error) {
	row, err := e.row(id)
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

// ChallengeStates returns a snapshot of every origin awaiting clearance.
func (e *Engine) ChallengeStates() map[string]ChallengeState {
	return e.fetcher.ChallengeStates()
}

// describe merges the installation record with live plugin state.
func (e *Engine) describe(row sourceRow) InstalledSource {
	out := InstalledSource{
		ID:           row.ID,
		PluginKey:    row.PluginKey,
		Name:         row.Name,
		Version:      row.Version,
		ABIVersion:   row.ABIVersion,
		Lang:         row.Lang,
		BaseURL:      row.BaseURL,
		InstalledAt:  row.InstalledAt,
		HasClearance: e.clearance.HasClearance(row.ID),
		HasSettings:  row.HasSettings,
		Languages:    decodeLanguages(row.Languages),
		Pinned:       row.Pinned,
		LastUsedAt:   row.LastUsedAt,
		NSFW:         row.NSFW,
	}
	if row.IconURL != nil {
		out.IconURL = *row.IconURL
	}
	out.Challenge = e.sourceChallenge(row.ID)

	e.mu.Lock()
	p, loaded := e.plugins[row.ID]
	e.mu.Unlock()
	if loaded {
		out.Loaded = true
		out.NSFW = p.meta.NSFW
		out.AllowedHosts = p.meta.AllowedHosts
		// A source installed before the has_settings column existed still
		// reports its settings surface once it has been loaded once.
		out.HasSettings = out.HasSettings || p.HasSettings()
	}
	return out
}

func (e *Engine) SetSourcePinned(id string, pinned bool) (InstalledSource, error) {
	row, err := e.row(id)
	if err != nil {
		return InstalledSource{}, err
	}
	if _, err := e.db.Exec(`UPDATE sources SET pinned=? WHERE id=?`, pinned, row.ID); err != nil {
		return InstalledSource{}, err
	}
	return e.Get(row.ID)
}

// SetSourceLanguages stores the chapter language selection for a source. An
// empty selection clears the column, so the source falls back to the global
// default.
func (e *Engine) SetSourceLanguages(id string, codes []string) (InstalledSource, error) {
	row, err := e.row(id)
	if err != nil {
		return InstalledSource{}, err
	}
	stored, err := encodeLanguages(codes)
	if err != nil {
		return InstalledSource{}, err
	}
	if _, err := e.db.Exec(`UPDATE sources SET languages=? WHERE id=?`, stored, row.ID); err != nil {
		return InstalledSource{}, err
	}
	return e.Get(row.ID)
}

// encodeLanguages renders a selection for the languages column. An empty
// selection stores an empty string, which reads back as no preference.
func encodeLanguages(codes []string) (string, error) {
	normalized := languages.Set(codes)
	if len(normalized) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode languages: %w", err)
	}
	return string(raw), nil
}

// decodeLanguages reads the languages column. A malformed value degrades to no
// preference rather than failing the source read.
func decodeLanguages(stored string) []string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return []string{}
	}
	var codes []string
	if err := json.Unmarshal([]byte(stored), &codes); err != nil {
		return []string{}
	}
	normalized := languages.Set(codes)
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func (e *Engine) TouchSource(id string) error {
	row, err := e.row(id)
	if err != nil {
		return err
	}
	_, err = e.db.Exec(`UPDATE sources SET last_used_at=? WHERE id=?`, time.Now().Unix(), row.ID)
	return err
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
