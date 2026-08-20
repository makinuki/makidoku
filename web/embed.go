package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed dist
var distFS embed.FS

// Mount serves the embedded React build at /. The dist directory may hold
// only a placeholder index.html, so the handler returns a notice when the
// real build is absent. The placeholder will be replaced by a Vite build and
// embedded via //go:embed all:dist later.
func Mount(r chi.Router) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist missing at compile time - serve notice.
		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><title>MakiDoku</title><p>MakiDoku - web/dist not yet built. Run the daemon and use /api/*.</p>`))
		})
		return
	}

	// Check if dist contains an index.html; if not, serve notice.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><title>MakiDoku</title><p>MakiDoku - web UI not yet built. API at /api/*.</p>`))
		})
		return
	}

	fileServer := http.FileServer(http.FS(sub))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		// SPA fallback: try file, fall back to index.html.
		path := req.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		if _, err := fs.Stat(sub, path[1:]); err != nil {
			req.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, req)
	})
}
