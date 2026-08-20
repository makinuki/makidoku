package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultRegistryURL is the public catalog of compiled sources.
const DefaultRegistryURL = "https://makinuki.github.io/index.json"

// RuntimeVersion is the host runtime package version compared against a
// registry entry's minRuntimeVersion floor. Release builds set it through the
// linker; it is empty in a development build, which disables the floor check
// because there is no released version to compare against. It is independent
// of the ABI version, which is always enforced.
var RuntimeVersion = ""

const (
	// indexTTL is how long a fetched index is reused before it is refetched.
	indexTTL = 15 * time.Minute
	// maxIndexBytes and maxWasmBytes bound registry downloads.
	maxIndexBytes = 4 << 20
	maxWasmBytes  = 32 << 20
	// registryIndexVersion is the index format this client understands.
	registryIndexVersion = 1
)

// RegistryIndex is the catalog manifest.
type RegistryIndex struct {
	Version   int             `json:"version"`
	UpdatedAt int64           `json:"updatedAt"`
	Sources   []RegistryEntry `json:"sources"`
}

// RegistryEntry describes one installable source.
type RegistryEntry struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	ABIVersion        int      `json:"abiVersion"`
	Lang              string   `json:"lang"`
	BaseURL           string   `json:"baseUrl"`
	IconURL           string   `json:"iconUrl"`
	NSFW              bool     `json:"nsfw"`
	WasmURL           string   `json:"wasmUrl"`
	SHA256            string   `json:"sha256"`
	MinRuntimeVersion string   `json:"minRuntimeVersion"`
	AllowedHosts      []string `json:"allowedHosts,omitempty"`
}

// Registry reads the catalog and materializes verified plugin binaries in the
// data directory. location is either an http or https URL, or a path to a
// local index.json for offline installs.
type Registry struct {
	location string
	cacheDir string
	client   *http.Client

	mu        sync.Mutex
	index     *RegistryIndex
	fetchedAt time.Time
}

