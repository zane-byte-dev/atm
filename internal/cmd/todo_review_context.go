package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

const (
	todoContextBindingLimit = 5
	todoContextGitTimeout   = 5 * time.Second
)

type todoContext struct {
	WorkState      store.Todo           `json:"work_state"`
	TaskDocument   reviewTaskDocument   `json:"task_document"`
	Implementation reviewImplementation `json:"implementation"`
	Verification   reviewVerification   `json:"verification"`
	Trace          reviewTrace          `json:"trace"`
}

type reviewTaskDocument struct {
	Path               string   `json:"path"`
	Exists             bool     `json:"exists"`
	ReportedMilestones []string `json:"reported_milestones"`
	Error              string   `json:"error,omitempty"`
}

type reviewImplementation struct {
	Source          string   `json:"source"`
	WorkspaceSource string   `json:"workspace_source"`
	CWD             string   `json:"cwd"`
	Available       bool     `json:"available"`
	Root            string   `json:"root,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	Head            string   `json:"head,omitempty"`
	Staged          []string `json:"staged"`
	Unstaged        []string `json:"unstaged"`
	Untracked       []string `json:"untracked"`
	ChangedFiles    int      `json:"changed_files"`
	Error           string   `json:"error,omitempty"`
}

type reviewVerification struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type reviewTrace struct {
	BindingCount   int                        `json:"binding_count"`
	RecentBindings []store.TodoSessionBinding `json:"recent_bindings"`
}

func runTodoContext(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	contextSnapshot, err := buildTodoContext(id, todoContextCWD)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(contextSnapshot)
		return nil
	}
	printTodoContext(contextSnapshot)
	return nil
}

func buildTodoContext(id, cwdOverride string) (*todoContext, error) {
	_, todo, err := loadTodoByID(id)
	if err != nil {
		return nil, err
	}

	bindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		return nil, fmt.Errorf("load todo session bindings: %w", err)
	}
	for left, right := 0, len(bindings)-1; left < right; left, right = left+1, right-1 {
		bindings[left], bindings[right] = bindings[right], bindings[left]
	}

	recentBindings := bindings
	if len(recentBindings) > todoContextBindingLimit {
		recentBindings = recentBindings[:todoContextBindingLimit]
	}
	if recentBindings == nil {
		recentBindings = []store.TodoSessionBinding{}
	}

	doc := reviewTaskDocument{
		Path:               store.TodoDocPath(todo.ID),
		ReportedMilestones: []string{},
	}
	if content, readErr := store.ReadTodoDoc(todo.ID); readErr == nil {
		doc.Exists = true
		doc.ReportedMilestones = extractRecentLogs(content, 3)
	} else if !os.IsNotExist(readErr) {
		doc.Error = reviewCommandError(readErr)
	}

	cwd, workspaceSource, err := resolveReviewContextCWD(cwdOverride, bindings)
	if err != nil {
		return nil, err
	}

	return &todoContext{
		WorkState:      *todo,
		TaskDocument:   doc,
		Implementation: collectReviewImplementation(cwd, workspaceSource),
		Verification: reviewVerification{
			Status: "not_run",
			Note:   "context does not run tests; reported milestones are trace evidence, not verified results",
		},
		Trace: reviewTrace{
			BindingCount:   len(bindings),
			RecentBindings: recentBindings,
		},
	}, nil
}

func resolveReviewContextCWD(override string, bindings []store.TodoSessionBinding) (string, string, error) {
	if value := strings.TrimSpace(override); value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", "", fmt.Errorf("resolve --cwd %s: %w", value, err)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return "", "", fmt.Errorf("inspect --cwd %s: %w", absolute, err)
		}
		if !info.IsDir() {
			return "", "", fmt.Errorf("--cwd is not a directory: %s", absolute)
		}
		return absolute, "flag", nil
	}

	activeWorktrees := make(map[string]struct{})
	for _, binding := range bindings {
		if binding.UnboundAt != nil {
			continue
		}
		if value := cleanReviewContextCWD(binding.CWD); value != "" {
			activeWorktrees[value] = struct{}{}
		}
	}
	if len(activeWorktrees) == 1 {
		for value := range activeWorktrees {
			return value, "active_binding", nil
		}
	}
	if len(activeWorktrees) > 1 {
		values := make([]string, 0, len(activeWorktrees))
		for value := range activeWorktrees {
			values = append(values, value)
		}
		sort.Strings(values)
		return "", "", fmt.Errorf("todo has active bindings in multiple worktrees; pass --cwd explicitly: %s", strings.Join(values, ", "))
	}

	for _, binding := range bindings {
		if value := cleanReviewContextCWD(binding.CWD); value != "" {
			return value, "latest_binding", nil
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve current directory: %w", err)
	}
	return cwd, "current_directory", nil
}

func cleanReviewContextCWD(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if absolute, err := filepath.Abs(value); err == nil {
		if evaluated, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil {
			return evaluated
		}
		return absolute
	}
	return value
}

func collectReviewImplementation(cwd, workspaceSource string) reviewImplementation {
	result := reviewImplementation{
		Source:          "git",
		WorkspaceSource: workspaceSource,
		CWD:             cwd,
		Staged:          []string{},
		Unstaged:        []string{},
		Untracked:       []string{},
	}

	root, err := reviewGitText(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		result.Error = reviewCommandError(err)
		return result
	}
	result.Available = true
	result.Root = root

	if branch, branchErr := reviewGitText(cwd, "branch", "--show-current"); branchErr == nil {
		result.Branch = branch
	}
	if head, headErr := reviewGitText(cwd, "rev-parse", "--verify", "HEAD"); headErr == nil {
		result.Head = head
	}

	var errors []string
	if result.Staged, err = reviewGitPaths(cwd, "diff", "--cached", "--name-only", "-z", "--"); err != nil {
		errors = append(errors, "staged: "+reviewCommandError(err))
		result.Staged = []string{}
	}
	if result.Unstaged, err = reviewGitPaths(cwd, "diff", "--name-only", "-z", "--"); err != nil {
		errors = append(errors, "unstaged: "+reviewCommandError(err))
		result.Unstaged = []string{}
	}
	if result.Untracked, err = reviewGitPaths(cwd, "ls-files", "--others", "--exclude-standard", "-z"); err != nil {
		errors = append(errors, "untracked: "+reviewCommandError(err))
		result.Untracked = []string{}
	}
	result.ChangedFiles = reviewChangedFileCount(result.Staged, result.Unstaged, result.Untracked)
	result.Error = strings.Join(errors, "; ")
	return result
}

func reviewGitText(cwd string, args ...string) (string, error) {
	data, err := reviewGitOutput(cwd, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func reviewGitPaths(cwd string, args ...string) ([]string, error) {
	data, err := reviewGitOutput(cwd, args...)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(data), "\x00")
	paths := make([]string, 0, len(parts))
	for _, path := range parts {
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func reviewGitOutput(cwd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), todoContextGitTimeout)
	defer cancel()
	commandArgs := append([]string{"-C", cwd}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	data, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git command timed out after %s", todoContextGitTimeout)
	}
	if err != nil {
		detail := strings.TrimSpace(string(data))
		if detail == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, detail)
	}
	return data, nil
}

func reviewChangedFileCount(groups ...[]string) int {
	unique := make(map[string]struct{})
	for _, group := range groups {
		for _, path := range group {
			unique[path] = struct{}{}
		}
	}
	return len(unique)
}

func reviewCommandError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(value)
	if len(runes) > 240 {
		return string(runes[:237]) + "..."
	}
	return value
}

func printTodoContext(contextSnapshot *todoContext) {
	todo := contextSnapshot.WorkState
	fmt.Printf("Todo:         %s [%s] %s\n", todo.ID, todo.Status, todo.Title)
	if todo.Project != "" {
		fmt.Printf("Project:      %s\n", todo.Project)
	}
	if todo.Description != "" {
		fmt.Printf("Goal:         %s\n", strings.ReplaceAll(todo.Description, "\n", "\n              "))
	}

	trace := contextSnapshot.Trace
	fmt.Printf("Trace:        %d binding(s)", trace.BindingCount)
	if len(trace.RecentBindings) > 0 {
		latest := trace.RecentBindings[0]
		fmt.Printf("; latest %s session %s", emptyAs(latest.Agent, "unknown"), shortSessionID(latest.SessionID))
	}
	fmt.Println()
	if contextSnapshot.TaskDocument.Error != "" {
		fmt.Printf("Task doc:     unavailable: %s\n", contextSnapshot.TaskDocument.Error)
	}

	implementation := contextSnapshot.Implementation
	fmt.Printf("Git:          %s (%s)\n", implementation.CWD, implementation.WorkspaceSource)
	if !implementation.Available {
		fmt.Printf("Git status:   unavailable: %s\n", implementation.Error)
	} else {
		revision := implementation.Branch
		if revision == "" {
			revision = "(detached)"
		}
		if implementation.Head != "" {
			revision += " @ " + shortReviewRevision(implementation.Head)
		}
		fmt.Printf("Revision:     %s\n", revision)
		printReviewFileGroup("Staged", implementation.Staged)
		printReviewFileGroup("Unstaged", implementation.Unstaged)
		printReviewFileGroup("Untracked", implementation.Untracked)
		if implementation.Error != "" {
			fmt.Printf("Git warning:  %s\n", implementation.Error)
		}
	}
	fmt.Printf("Verification: %s — %s\n", contextSnapshot.Verification.Status, contextSnapshot.Verification.Note)
}

func printReviewFileGroup(label string, paths []string) {
	fmt.Printf("%-13s %d\n", label+":", len(paths))
	for _, path := range paths {
		fmt.Printf("  - %s\n", path)
	}
}

func shortReviewRevision(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
