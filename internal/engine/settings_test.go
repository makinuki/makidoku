package engine

import (
	"encoding/json"
	"testing"
)

func TestBedSerializeSettingValue(t *testing.T) {
	selectSchema := SettingSchema{
		ID:      "mirror",
		Type:    SettingKindSelect,
		Options: []SettingOption{{Label: "Primary", Value: "primary"}},
	}
	cases := []struct {
		name    string
		schema  SettingSchema
		value   any
		want    string
		wantErr bool
	}{
		{name: "checkbox on", schema: SettingSchema{ID: "flag", Type: SettingKindCheckbox}, value: true, want: "true"},
		{name: "checkbox off", schema: SettingSchema{ID: "flag", Type: SettingKindCheckbox}, value: false, want: "false"},
		{name: "checkbox rejects string", schema: SettingSchema{ID: "flag", Type: SettingKindCheckbox}, value: "true", wantErr: true},
		{name: "select declared option", schema: selectSchema, value: "primary", want: "primary"},
		{name: "select undeclared option", schema: selectSchema, value: "other", wantErr: true},
		{name: "select rejects bool", schema: selectSchema, value: true, wantErr: true},
		{name: "text passes through", schema: SettingSchema{ID: "base_url", Type: SettingKindText}, value: "https://example.test", want: "https://example.test"},
		{name: "text rejects bool", schema: SettingSchema{ID: "base_url", Type: SettingKindText}, value: false, wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := SerializeSettingValue(test.schema, test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("serialized = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFindSetting(t *testing.T) {
	schemas := []SettingSchema{{ID: "base_url", Type: SettingKindText}, {ID: "data_saver", Type: SettingKindCheckbox}}
	if schema, ok := FindSetting(schemas, "data_saver"); !ok || schema.Type != SettingKindCheckbox {
		t.Fatalf("FindSetting(data_saver) = %+v, %v", schema, ok)
	}
	if _, ok := FindSetting(schemas, "missing"); ok {
		t.Fatal("FindSetting(missing) reported a match")
	}
}

// The settings payload is a union of kinds, so it must decode into the shared
// schema struct with each kind carrying only its own fields.
func TestDecodeSettingSchemas(t *testing.T) {
	payload := `[
		{"id":"data_saver","title":"Data saver","type":"checkbox","default":false},
		{"id":"mirror","title":"Mirror","type":"select","options":[{"label":"A","value":"a"}],"default":"a"},
		{"id":"base_url","title":"Address","type":"text","placeholder":"https://example.test","default":"https://example.test","sensitive":true}
	]`
	var schemas []SettingSchema
	if err := json.Unmarshal([]byte(payload), &schemas); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if len(schemas) != 3 {
		t.Fatalf("decoded %d settings, want 3", len(schemas))
	}
	if schemas[0].Type != SettingKindCheckbox || string(schemas[0].Default) != "false" {
		t.Fatalf("checkbox = %+v", schemas[0])
	}
	if schemas[1].Type != SettingKindSelect || len(schemas[1].Options) != 1 {
		t.Fatalf("select = %+v", schemas[1])
	}
	if schemas[2].Type != SettingKindText || !schemas[2].Sensitive || schemas[2].Placeholder == "" {
		t.Fatalf("text = %+v", schemas[2])
	}
}

func TestDecodeSourceMetadataHints(t *testing.T) {
	payload := `{"id":"demo","name":"Demo","version":"1.0.0","abiVersion":1,"lang":"en","baseUrl":"https://example.test","iconUrl":"https://example.test/icon.png","nsfw":false,"rateLimit":{"intervalMs":500,"burst":1},"retry":{"maxAttempts":2,"backoffMs":3000}}`
	var meta SourceMetadata
	if err := json.Unmarshal([]byte(payload), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.RateLimit == nil || meta.RateLimit.IntervalMs == nil || *meta.RateLimit.IntervalMs != 500 {
		t.Fatalf("rateLimit = %+v", meta.RateLimit)
	}
	if meta.Retry == nil || meta.Retry.BackoffMs == nil || *meta.Retry.BackoffMs != 3000 {
		t.Fatalf("retry = %+v", meta.Retry)
	}
}
