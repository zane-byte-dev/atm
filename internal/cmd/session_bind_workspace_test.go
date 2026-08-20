package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// gitRepoDir makes a directory look like a git root to config.ProjectFromPath,
// which walks up looking for `.git` and then reads the origin remote. No origin
// is configured, so the project name falls back to the directory name — which is
// exactly the shape of the real failure: a task filed against `atm` bound to a
// session sitting in `wanda`.
func gitRepoDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBindRefusesASessionWorkingInAnotherProject(t *testing.T) {
	withTempAtmDir(t)
	oldSession, oldAgent, oldJSON := sessionIDFlag, sessionBindAgentFlag, jsonOutput
	oldProject, oldCWD, oldForce := sessionBindProjectFlag, sessionBindCWDFlag, sessionBindForceFlag
	t.Cleanup(func() {
		sessionIDFlag, sessionBindAgentFlag, jsonOutput = oldSession, oldAgent, oldJSON
		sessionBindProjectFlag, sessionBindCWDFlag, sessionBindForceFlag = oldProject, oldCWD, oldForce
	})
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Stay in ATM", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	sessionIDFlag = "workspace-refusal"
	jsonOutput = false
	sessionBindAgentFlag = "codex"
	sessionBindProjectFlag = ""
	sessionBindForceFlag = false

	wrong := gitRepoDir(t, "wanda")
	sessionBindCWDFlag = wrong
	err := runSessionBind(sessionBindCmd, []string{"t1"})
	if err == nil {
		t.Fatal("binding an atm todo to a wanda worktree was accepted")
	}
	for _, want := range []string{"t1", "atm", "wanda", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestBindAcceptsTheMatchingProjectAndAnyWorktreeOfIt(t *testing.T) {
	withTempAtmDir(t)
	oldSession, oldAgent, oldJSON := sessionIDFlag, sessionBindAgentFlag, jsonOutput
	oldProject, oldCWD, oldForce := sessionBindProjectFlag, sessionBindCWDFlag, sessionBindForceFlag
	t.Cleanup(func() {
		sessionIDFlag, sessionBindAgentFlag, jsonOutput = oldSession, oldAgent, oldJSON
		sessionBindProjectFlag, sessionBindCWDFlag, sessionBindForceFlag = oldProject, oldCWD, oldForce
	})
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Work in ATM", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	sessionIDFlag = "workspace-match"
	jsonOutput = false
	sessionBindAgentFlag = "codex"
	sessionBindProjectFlag = ""
	sessionBindForceFlag = false

	repo := gitRepoDir(t, "atm")
	sessionBindCWDFlag = repo
	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("matching project refused: %v", err)
	}
	// A subdirectory resolves through the same git root, so working inside the
	// repository is not a different project.
	nested := filepath.Join(repo, "internal", "cmd")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	sessionBindCWDFlag = nested
	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("subdirectory of the repository refused: %v", err)
	}
}

func TestBindWorkspaceCheckStaysOutOfTheWayWhenItCannotKnow(t *testing.T) {
	for _, test := range []struct {
		name        string
		todoProject string
		force       bool
	}{
		// No project means the Todo makes no claim about where it belongs.
		{name: "todo has no project"},
		// Deliberate cross-project work stays possible.
		{name: "forced", todoProject: "atm", force: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			withTempAtmDir(t)
			oldSession, oldAgent, oldJSON := sessionIDFlag, sessionBindAgentFlag, jsonOutput
			oldProject, oldCWD, oldForce := sessionBindProjectFlag, sessionBindCWDFlag, sessionBindForceFlag
			t.Cleanup(func() {
				sessionIDFlag, sessionBindAgentFlag, jsonOutput = oldSession, oldAgent, oldJSON
				sessionBindProjectFlag, sessionBindCWDFlag, sessionBindForceFlag = oldProject, oldCWD, oldForce
			})
			if err := seedTodos(store.Todo{
				ID: "t1", Title: "Cross-project work", Priority: "P1", Status: store.TodoStatusOpen,
				Project: test.todoProject, Created: store.Today(),
			}); err != nil {
				t.Fatal(err)
			}
			sessionIDFlag = "workspace-unknown"
			jsonOutput = false
			sessionBindAgentFlag = "codex"
			sessionBindProjectFlag = ""
			sessionBindCWDFlag = gitRepoDir(t, "wanda")
			sessionBindForceFlag = test.force
			if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}
