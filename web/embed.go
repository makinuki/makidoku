package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:generate pnpm run build
//go:embed dist
var distFS embed.FS

// Mount serves the embedded React build at /. Unknown non-API paths fall back
// to index.html so React Router can handle deep links in the single binary.
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
		// SPA fallback: try a static asset, then serve index.html.
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
