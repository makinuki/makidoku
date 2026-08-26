package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

//go:generate pnpm run build
//go:embed all:dist
var distFS embed.FS

// Mount serves the embedded React build at /. Unknown non-API paths fall back
// to index.html so React Router can handle deep links in the single binary.
func Mount(r chi.Router) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		handler := missingDistHandler()
		r.Get("/*", handler)
		r.Head("/*", handler)
		return
	}

	indexBytes, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		handler := missingDistHandler()
		r.Get("/*", handler)
		r.Head("/*", handler)
		return
	}

	fileServer := http.FileServer(http.FS(sub))
	indexModTime := time.Now()

	handler := func(w http.ResponseWriter, req *http.Request) {
		cleanPath := path.Clean(req.URL.Path)
		if cleanPath == "/" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, req, "index.html", indexModTime, bytes.NewReader(indexBytes))
			return
		}

		trimmed := strings.TrimPrefix(cleanPath, "/")
		if _, err := fs.Stat(sub, trimmed); err == nil {
			if strings.HasPrefix(cleanPath, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, req)
			return
		}

		if strings.Contains(path.Base(cleanPath), ".") {
			http.NotFound(w, req)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, req, "index.html", indexModTime, bytes.NewReader(indexBytes))
	}

	r.Get("/*", handler)
	r.Head("/*", handler)
}

func missingDistHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<!doctype html><title>MakiDoku</title><p>MakiDoku - web/dist not yet built. Run pnpm --dir web build and rebuild the binary. API at /api/*.</p>`))
	}
}
