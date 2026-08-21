package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
)

// withFakeHome points config.Home at a temporary directory as well as the data
// dir, so a test never writes into the real user's home.
func withFakeHome(t *testing.T) string {
	t.Helper()
	oldHome, oldDir, oldDB, oldConfig := config.Home, config.AtmDir, config.AtmDB, config.ConfigPath
	home := t.TempDir()
	config.Home = home
	config.AtmDir = filepath.Join(home, ".atm")
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	config.ConfigPath = filepath.Join(config.AtmDir, "config.json")
	if err := os.MkdirAll(config.AtmDir, 0700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	t.Cleanup(func() {
		config.Home, config.AtmDir, config.AtmDB, config.ConfigPath = oldHome, oldDir, oldDB, oldConfig
	})
	return home
}

// The log is attached to public bug reports, and `atm todo add "<title>"` and
// `atm knowledge import <path>` put content directly in argv. Recording the
// subcommand without its arguments is what keeps that out.
func TestCommandLoggingExcludesArguments(t *testing.T) {
	withFakeHome(t)
	oldArgs := os.Args
	os.Args = []string{"atm", "todo", "add", "SECRET_TODO_TITLE_IN_ARGV"}
	t.Cleanup(func() { os.Args = oldArgs })

	logging.Failure("command_failed", failedCommandPath(), errors.New("boom"), nil)

	lines, err := logging.Tail(logging.Path(), 0)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "SECRET_TODO_TITLE_IN_ARGV") {
		t.Fatalf("log captured a command argument: %s", joined)
	}
	if !strings.Contains(joined, "atm todo add") {
		t.Errorf("log lost the command path, so the failure is not locatable: %s", joined)
	}
}