func NewRegistry(location, cacheDir string) *Registry {
	if strings.TrimSpace(location) == "" {
		location = DefaultRegistryURL
	}
	return &Registry{
		location: location,
		cacheDir: cacheDir,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

// Location reports the configured catalog location.
func (r *Registry) Location() string { return r.location }

// Index returns the catalog, reusing the last read for a short window. Pass
// refresh to force a read.
func (r *Registry) Index(ctx context.Context, refresh bool) (*RegistryIndex, error) {
	r.mu.Lock()
	cached := r.index
	fresh := time.Since(r.fetchedAt) < indexTTL
	r.mu.Unlock()
	if cached != nil && fresh && !refresh {
		return cached, nil
	}

	raw, err := r.read(ctx, r.location, maxIndexBytes)
	if err != nil {
		return nil, err
	}
	var index RegistryIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, CodedError(CodeParsingError, "registry index is not valid JSON: %v", err)
	}
	if index.Version != registryIndexVersion {
		return nil, CodedError(CodeUnsupportedMedia,
			"registry index format version %d is not supported, this host reads version %d",
			index.Version, registryIndexVersion)
	}

	r.mu.Lock()
	r.index = &index
	r.fetchedAt = time.Now()
	r.mu.Unlock()
	return &index, nil
}

// Find returns the entry for id.
func (r *Registry) Find(ctx context.Context, id string) (RegistryEntry, error) {
	index, err := r.Index(ctx, false)
	if err != nil {
		return RegistryEntry{}, err
	}
	for _, entry := range index.Sources {
		if entry.ID == id {
			return entry, nil
		}
	}
	// A stale cached index is the likely cause of a miss, so retry once
	// against the live catalog before reporting the source as unavailable.
	index, err = r.Index(ctx, true)
	if err != nil {
		return RegistryEntry{}, err
	}
	for _, entry := range index.Sources {
		if entry.ID == id {
			return entry, nil
		}
	}
	return RegistryEntry{}, CodedError(CodeNotFound, "registry has no source %q", id)
}

// Installable returns the catalog entries this host can execute.
func (r *Registry) Installable(ctx context.Context, refresh bool) ([]RegistryEntry, error) {
	index, err := r.Index(ctx, refresh)
	if err != nil {
		return nil, err
	}
	out := make([]RegistryEntry, 0, len(index.Sources))
	for _, entry := range index.Sources {
		if entry.ABIVersion == ABIVersion && meetsRuntimeFloor(entry) == nil {
			out = append(out, entry)
		}
	}
	return out, nil
}

// Fetch materializes the verified binary for entry and returns its cache path
// along with its bytes. A cached file is reused only when its digest still
// matches the catalog.
func (r *Registry) Fetch(ctx context.Context, entry RegistryEntry) (string, []byte, error) {
	if err := validateEntry(entry); err != nil {
		return "", nil, err
	}

	cachePath := r.CachePath(entry)
	if cached, err := os.ReadFile(cachePath); err == nil {
		if digest(cached) == strings.ToLower(entry.SHA256) {
			return cachePath, cached, nil
		}
	}

	wasm, err := r.read(ctx, r.wasmLocation(entry), maxWasmBytes)
	if err != nil {
		return "", nil, err
	}
	// Verification happens before the bytes are written or executed.
	if got := digest(wasm); got != strings.ToLower(entry.SHA256) {
		return "", nil, CodedError(CodeParsingError,
			"digest mismatch for source %s: catalog declares %s, download is %s",
			entry.ID, strings.ToLower(entry.SHA256), got)
	}
	if err := writeFileAtomic(cachePath, wasm); err != nil {
		return "", nil, err
	}
	return cachePath, wasm, nil
}

// CachePath is where the binary for entry is stored.
func (r *Registry) CachePath(entry RegistryEntry) string {
	name := fmt.Sprintf("%s-v%s.wasm", sanitizeFileName(entry.ID), sanitizeFileName(entry.Version))
	return filepath.Join(r.cacheDir, "wasm", name)
}

// wasmLocation resolves where the binary for entry is read from. A local
// catalog is treated as a mirror: the path of wasmUrl is resolved against the
// directory holding index.json, so an offline copy of the published layout
// installs without network access.
func (r *Registry) wasmLocation(entry RegistryEntry) string {
	if isHTTPURL(r.location) {
		return entry.WasmURL
	}
	relative := entry.WasmURL
	if parsed, err := url.Parse(entry.WasmURL); err == nil && parsed.Path != "" {
		relative = strings.TrimPrefix(parsed.Path, "/")
	}
	return filepath.Join(filepath.Dir(r.location), filepath.FromSlash(path.Clean(relative)))
}

// read loads a catalog resource from an http URL or from disk.
func (r *Registry) read(ctx context.Context, location string, limit int64) ([]byte, error) {
	if !isHTTPURL(location) {
		raw, err := os.ReadFile(location)
		if err != nil {
			return nil, CodedError(CodeNotFound, "reading %s failed: %v", location, err)
		}
		if int64(len(raw)) > limit {
			return nil, CodedError(CodeMemoryLimitExceeded, "%s is larger than %d bytes", location, limit)
		}
		return raw, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, CodedError(CodeParsingError, "registry location %q is unusable: %v", location, err)
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, CodedError(CodeNetworkTimeout, "requesting %s failed: %v", location, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, CodedError(codeForStatus(resp.StatusCode), "%s returned status %d", location, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, CodedError(CodeNetworkTimeout, "reading %s failed: %v", location, err)
	}
	if int64(len(raw)) > limit {
		return nil, CodedError(CodeMemoryLimitExceeded, "%s is larger than %d bytes", location, limit)
	}
	return raw, nil
}

// validateEntry applies the catalog rules that must hold before a binary is
// downloaded or executed.
func validateEntry(entry RegistryEntry) error {
	if entry.ID == "" {
		return CodedError(CodeParsingError, "registry entry is missing an id")
	}
	if entry.ABIVersion != ABIVersion {
		return CodedError(CodeUnsupportedMedia,
			"source %s targets ABI version %d, this host implements %d",
			entry.ID, entry.ABIVersion, ABIVersion)
	}
	if entry.WasmURL == "" {
		return CodedError(CodeParsingError, "source %s has no wasmUrl", entry.ID)
	}
	if !isHexDigest(entry.SHA256) {
		return CodedError(CodeParsingError, "source %s has no usable sha256 digest", entry.ID)
	}
	return meetsRuntimeFloor(entry)
}

// meetsRuntimeFloor checks the runtime package floor, which is independent of
// the ABI version.
func meetsRuntimeFloor(entry RegistryEntry) error {
	if RuntimeVersion == "" || entry.MinRuntimeVersion == "" {
		return nil
	}
	have, err := parseSemver(RuntimeVersion)
	if err != nil {
		return nil
	}
	want, err := parseSemver(entry.MinRuntimeVersion)
	if err != nil {
		return nil
	}
	if compareSemver(have, want) < 0 {
		return CodedError(CodeUnsupportedMedia,
			"source %s requires runtime %s or newer, this host is %s",
			entry.ID, entry.MinRuntimeVersion, RuntimeVersion)
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isHTTPURL(location string) bool {
	parsed, err := url.Parse(location)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

// sanitizeFileName keeps catalog identifiers from escaping the cache
// directory.
func sanitizeFileName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "source"
	}
	return out
}

// writeFileAtomic writes through a temporary file so an interrupted download
// never leaves a partial binary in the cache.
func writeFileAtomic(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write %s: %w", dest, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, dest); err != nil {
		os.Remove(name)
		return fmt.Errorf("install %s: %w", dest, err)
	}
	return nil
}

type semver struct {
	major, minor, patch int
}

func parseSemver(value string) (semver, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "v")
	if cut := strings.IndexAny(trimmed, "-+"); cut >= 0 {
		trimmed = trimmed[:cut]
	}
	parts := strings.Split(trimmed, ".")
	if len(parts) == 0 || parts[0] == "" {
		return semver{}, fmt.Errorf("not a semantic version: %q", value)
	}
	out := semver{}
	targets := []*int{&out.major, &out.minor, &out.patch}
	for i, part := range parts {
		if i >= len(targets) {
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return semver{}, fmt.Errorf("not a semantic version: %q", value)
		}
		*targets[i] = n
	}
	return out, nil
}

func compareSemver(a, b semver) int {
	switch {
	case a.major != b.major:
		return a.major - b.major
	case a.minor != b.minor:
		return a.minor - b.minor
	default:
		return a.patch - b.patch
	}
}
