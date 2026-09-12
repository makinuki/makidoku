package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	extism "github.com/extism/go-sdk"
)

// hostNamespaceMakiNuki is the plain namespace used by PDKs that import host
// functions directly. hostNamespaceInterface is the namespace generated for
// the interface declared by JavaScript plugins. Every host function is
// registered under both so a plugin resolves its imports regardless of the
// PDK it was built with.
const (
	hostNamespaceMakiNuki  = "makinuki"
	hostNamespaceInterface = "extism:host/makinuki"
)

// hostFunctions builds the four host imports for one source, registered under
// both namespaces. Every function takes and returns a single Extism memory
// offset.
func hostFunctions(sourceID string, fetcher *Fetcher, storage Storage) []extism.HostFunction {
	base := []extism.HostFunction{
		offsetFunction("makinuki_fetch", func(ctx context.Context, p *extism.CurrentPlugin, input string) uint64 {
			return writeOffset(p, fetchPayload(ctx, sourceID, fetcher, input))
		}),
		offsetFunction("makinuki_storage_get", func(ctx context.Context, p *extism.CurrentPlugin, input string) uint64 {
			value, found, err := storage.Get(sourceID, storageKey(input))
			if err != nil {
				slog.Warn("engine storage read failed", "source", sourceID, "err", err)
				return 0
			}
			// A missing key yields offset 0, which PDKs map to null. An empty
			// string is a legitimate stored value and keeps a real offset.
			if !found {
				return 0
			}
			return writeOffset(p, []byte(value))
		}),
		offsetFunction("makinuki_storage_set", func(ctx context.Context, p *extism.CurrentPlugin, input string) uint64 {
			var entry StorageEntry
			if err := json.Unmarshal([]byte(input), &entry); err != nil {
				panic("makinuki_storage_set: invalid payload: " + err.Error())
			}
			// The cap is enforced at the call boundary: an over-cap write is
			// rejected rather than silently truncated, which surfaces in the
			// plugin as a failed host call.
			if err := storage.Set(sourceID, entry.Key, entry.Value); err != nil {
				panic("makinuki_storage_set: " + err.Error())
			}
			return voidOffset(p)
		}),
		offsetFunction("makinuki_log", func(ctx context.Context, p *extism.CurrentPlugin, input string) uint64 {
			var entry LogEntry
			if err := json.Unmarshal([]byte(input), &entry); err != nil {
				entry = LogEntry{Level: "info", Message: input}
			}
			level := strings.ToLower(entry.Level)
			switch level {
			case "debug":
				slog.Debug("plugin log", "source", sourceID, "msg", entry.Message)
			case "warn":
				slog.Warn("plugin log", "source", sourceID, "msg", entry.Message)
			case "error":
				slog.Error("plugin log", "source", sourceID, "msg", entry.Message)
			default:
				slog.Info("plugin log", "source", sourceID, "msg", entry.Message)
			}
			return voidOffset(p)
		}),
	}

	out := make([]extism.HostFunction, 0, len(base)*2)
	for _, namespace := range []string{hostNamespaceMakiNuki, hostNamespaceInterface} {
		for _, fn := range base {
			fn.SetNamespace(namespace)
			out = append(out, fn)
		}
	}
	return out
}

// offsetFunction adapts a handler that reads one string and returns one
// memory offset to the Extism host function signature.
func offsetFunction(name string, handler func(context.Context, *extism.CurrentPlugin, string) uint64) extism.HostFunction {
	return extism.NewHostFunctionWithStack(
		name,
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			input, err := p.ReadString(stack[0])
			if err != nil {
				panic(name + ": unreadable input: " + err.Error())
			}
			stack[0] = handler(ctx, p, input)
		},
		[]extism.ValueType{extism.ValueTypeI64},
		[]extism.ValueType{extism.ValueTypeI64},
	)
}

// fetchPayload runs one plugin HTTP request and serializes either the
// HttpResponse or the HttpError the plugin should observe.
func fetchPayload(ctx context.Context, sourceID string, fetcher *Fetcher, input string) []byte {
	var req HttpRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return mustJSON(HttpError{
			Error:   CodeParsingError,
			Message: "makinuki_fetch received an invalid HttpRequest: " + err.Error(),
		})
	}
	resp, fetchErr := fetcher.Do(ctx, sourceID, req)
	if fetchErr != nil {
		return mustJSON(fetchErr)
	}
	return mustJSON(resp)
}

// storageKey accepts both a JSON encoded string, which is what the TypeScript
// PDK sends, and a bare key sent by other PDKs.
func storageKey(input string) string {
	var key string
	if err := json.Unmarshal([]byte(input), &key); err == nil {
		return key
	}
	return input
}

func writeOffset(p *extism.CurrentPlugin, payload []byte) uint64 {
	offset, err := p.WriteBytes(payload)
	if err != nil {
		slog.Warn("engine writing into plugin memory failed", "bytes", len(payload), "err", err)
		return 0
	}
	return offset
}

// voidOffset returns a valid memory offset for a host import the contract
// declares void. Offset zero is reserved for an absent value, which PDKs map
// to null, so a payload keeps plugin builds that read the offset
// unconditionally working.
func voidOffset(p *extism.CurrentPlugin) uint64 {
	return writeOffset(p, []byte("null"))
}

func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		// The host only marshals its own fixed structs here, so a failure
		// means a programming error rather than bad plugin input.
		return []byte(`{"error":"PARSING_ERROR","message":"host payload could not be serialized"}`)
	}
	return out
}
