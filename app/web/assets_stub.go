//go:build !webui

package webassets

import (
	"fmt"
	"io/fs"
)

func Assets() (fs.FS, error) {
	return nil, fmt.Errorf("this CLI build has no Web workspace; install the full release or run make build (requires Node.js only when building)")
}
