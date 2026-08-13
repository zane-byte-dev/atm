package cmd

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// The route and both parameter names were established by trying every plausible
// spelling against Codex Desktop 0.147.0-alpha and watching which one landed.
// The ones that lose silently are the reason this is a test and not a comment:
// `threads/new?cwd=` opens the chat in the app's *last* workspace, and `q=`
// leaves the composer empty, neither of which fails loudly.
func TestCodexDeepLinkUsesTheRouteAndParametersThatActuallyWork(t *testing.T) {
	link := codexNewThreadDeepLink("/Users/tester/mox/atm", "使用 atm 实现任务 t1：修一个 bug\n先跑 atm todo doc t1。")

	if !strings.HasPrefix(link, "codex://new?") {
		t.Fatalf("wrong route: %q", link)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if got := query.Get("path"); got != "/Users/tester/mox/atm" {
		t.Errorf("path = %q", got)
	}
	if got := query.Get("prompt"); !strings.HasPrefix(got, "使用 atm 实现任务 t1") ||
		!strings.Contains(got, "\n先跑 atm todo doc t1。") {
		t.Errorf("prompt = %q", got)
	}
	if query.Has("cwd") || query.Has("q") {
		t.Errorf("the ignored spellings must not be sent: %q", link)
	}
	// Spaces percent-encoded, not `+`: the app is not a form decoder.
	if strings.Contains(link, "+") {
		t.Errorf("link uses + for spaces: %q", link)
	}
	if !strings.Contains(link, "%20") {
		t.Errorf("link does not percent-encode spaces: %q", link)
	}
}

func TestTodoHandoffOpensTheProjectDirectoryAndDoesNotStartTheTodo(t *testing.T) {
	withTempAtmDir(t)
	withCommandFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "mox", "atm")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Hand me over", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	oldOpen, oldCWD, oldPrint := openHandoffURL, todoHandoffCWDFlag, todoHandoffPrintFlag
	t.Cleanup(func() {
		openHandoffURL, todoHandoffCWDFlag, todoHandoffPrintFlag = oldOpen, oldCWD, oldPrint
	})
	todoHandoffCWDFlag, todoHandoffPrintFlag = "", false
	var opened string
	openHandoffURL = func(target string) error {
		opened = target
		return nil
	}

	if err := runTodoHandoff(todoHandoffCmd, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	query, err := url.Parse(opened)
	if err != nil {
		t.Fatal(err)
	}
	if got := query.Query().Get("path"); got != workDir {
		t.Errorf("path = %q, want the project directory %q", got, workDir)
	}
	if got := query.Query().Get("prompt"); !strings.Contains(got, "atm session bind t1") {
		t.Errorf("prompt does not carry the pointer: %q", got)
	}

	// The Todo is untouched. `session bind` is what records that work began, and
	// the human has not pressed Enter yet — a handoff that marked the Todo
	// in_progress would claim work that may never start.
	_, todo, err := loadTodoByID("t1")
	if err != nil {
		t.Fatal(err)
	}
	if todo.Status != store.TodoStatusOpen || todo.StartTS != nil {
		t.Fatalf("handoff changed the todo: status=%s start=%v", todo.Status, todo.StartTS)
	}
}

func TestTodoHandoffPrintDoesNotOpenAnything(t *testing.T) {
	withTempAtmDir(t)
	withCommandFlags(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Print only", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	oldOpen, oldCWD, oldPrint := openHandoffURL, todoHandoffCWDFlag, todoHandoffPrintFlag
	t.Cleanup(func() {
		openHandoffURL, todoHandoffCWDFlag, todoHandoffPrintFlag = oldOpen, oldCWD, oldPrint
	})
	todoHandoffCWDFlag = t.TempDir()
	todoHandoffPrintFlag = true
	openHandoffURL = func(string) error {
		t.Fatal("--print must not open Codex")
		return nil
	}
	printed := captureStdout(t, func() {
		if err := runTodoHandoff(todoHandoffCmd, []string{"t1"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(strings.TrimSpace(printed), "codex://new?") {
		t.Fatalf("stdout = %q", printed)
	}
}
