package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFiles embed.FS

// staticHandler serves the vendored css and (later) htmx from the binary.
//
// A file embedded in a binary carries no modification time, so the file server
// cannot send a validator of its own and a browser is entitled to keep an old
// stylesheet for as long as it likes after a deploy. That is the one way a
// correct page can still render wrong, so the handler sends an ETag built from
// the file's own bytes and asks the browser to revalidate: one 304 per page
// load, and never a stale rule.
func staticHandler() http.Handler {
	root, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Only possible if the embedded path above stops existing, which is a
		// build error we want to hear about immediately.
		panic("web: embedded static directory missing: " + err.Error())
	}
	files := http.StripPrefix("/static/", http.FileServerFS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Revalidate every time: the tag is cheap and the bytes are the truth.
		w.Header().Set("Cache-Control", "no-cache")
		name := strings.TrimPrefix(r.URL.Path, "/static/")
		if data, err := fs.ReadFile(root, name); err == nil {
			sum := sha256.Sum256(data)
			tag := `"` + hex.EncodeToString(sum[:12]) + `"`
			w.Header().Set("ETag", tag)
			if strings.Contains(r.Header.Get("If-None-Match"), tag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}
