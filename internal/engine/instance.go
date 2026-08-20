package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"

	// Registered for DecodeConfig so image dimensions can be checked before a
	// buffer is handed to a plugin.
	_ "image/jpeg"
	_ "image/png"
)

const (
	// instanceMemoryPages caps a single instance at 64 MB. WebAssembly pages
	// are 64 KB, so 1024 pages is the whole budget a source may occupy.
	instanceMemoryPages = 1024
	// defaultPoolSize bounds concurrent calls into one source. A plugin
	// instance is single threaded, so parallelism comes from separate
	// instances of the same compiled module.
	defaultPoolSize = 2
	// defaultCallTimeout bounds one plugin export call, including every host
	// request it makes.
	defaultCallTimeout = 90 * time.Second
	// maxUnscrambleBytes and maxUnscrambleDimension bound the image handed to
	// unscramble_image so a malformed descriptor cannot exhaust the instance
	// budget.
	maxUnscrambleBytes     = 16 << 20
	maxUnscrambleDimension = 8192
)

// exportNames is the full export surface of the plugin contract. Presence is
// probed once at load so a call on an absent optional export fails without
// entering the guest.
var exportNames = []string{
	ExportGetMetadata,
	ExportGetFilters,
	ExportSearch,
	ExportGetDetails,
	ExportGetPages,
	ExportUnscrambleImage,
}

// loadedPlugin owns one compiled WebAssembly module and a pool of instances
// created from it. Compilation happens once; instances are cheap to create and
// are reused across calls.
type loadedPlugin struct {
	id      string
	meta    SourceMetadata
	exports map[string]bool

	compiled    *extism.CompiledPlugin
	callTimeout time.Duration
	idle        chan *extism.Plugin

	mu     sync.Mutex
	live   int
	closed bool
}

// loadPlugin compiles wasm, wires the host imports for sourceID and reads the
// source metadata. A module whose ABI version does not match is rejected
// before it is usable.
func loadPlugin(ctx context.Context, sourceID string, wasm []byte, fetcher *Fetcher, storage Storage) (*loadedPlugin, error) {
	manifest := extism.Manifest{
		Wasm: []extism.Wasm{extism.WasmData{Data: wasm, Name: "main"}},
		Memory: &extism.ManifestMemory{
			MaxPages: instanceMemoryPages,
			// A negative bound selects the runtime default. Both limits apply to
			// Extism's own facilities, which this contract does not use: network
			// access goes through makinuki_fetch and persistence through
			// makinuki_storage_set.
			MaxHttpResponseBytes: -1,
			MaxVarBytes:          -1,
		},
		Timeout: uint64(defaultCallTimeout / time.Millisecond),
	}
	// WASI is required at load: plugins are compiled against it and fail to
	// instantiate without the imports it provides.
	config := extism.PluginConfig{EnableWasi: true}

	compiled, err := extism.NewCompiledPlugin(ctx, manifest, config, hostFunctions(sourceID, fetcher, storage))
	if err != nil {
		return nil, CodedError(CodeParsingError, "compiling source %s failed: %v", sourceID, err)
	}

	p := &loadedPlugin{
		id:          sourceID,
		compiled:    compiled,
		callTimeout: defaultCallTimeout,
		idle:        make(chan *extism.Plugin, defaultPoolSize),
		exports:     map[string]bool{},
	}

	instance, err := p.newInstance(ctx)
	if err != nil {
		_ = compiled.Close(ctx)
		return nil, err
	}
	for _, name := range exportNames {
		p.exports[name] = instance.FunctionExists(name)
	}
	p.release(instance, nil)

	if !p.exports[ExportGetMetadata] {
		_ = p.close(ctx)
		return nil, CodedError(CodeParsingError, "source %s does not export %s", sourceID, ExportGetMetadata)
	}
	meta, err := p.Metadata(ctx)
	if err != nil {
		_ = p.close(ctx)
		return nil, err
	}
	if meta.ABIVersion != ABIVersion {
		_ = p.close(ctx)
		return nil, CodedError(CodeUnsupportedMedia,
			"source %s targets ABI version %d, this runtime implements %d",
			sourceID, meta.ABIVersion, ABIVersion)
	}
	p.meta = meta
	return p, nil
}

// probeMetadata reads the metadata of an uninstalled module. Host calls made
// during the probe are backed by throwaway storage, so nothing the module
// writes is persisted.
func probeMetadata(ctx context.Context, wasm []byte) (SourceMetadata, error) {
	scratch := NewMemoryStorage()
	p, err := loadPlugin(ctx, "", wasm, NewFetcher(scratch, nil), scratch)
	if err != nil {
		return SourceMetadata{}, err
	}
	defer p.close(ctx)
	return p.meta, nil
}

