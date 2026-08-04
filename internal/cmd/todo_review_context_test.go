package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestBuildTodoContextSeparatesFactSources(t *testing.T) {
	withTempAtmDir(t)
	repository := createContextRepository(t)
	todo := store.Todo{
		ID:          "t1",
		Title:       "Review implementation",
		Description: "Confirm all requested changes and report evidence.",
		Priority:    "P1",
		Status:      store.TodoStatusInProgress,
		Project:     "atm",
		Created:     store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "session-review", TodoID: todo.ID, Agent: "codex", Project: "atm", CWD: repository,
	}); err != nil {
		t.Fatalf("bind todo: %v", err)
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatalf("init todo doc: %v", err)
	}
	if _, err := store.AppendTodoLog(&todo, "结果：实现完成；证据：作者报告单测通过；下一步：独立审核。", ""); err != nil {
		t.Fatalf("append todo log: %v", err)
	}

	context, err := buildTodoContext(todo.ID, "")
	if err != nil {
		t.Fatalf("buildTodoContext: %v", err)
	}
	if context.WorkState.ID != todo.ID || context.WorkState.Description != todo.Description {
		t.Fatalf("work state = %#v", context.WorkState)
	}
	if context.Implementation.Source != "git" || context.Implementation.WorkspaceSource != "active_binding" {
		t.Fatalf("implementation source = %#v", context.Implementation)
	}
	rootInfo, rootErr := os.Stat(context.Implementation.Root)
	repositoryInfo, repositoryErr := os.Stat(repository)
	if !context.Implementation.Available || context.Implementation.Head == "" ||
		rootErr != nil || repositoryErr != nil || !os.SameFile(rootInfo, repositoryInfo) {
		t.Fatalf("implementation metadata = %#v", context.Implementation)
	}
	if !slices.Equal(context.Implementation.Staged, []string{"staged.txt"}) {
		t.Fatalf("staged = %#v", context.Implementation.Staged)
	}
	if !slices.Equal(context.Implementation.Unstaged, []string{"unstaged.txt"}) {
		t.Fatalf("unstaged = %#v", context.Implementation.Unstaged)
	}
	if !slices.Equal(context.Implementation.Untracked, []string{"untracked.txt"}) {
		t.Fatalf("untracked = %#v", context.Implementation.Untracked)
	}
	if context.Implementation.ChangedFiles != 3 {
		t.Fatalf("changed files = %d", context.Implementation.ChangedFiles)
	}
	if context.Verification.Status != "not_run" || !strings.Contains(context.Verification.Note, "does not run tests") {
		t.Fatalf("verification = %#v", context.Verification)
	}
	if context.Trace.BindingCount != 1 || len(context.Trace.RecentBindings) != 1 ||
		context.Trace.RecentBindings[0].SessionID != "session-review" {
		t.Fatalf("trace = %#v", context.Trace)
	}
	if len(context.TaskDocument.ReportedMilestones) != 1 ||
		!strings.Contains(context.TaskDocument.ReportedMilestones[0], "作者报告单测通过") {
		t.Fatalf("reported milestones = %#v", context.TaskDocument.ReportedMilestones)
	}
}

func TestRunTodoContextJSONIsReadOnly(t *testing.T) {
	withTempAtmDir(t)
	repository := createContextRepository(t)
	todo := store.Todo{
		ID: "t1", Title: "Read-only review context", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "session-read-only", TodoID: todo.ID, Agent: "codex", Project: "atm", CWD: repository,
	}); err != nil {
		t.Fatalf("bind todo: %v", err)
	}

	beforeTodos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	beforeBindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, _ := json.Marshal(struct {
		Todos    *store.TodoFile
		Bindings []store.TodoSessionBinding
	}{beforeTodos, beforeBindings})
	oldJSON, oldCWD := jsonOutput, todoContextCWD
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoContextCWD = oldCWD
	})
	jsonOutput = true
	todoContextCWD = ""

	var runErr error
	result := captureStdout(t, func() {
		runErr = runTodoContext(todoContextCmd, []string{todo.ID})
	})
	if runErr != nil {
		t.Fatalf("runTodoContext: %v", runErr)
	}
	var decoded todoContext
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode review context: %v\n%s", err, result)
	}
	if decoded.WorkState.ID != todo.ID || decoded.Implementation.ChangedFiles != 3 {
		t.Fatalf("decoded context = %#v", decoded)
	}
	afterTodos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	afterBindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterState, _ := json.Marshal(struct {
		Todos    *store.TodoFile
		Bindings []store.TodoSessionBinding
	}{afterTodos, afterBindings})
	if string(afterState) != string(beforeState) {
		t.Fatal("context modified SQLite work state")
	}
}

func TestReviewContextCompatibilityAliasUsesLiveContext(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Compatibility alias", Priority: "P1",
		Status: store.TodoStatusOpen, Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}
	oldJSON, oldCWD := jsonOutput, todoContextCWD
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoContextCWD = oldCWD
	})
	jsonOutput = true
	todoContextCWD = t.TempDir()

	var runErr error
	result := captureStdout(t, func() {
		runErr = todoReviewContextCmd.RunE(todoReviewContextCmd, []string{todo.ID})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var decoded todoContext
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode alias output: %v\n%s", err, result)
	}
	if decoded.WorkState.ID != todo.ID || decoded.Verification.Status != "not_run" {
		t.Fatalf("alias context = %#v", decoded)
	}
}

