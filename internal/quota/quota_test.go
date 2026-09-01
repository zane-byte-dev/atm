package quota

import (
	"path/filepath"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
)

// withTempAtmDir points the data root at a temporary directory. The provider
// card cache lives under it, so without this a test would read and rewrite the
// developer's own placeholders.
func withTempAtmDir(t *testing.T) string {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir, config.AtmDB = oldDir, oldDB
	})
	return dir
}

func quotaTestCall() application.Call {
	return application.Call{
		RequestID: "quota-service-test",
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginCLI,
		},
	}
}