// moduleConfig supplies the real host clocks and entropy source. The wazero
// defaults are deterministic stubs, which break plugins that sign requests or
// generate identifiers.
func moduleConfig() wazero.ModuleConfig {
	return wazero.NewModuleConfig().
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep().
		WithRandSource(rand.Reader)
}

// newInstance creates one instance and counts it against the pool.
func (p *loadedPlugin) newInstance(ctx context.Context) (*extism.Plugin, error) {
	instance, err := p.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: moduleConfig()})
	if err != nil {
		return nil, CodedError(CodeMemoryLimitExceeded, "instantiating source %s failed: %v", p.id, err)
	}
	p.mu.Lock()
	p.live++
	p.mu.Unlock()
	return instance, nil
}

// acquire takes an idle instance, creates one while the pool is below its
// bound, or waits for a call in flight to finish.
func (p *loadedPlugin) acquire(ctx context.Context) (*extism.Plugin, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, CodedError(CodeSourceOffline, "source %s is not loaded", p.id)
	}
	room := p.live < defaultPoolSize
	p.mu.Unlock()

	select {
	case instance := <-p.idle:
		return instance, nil
	default:
	}
	if room {
		return p.newInstance(ctx)
	}
	select {
	case instance := <-p.idle:
		return instance, nil
	case <-ctx.Done():
		return nil, CodedError(CodeNetworkTimeout, "source %s stayed busy: %v", p.id, ctx.Err())
	}
}

// release returns an instance to the pool. An instance whose call failed is
// discarded: a guest interrupted mid call may hold inconsistent state, and a
// timeout closes its underlying module.
func (p *loadedPlugin) release(instance *extism.Plugin, callErr error) {
	if callErr != nil {
		p.discard(instance)
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		p.discard(instance)
		return
	}
	select {
	case p.idle <- instance:
	default:
		p.discard(instance)
	}
}

func (p *loadedPlugin) discard(instance *extism.Plugin) {
	p.mu.Lock()
	p.live--
	p.mu.Unlock()
	_ = instance.Close(context.Background())
}

// close releases every instance and the compiled module.
func (p *loadedPlugin) close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	for {
		select {
		case instance := <-p.idle:
			p.discard(instance)
		default:
			return p.compiled.Close(ctx)
		}
	}
}

// callRaw invokes one export and returns its raw output.
func (p *loadedPlugin) callRaw(ctx context.Context, name string, input []byte) ([]byte, error) {
	if !p.exports[name] {
		return nil, CodedError(CodeParsingError, "source %s does not export %s", p.id, name)
	}

	ctx, cancel := context.WithTimeout(ctx, p.callTimeout)
	defer cancel()

	instance, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}
	exit, out, callErr := instance.CallWithContext(ctx, name, input)
	p.release(instance, callErr)

	if callErr != nil {
		if ctx.Err() != nil {
			return nil, CodedError(CodeNetworkTimeout, "%s.%s exceeded %s", p.id, name, p.callTimeout)
		}
		return nil, CodedError(CodeParsingError, "%s.%s failed: %v", p.id, name, callErr)
	}
	if exit != 0 {
		return nil, CodedError(CodeParsingError, "%s.%s exited with status %d", p.id, name, exit)
	}
	return out, nil
}

// call invokes an export whose output is a result envelope and returns the
// payload only when the plugin reported success.
func (p *loadedPlugin) call(ctx context.Context, name string, input any) (json.RawMessage, error) {
	var encoded []byte
	if input != nil {
		var err error
		encoded, err = json.Marshal(input)
		if err != nil {
			return nil, CodedError(CodeParsingError, "encoding input for %s.%s failed: %v", p.id, name, err)
		}
	}
	out, err := p.callRaw(ctx, name, encoded)
	if err != nil {
		return nil, err
	}
	return unwrapEnvelope(p.id, name, out)
}

// unwrapEnvelope reads the result envelope. Success is decided by the ok
// field alone; an error is never inferred from the shape of the payload.
func unwrapEnvelope(sourceID, name string, out []byte) (json.RawMessage, error) {
	var envelope pluginResult
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, CodedError(CodeParsingError, "%s.%s returned an unreadable envelope: %v", sourceID, name, err)
	}
	if envelope.OK == nil {
		return nil, CodedError(CodeParsingError, "%s.%s returned an envelope without an ok field", sourceID, name)
	}
	if !*envelope.OK {
		if envelope.Error == nil {
			return nil, CodedError(CodeParsingError, "%s.%s failed without an error payload", sourceID, name)
		}
		return nil, CodedError(envelope.Error.Code, "%s", envelope.Error.Message)
	}
	if len(envelope.Data) == 0 {
		return nil, CodedError(CodeParsingError, "%s.%s succeeded without data", sourceID, name)
	}
	return envelope.Data, nil
}

