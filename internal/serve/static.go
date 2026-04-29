package serve

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// staticHandler returns an http.Handler that serves the embedded
// static/ subtree at the URL prefix `/static/`. The root URL `/` is
// handled separately by indexHandler so we can return index.html
// without the user typing /index.html.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// At compile time the path is fixed; an error here means a
		// build with broken embedding tags and is unrecoverable.
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}

// indexHandler serves static/index.html for the root URL. We don't
// reuse FileServer for this because we want a strict 200 on /, not a
// 301 redirect or directory listing.
func indexHandler() http.HandlerFunc {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}
}
