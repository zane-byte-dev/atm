package cmd

import (
	"os"
	"testing"
)

// Command tests exercise the real todo mutation paths, including lifecycle
// notification calls. Their databases are isolated, but macOS notifications
// are process-external side effects, so disable them for the whole test binary.
// Individual skipLocalNotification tests override this value with t.Setenv.
func TestMain(m *testing.M) {
	if err := os.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestCommandTestsDisableLocalNotificationsByDefault(t *testing.T) {
	if !skipLocalNotification() {
		t.Fatal("command tests must not emit system notifications")
	}
}
