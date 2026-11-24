// Package web embeds the browser client into the hub binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed assets
var files embed.FS

// Handler serves the single-page app. Unknown non-file paths fall back to
// index.html so client-side routes work on refresh.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			p = "index.html"
		}
		if st, err := fs.Stat(sub, p); err != nil || st.IsDir() {
			p = "index.html"
		}
		if p == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			data, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, "web client missing", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(data)
			return
		}
		// Filenames are not content-hashed, so clients must revalidate after
		// an upgrade instead of caching assets for a year.
		w.Header().Set("Cache-Control", "no-cache")
		if ctype := assetContentType(p); ctype != "" {
			// Pin the types the app depends on instead of trusting the host
			// MIME table: Windows registry mappings and unregistered
			// extensions otherwise yield text/plain, which nosniff turns into
			// a refused module script or manifest.
			w.Header().Set("Content-Type", ctype)
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + p
		fileServer.ServeHTTP(w, r2)
	})
}

func assetContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}
