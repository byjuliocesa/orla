//go:build !webui

package webui

import "io/fs"

// Files returns nil: this binary was built without the client.
func Files() fs.FS { return nil }
