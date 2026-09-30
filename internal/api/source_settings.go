package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/makinuki/makidoku/internal/engine"
)

// sourceSettingResponse is one setting a source declares together with its
// stored state. A sensitive setting never carries its value, only hasValue.
type sourceSettingResponse struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Type        string                 `json:"type"`
	Options     []engine.SettingOption `json:"options,omitempty"`
	Placeholder string                 `json:"placeholder,omitempty"`
	Default     any                    `json:"default,omitempty"`
	Sensitive   bool                   `json:"sensitive,omitempty"`
	// Value is the effective value: the stored value when one exists, else the
	// schema default. It is absent for a sensitive setting, whose value is
	// write-only.
	Value    any  `json:"value,omitempty"`
	HasValue bool `json:"hasValue"`
}

// sourceSettings lists the settings a source declares, each with its stored
// value. The response is empty for a source without get_settings.
func (s *Server) sourceSettings(w http.ResponseWriter, r *http.Request) {
	items, err := s.buildSourceSettings(r, chi.URLParam(r, "sourceID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) buildSourceSettings(r *http.Request, sourceID string) ([]sourceSettingResponse, error) {
	schemas, err := s.engine.Settings(r.Context(), sourceID)
	if err != nil {
		return nil, err
	}
	items := make([]sourceSettingResponse, 0, len(schemas))
	for _, schema := range schemas {
		item, err := s.buildSourceSetting(r, sourceID, schema)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Server) buildSourceSetting(r *http.Request, sourceID string, schema engine.SettingSchema) (sourceSettingResponse, error) {
	item := sourceSettingResponse{
		ID:          schema.ID,
		Title:       schema.Title,
		Description: schema.Description,
		Type:        string(schema.Type),
		Options:     schema.Options,
		Placeholder: schema.Placeholder,
		Sensitive:   schema.Sensitive,
	}
	if len(schema.Default) > 0 {
		var def any
		if err := json.Unmarshal(schema.Default, &def); err != nil {
			return item, engine.CodedError(engine.CodeParsingError, "setting %q has an unreadable default: %v", schema.ID, err)
		}
		item.Default = def
	}
	stored, found, err := s.engine.SettingValue(r.Context(), sourceID, schema.ID)
	if err != nil {
		return item, err
	}
	item.HasValue = found
	// A sensitive value is write-only: the client learns whether it is set,
	// never what it is.
	if schema.Sensitive {
		return item, nil
	}
	if found {
		item.Value = decodeStoredSetting(schema, stored)
	} else if item.Default != nil {
		item.Value = item.Default
	}
	return item, nil
}

// decodeStoredSetting renders a stored string into the value type the setting
// declares. A checkbox is stored as "true"/"false"; select and text values are
// stored verbatim.
func decodeStoredSetting(schema engine.SettingSchema, stored string) any {
	if schema.Type == engine.SettingKindCheckbox {
		return stored == "true"
	}
	return stored
}

// putSourceSetting writes one setting value. A null value deletes the key,
// restoring the schema default; anything else is validated against the
// declared setting before it is stored.
func (s *Server) putSourceSetting(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	var body struct {
		Value *json.RawMessage `json:"value"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Value == nil {
		writeBadRequest(w, "value is required")
		return
	}
	var value any
	if !bytes.Equal(bytes.TrimSpace(*body.Value), []byte("null")) {
		if err := json.Unmarshal(*body.Value, &value); err != nil {
			writeBadRequest(w, "value is not valid JSON: "+err.Error())
			return
		}
	}
	if err := s.engine.SetSettingValue(r.Context(), sourceID, key, value); err != nil {
		writeError(w, err)
		return
	}
	schemas, err := s.engine.Settings(r.Context(), sourceID)
	if err != nil {
		writeError(w, err)
		return
	}
	schema, ok := engine.FindSetting(schemas, key)
	if !ok {
		writeError(w, engine.CodedError(engine.CodeNotFound, "source %s declares no setting %q", sourceID, key))
		return
	}
	item, err := s.buildSourceSetting(r, sourceID, schema)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