func TestTodoContextRequiresCWDForMultipleActiveWorktrees(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Cross-worktree review", Priority: "P1",
		Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	first := t.TempDir()
	second := t.TempDir()
	for _, binding := range []store.TodoSessionBinding{
		{SessionID: "session-one", TodoID: todo.ID, Agent: "codex", CWD: first},
		{SessionID: "session-two", TodoID: todo.ID, Agent: "claude", CWD: second},
	} {
		if _, err := store.BindTodoSession(binding); err != nil {
			t.Fatalf("bind todo: %v", err)
		}
	}

	if _, err := buildTodoContext(todo.ID, ""); err == nil ||
		!strings.Contains(err.Error(), "pass --cwd explicitly") {
		t.Fatalf("multiple worktree error = %v", err)
	}
	context, err := buildTodoContext(todo.ID, first)
	if err != nil {
		t.Fatalf("build with explicit cwd: %v", err)
	}
	if context.Implementation.WorkspaceSource != "flag" {
		t.Fatalf("workspace source = %q", context.Implementation.WorkspaceSource)
	}
}

func TestTodoContextUsesAppendOrderForSameSecondHistoricalBindings(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Historical handoff", Priority: "P1",
		Status: store.TodoStatusReview, Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	// Both bindings are created and closed back-to-back, so they share a
	// bound_at second and only insertion order can tell them apart.
	first := t.TempDir()
	second := t.TempDir()
	for _, binding := range []store.TodoSessionBinding{
		{SessionID: "session-old", TodoID: todo.ID, Agent: "codex", CWD: first},
		{SessionID: "session-new", TodoID: todo.ID, Agent: "claude", CWD: second},
	} {
		if _, err := store.BindTodoSession(binding); err != nil {
			t.Fatalf("bind %s: %v", binding.SessionID, err)
		}
		if _, err := store.UnbindTodoSession(binding.SessionID, "handoff"); err != nil {
			t.Fatalf("unbind %s: %v", binding.SessionID, err)
		}
	}

	context, err := buildTodoContext(todo.ID, "")
	if err != nil {
		t.Fatalf("buildTodoContext: %v", err)
	}
	selectedInfo, selectedErr := os.Stat(context.Implementation.CWD)
	secondInfo, secondErr := os.Stat(second)
	if selectedErr != nil || secondErr != nil || !os.SameFile(selectedInfo, secondInfo) {
		t.Fatalf("selected cwd = %q, want latest appended %q", context.Implementation.CWD, second)
	}
	if context.Implementation.WorkspaceSource != "latest_binding" ||
		context.Trace.RecentBindings[0].SessionID != "session-new" {
		t.Fatalf("historical context = %#v", context)
	}
}

func TestTodoContextReportsUnavailableGitWithoutLosingTodo(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Non-Git handoff", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}

	context, err := buildTodoContext(todo.ID, t.TempDir())
	if err != nil {
		t.Fatalf("buildTodoContext: %v", err)
	}
	if context.WorkState.ID != todo.ID {
		t.Fatalf("work state = %#v", context.WorkState)
	}
	if context.Implementation.Available || context.Implementation.Error == "" {
		t.Fatalf("implementation = %#v", context.Implementation)
	}
	if context.Implementation.Staged == nil || context.Implementation.Unstaged == nil || context.Implementation.Untracked == nil {
		t.Fatalf("change lists must be empty arrays: %#v", context.Implementation)
	}
}

func TestTodoContextReportsTaskDocumentReadFailure(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Unreadable task document", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	if err := os.MkdirAll(store.TodoDocPath(todo.ID), 0755); err != nil {
		t.Fatalf("create invalid todo doc directory: %v", err)
	}

	context, err := buildTodoContext(todo.ID, t.TempDir())
	if err != nil {
		t.Fatalf("buildTodoContext: %v", err)
	}
	if context.TaskDocument.Exists || context.TaskDocument.Error == "" {
		t.Fatalf("task document = %#v", context.TaskDocument)
	}
	if context.TaskDocument.ReportedMilestones == nil {
		t.Fatalf("reported milestones must remain an empty array: %#v", context.TaskDocument)
	}
}

func TestTodoContextDoesNotHideTaskDocumentStatFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-referential symlink behavior is platform-specific")
	}
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Invalid task document path", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	if err := os.MkdirAll(store.TodoDocDir(), 0755); err != nil {
		t.Fatalf("create todo doc dir: %v", err)
	}
	path := store.TodoDocPath(todo.ID)
	if err := os.Symlink(path, path); err != nil {
		t.Skipf("create self-referential symlink: %v", err)
	}
	if store.TodoDocExists(todo.ID) {
		t.Fatal("self-referential symlink should reproduce the stat failure")
	}

	context, err := buildTodoContext(todo.ID, t.TempDir())
	if err != nil {
		t.Fatalf("buildTodoContext: %v", err)
	}
	if context.TaskDocument.Exists || context.TaskDocument.Error == "" {
		t.Fatalf("task document = %#v", context.TaskDocument)
	}
}

func createContextRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runContextGit(t, repository, "init")
	runContextGit(t, repository, "config", "user.email", "atm-test@example.com")
	runContextGit(t, repository, "config", "user.name", "ATM Test")
	for _, name := range []string{"staged.txt", "unstaged.txt"} {
		if err := os.WriteFile(filepath.Join(repository, name), []byte("base\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runContextGit(t, repository, "add", ".")
	runContextGit(t, repository, "commit", "-m", "base")

	if err := os.WriteFile(filepath.Join(repository, "staged.txt"), []byte("staged change\n"), 0644); err != nil {
		t.Fatalf("write staged change: %v", err)
	}
	runContextGit(t, repository, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(repository, "unstaged.txt"), []byte("unstaged change\n"), 0644); err != nil {
		t.Fatalf("write unstaged change: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, "untracked.txt"), []byte("untracked\n"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	return repository
}

func runContextGit(t *testing.T, repository string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
