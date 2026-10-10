// Package uiassets contains the shared presentation layer for the local and cloud consoles.
package uiassets

import (
	"embed"
	"net/http"
	"path"
)

//go:embed spatial.css spatial.js
var assets embed.FS

// Serve serves only the two explicitly named UI resources; it never exposes device data.
func Serve(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	if name != "spatial.css" && name != "spatial.js" {
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if name == "spatial.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
