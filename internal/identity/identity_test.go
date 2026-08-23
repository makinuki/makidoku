package identity

import (
	"strings"
	"testing"
)

func TestNewIDReturnsOpaqueUUIDv7(t *testing.T) {
	first, err := New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("generated IDs must be unique")
	}
	if !IsValid(first) || !IsValid(second) {
		t.Fatalf("generated IDs are not valid UUIDs: %q %q", first, second)
	}
	if strings.Contains(first, ":") || strings.Contains(second, ":") {
		t.Fatal("generated IDs must not encode source delimiters")
	}
}

func TestIsValidRejectsExternalAndEmptyIDs(t *testing.T) {
	for _, value := range []string{"", "mangadex:remote-id", "remote-id", "not-a-uuid"} {
		if IsValid(value) {
			t.Errorf("IsValid(%q) = true", value)
		}
	}
}
