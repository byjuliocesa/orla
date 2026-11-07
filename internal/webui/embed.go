//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

// dist is web/out (the Vite build), copied here by `make web`: go:embed cannot reach outside this directory.
//
//go:embed all:dist
var dist embed.FS

// Files returns the embedded client.
func Files() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the directory is embedded above; it cannot be missing
	}
	return sub
}
