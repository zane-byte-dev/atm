//go:build webui

// Package webassets provides the browser workspace bundled with the CLI.
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

func Assets() (fs.FS, error) {
	return fs.Sub(files, "dist")
}
