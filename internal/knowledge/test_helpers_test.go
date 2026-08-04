package knowledge

import (
	"path/filepath"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

// newDataDir isolates both halves of a knowledge test: the markdown corpus under
// the returned directory, and the database that memory, feedback, and session
// reviews now live in. Pointing only the first one at a temp directory would let
// a test write into the developer's real ~/.atm/atm.db.
func newDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
	})
	return dir
}
