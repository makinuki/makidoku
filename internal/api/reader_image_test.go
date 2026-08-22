package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/makinuki/makidoku/internal/db"
	"github.com/makinuki/makidoku/internal/engine"
)

func TestReaderImageUsesEngineAndRejectsUnknownHosts(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	defer imageServer.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "makidoku.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO sources(id,name,version,abi_version,lang,base_url,wasm_path,installed_at) VALUES(?,?,?,?,?,?,?,?)`, "demo", "Demo", "1", 1, "en", imageServer.URL, "demo.wasm", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepository(database)
	server := NewServer(repo, engine.New(database, engine.Options{DataDir: t.TempDir()}))
	router := chi.NewRouter()
	server.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/api/reader/image?source=demo&url="+imageServer.URL+"/page.png", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "png-data" {
		t.Fatalf("image response = %d %q", res.Code, res.Body.String())
	}
	unknown := httptest.NewRecorder()
	router.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/api/reader/image?source=demo&url=https://other.example/page.png", nil))
	if unknown.Code != http.StatusForbidden {
		t.Fatalf("unknown host status = %d", unknown.Code)
	}
}
