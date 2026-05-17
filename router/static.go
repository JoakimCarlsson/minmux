package router

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// Static mounts a generic file server at the given URL prefix. Requests for
// paths under the prefix are served from fsys; missing files return 404.
// Useful for versioned/hashed asset directories.
//
//	r.Static("/assets/", http.Dir("./public/assets"))
func (r *Router) Static(prefix string, fsys http.FileSystem) {
	if prefix == "" {
		prefix = "/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	handler := http.StripPrefix(strings.TrimSuffix(prefix, "/"), http.FileServer(fsys))
	r.mux.Handle(http.MethodGet+" "+prefix, handler)
}

// SPA mounts a single-page app at "/". Requests for files that exist in
// fsys are served as-is; any other GET path serves index.html so client-side
// routing can claim the URL. Returns an error if index.html is missing.
//
// More specific patterns registered on the router (e.g. /v1/health) take
// precedence over the SPA fallback per net/http ServeMux semantics.
func (r *Router) SPA(fsys fs.FS) error {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return fmt.Errorf("spa: read index.html: %w", err)
	}
	fileServer := http.FileServer(http.FS(fsys))

	r.mux.HandleFunc(http.MethodGet+" /", func(w http.ResponseWriter, req *http.Request) {
		path := strings.TrimPrefix(req.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(fsys, path); err == nil {
				fileServer.ServeHTTP(w, req)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
	return nil
}
