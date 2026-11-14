// Package adminui holds the built admin UI (web/admin/dist) inside the binary (req_001 D-151,
// D-152). Git holds only dist/.gitkeep, so a plain `go build` needs no npm and embeds no UI;
// internal/webui then serves its one-page stub. `make build` runs the Vite build first.
package adminui

import (
	"embed"
	"io/fs"
)

// The all: prefix keeps dist/.gitkeep, so the pattern matches on a tree where the UI was never
// built. webui never serves a dot file.
//
//go:embed all:dist
var dist embed.FS

// Files returns the built admin UI with index.html at its root (empty apart from .gitkeep when
// the UI was not built).
func Files() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// fs.Sub fails only on an invalid name; "dist" is valid.
		panic(err)
	}
	return sub
}
