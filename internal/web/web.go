// Package web serves the embedded admin SPA, the OpenAPI spec and Swagger UI.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assets embed.FS

// FS returns the embedded asset filesystem rooted at the assets directory.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

// Handler serves the static assets (SPA at /, styles, app.js, openapi.yaml,
// docs.html). The SPA uses hash routing, so no server-side fallback is needed.
//
// A no-cache header is set so browsers always revalidate: otherwise, after a
// binary upgrade, a cached app.js/index.html would keep showing the old UI.
func Handler() http.Handler {
	fileServer := http.FileServer(http.FS(FS()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}
