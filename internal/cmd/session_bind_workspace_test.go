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
	old := sessionBindForceFlag
	t.Cleanup(func() { sessionBindForceFlag = old })
	sessionBindForceFlag = false

	todo := &store.Todo{ID: "t1", Project: "atm"}
	wrong := gitRepoDir(t, "wanda")
	err := checkBindingWorkspace(todo, wrong)
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
	old := sessionBindForceFlag
	t.Cleanup(func() { sessionBindForceFlag = old })
	sessionBindForceFlag = false

	todo := &store.Todo{ID: "t1", Project: "atm"}
	repo := gitRepoDir(t, "atm")
	if err := checkBindingWorkspace(todo, repo); err != nil {
		t.Fatalf("matching project refused: %v", err)
	}
	// A subdirectory resolves through the same git root, so working inside the
	// repository is not a different project.
	nested := filepath.Join(repo, "internal", "cmd")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := checkBindingWorkspace(todo, nested); err != nil {
		t.Fatalf("subdirectory of the repository refused: %v", err)
	}
}

func TestBindWorkspaceCheckStaysOutOfTheWayWhenItCannotKnow(t *testing.T) {
	old := sessionBindForceFlag
	t.Cleanup(func() { sessionBindForceFlag = old })

	wrong := gitRepoDir(t, "wanda")
	for _, test := range []struct {
		name  string
		todo  *store.Todo
		cwd   string
		force bool
	}{
		// No project means the Todo makes no claim about where it belongs.
		{name: "todo has no project", todo: &store.Todo{ID: "t1"}, cwd: wrong},
		// A client that reports no directory cannot be checked without guessing.
		{name: "session has no cwd", todo: &store.Todo{ID: "t1", Project: "atm"}, cwd: ""},
		// Deliberate cross-project work stays possible.
		{name: "forced", todo: &store.Todo{ID: "t1", Project: "atm"}, cwd: wrong, force: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessionBindForceFlag = test.force
			if err := checkBindingWorkspace(test.todo, test.cwd); err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}
