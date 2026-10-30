package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// staticHandler serves the vendored css and (later) htmx from the binary.
// The url namespace is versioned by path only, so a short max-age with the
// ETag from FileServer is enough.
func staticHandler() http.Handler {
	root, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Only possible if the embedded path above stops existing, which is a
		// build error we want to hear about immediately.
		panic("web: embedded static directory missing: " + err.Error())
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.StripPrefix("/static/", files).ServeHTTP(w, r)
	})
}