// decodeResult decodes an unwrapped payload into target.
func decodeResult[T any](payload json.RawMessage, sourceID, name string) (T, error) {
	var target T
	if err := json.Unmarshal(payload, &target); err != nil {
		return target, CodedError(CodeParsingError, "%s.%s returned unexpected data: %v", sourceID, name, err)
	}
	return target, nil
}

// Metadata reads the static source descriptor.
func (p *loadedPlugin) Metadata(ctx context.Context) (SourceMetadata, error) {
	out, err := p.callRaw(ctx, ExportGetMetadata, nil)
	if err != nil {
		return SourceMetadata{}, err
	}
	// Metadata is returned bare rather than wrapped in a result envelope.
	var meta SourceMetadata
	if err := json.Unmarshal(out, &meta); err != nil {
		return SourceMetadata{}, CodedError(CodeParsingError, "%s.%s returned unexpected data: %v", p.id, ExportGetMetadata, err)
	}
	if meta.ID == "" {
		return SourceMetadata{}, CodedError(CodeParsingError, "%s.%s returned metadata without an id", p.id, ExportGetMetadata)
	}
	return meta, nil
}

// Filters reads the filter schemas the source accepts in a search query. The
// schemas are a union of filter kinds with kind specific default values, so
// they are relayed as the plugin produced them instead of being reshaped.
func (p *loadedPlugin) Filters(ctx context.Context) (json.RawMessage, error) {
	if !p.exports[ExportGetFilters] {
		return json.RawMessage("[]"), nil
	}
	out, err := p.callRaw(ctx, ExportGetFilters, nil)
	if err != nil {
		return nil, err
	}
	var filters []json.RawMessage
	if err := json.Unmarshal(out, &filters); err != nil {
		return nil, CodedError(CodeParsingError, "%s.%s returned unexpected data: %v", p.id, ExportGetFilters, err)
	}
	return json.RawMessage(out), nil
}

func (p *loadedPlugin) Search(ctx context.Context, query SearchQuery) (PageResult, error) {
	payload, err := p.call(ctx, ExportSearch, query)
	if err != nil {
		return PageResult{}, err
	}
	return decodeResult[PageResult](payload, p.id, ExportSearch)
}

func (p *loadedPlugin) Details(ctx context.Context, mangaID string) (MangaDetails, error) {
	payload, err := p.call(ctx, ExportGetDetails, mangaID)
	if err != nil {
		return MangaDetails{}, err
	}
	return decodeResult[MangaDetails](payload, p.id, ExportGetDetails)
}

func (p *loadedPlugin) Pages(ctx context.Context, chapterID string) ([]PageItem, error) {
	payload, err := p.call(ctx, ExportGetPages, chapterID)
	if err != nil {
		return nil, err
	}
	return decodeResult[[]PageItem](payload, p.id, ExportGetPages)
}

// Unscramble descrambles one image through the source. The export takes and
// returns raw image bytes rather than JSON. An empty result buffer is the
// contract's failure signal rather than an error envelope, so it is translated
// into UNSCRAMBLE_FAILED here.
func (p *loadedPlugin) Unscramble(ctx context.Context, data []byte) ([]byte, error) {
	if !p.exports[ExportUnscrambleImage] {
		return nil, CodedError(CodeUnsupportedMedia, "source %s does not descramble images", p.id)
	}
	if len(data) == 0 {
		return nil, CodedError(CodeUnscrambleFailed, "no image data to descramble")
	}
	if len(data) > maxUnscrambleBytes {
		return nil, CodedError(CodeMemoryLimitExceeded,
			"image is %d bytes, above the %d byte descramble cap", len(data), maxUnscrambleBytes)
	}
	if err := checkImageBounds(data); err != nil {
		return nil, err
	}

	out, err := p.callRaw(ctx, ExportUnscrambleImage, data)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, CodedError(CodeUnscrambleFailed, "source %s could not descramble the image", p.id)
	}
	return out, nil
}

// checkImageBounds rejects images above the dimension cap. An undecodable
// buffer is passed through: the source may handle a format this host does not
// recognize, and a genuine failure surfaces as an empty result.
func checkImageBounds(data []byte) error {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	if config.Width > maxUnscrambleDimension || config.Height > maxUnscrambleDimension {
		return CodedError(CodeMemoryLimitExceeded,
			"image is %dx%d, above the %dx%d descramble cap",
			config.Width, config.Height, maxUnscrambleDimension, maxUnscrambleDimension)
	}
	return nil
}

func (p *loadedPlugin) String() string {
	return fmt.Sprintf("%s v%s", p.meta.ID, p.meta.Version)
}
