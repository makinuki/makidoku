package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

func TestFetchImagePreservesBytesAndHeaders(t *testing.T) {
	want := []byte{0, 1, 2, 3, 0xff}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "https://reader.example/" {
			t.Errorf("referer = %q", got)
		}
		_, _ = w.Write(want)
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	got, err := fetcher.FetchImage(context.Background(), "source", server.URL, map[string]string{
		"Referer": "https://reader.example/",
	})
	if err != nil {
		t.Fatalf("fetch image: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes = %v, want %v", got, want)
	}
}

func TestFetchImageRejectsBodiesAboveTransferCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}))
	defer server.Close()

	fetcher := NewFetcher(NewMemoryStorage(), nil)
	_, err := fetcher.FetchImage(context.Background(), "source", server.URL, nil)
	if CodeOf(err) != CodeMemoryLimitExceeded {
		t.Fatalf("error = %v, want %s", err, CodeMemoryLimitExceeded)
	}
}

func TestEngineFetchImageUsesFetcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()

	eng := &Engine{fetcher: NewFetcher(NewMemoryStorage(), nil)}
	got, err := eng.FetchImage(context.Background(), "source", server.URL, nil)
	if err != nil {
		t.Fatalf("fetch image: %v", err)
	}
	if string(got) != "image" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestEngineFetchImageUsesSourceBaseURLAsDefaultReferer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "https://mangadex.org/" {
			t.Errorf("referer = %q", got)
		}
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	handle, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.Exec(`INSERT INTO sources(
		id, name, version, abi_version, lang, base_url, wasm_path, installed_at
	) VALUES('mangadex', 'MangaDex', '1.0.0', 1, 'multi', 'https://mangadex.org', 'mangadex.wasm', ?)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	eng := New(handle, Options{DataDir: t.TempDir()})
	if _, err := eng.FetchImage(context.Background(), "mangadex", server.URL, nil); err != nil {
		t.Fatal(err)
	}
}
