package work

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

const (
	contextBindingLimit = 5
	contextGitTimeout   = 5 * time.Second
)

type ContextInput struct {
	TodoID string `json:"todo_id,omitempty"`
	CWD    string `json:"cwd,omitempty"`
}

type ContextTaskDocument struct {
	Path               string   `json:"path"`
	Exists             bool     `json:"exists"`
	ReportedMilestones []string `json:"reported_milestones"`
	Error              string   `json:"error,omitempty"`
}

type ContextImplementation struct {
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

type VerificationStatus string

const VerificationNotRun VerificationStatus = "not_run"

type ContextVerification struct {
	Status VerificationStatus `json:"status"`
}

type ContextTrace struct {
	BindingCount   int                  `json:"binding_count"`
	RecentBindings []TodoSessionBinding `json:"recent_bindings"`
}

type ContextResult struct {
	WorkState      Todo                  `json:"work_state"`
	TaskDocument   ContextTaskDocument   `json:"task_document"`
	Implementation ContextImplementation `json:"implementation"`
	Verification   ContextVerification   `json:"verification"`
	Trace          ContextTrace          `json:"trace"`
	LatestPlan     *PlanSnapshot         `json:"latest_plan,omitempty"`
}

// Context assembles a live, read-only work snapshot. Git inspection is an
// infrastructure read owned by this use case; transports only choose an
// optional cwd and render the resulting facts.
func (service Service) Context(ctx context.Context, call application.Call, input ContextInput) (ContextResult, error) {
	ctx, err := validateReadCall(ctx, call)
	if err != nil {
		return ContextResult{}, err
	}
	_, todo, err := loadTodoForRead(ctx, call, input.TodoID)
	if err != nil {
		return ContextResult{}, err
	}
	bindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		return ContextResult{}, readApplicationError("load todo session bindings", err)
	}
	for left, right := 0, len(bindings)-1; left < right; left, right = left+1, right-1 {
		bindings[left], bindings[right] = bindings[right], bindings[left]
	}
	recent := bindings
	if len(recent) > contextBindingLimit {
		recent = recent[:contextBindingLimit]
	}
	if recent == nil {
		recent = []TodoSessionBinding{}
	}

	document := inspectContextDocument(todo.ID)
	cwd, workspaceSource, err := resolveContextCWD(input.CWD, bindings)
	if err != nil {
		return ContextResult{}, err
	}
	latestPlan, err := latestPlanSnapshot(todo.ID)
	if err != nil {
		return ContextResult{}, readApplicationError("load latest todo plan", err)
	}
	return ContextResult{
		WorkState:      *todo,
		TaskDocument:   document,
		Implementation: collectContextImplementation(ctx, cwd, workspaceSource),
		Verification:   ContextVerification{Status: VerificationNotRun},
		Trace: ContextTrace{
			BindingCount:   len(bindings),
			RecentBindings: recent,
		},
		LatestPlan: latestPlan,
	}, nil
}

func inspectContextDocument(todoID string) ContextTaskDocument {
	document := ContextTaskDocument{
		Path:               store.TodoDocPath(todoID),
		ReportedMilestones: []string{},
	}
	content, err := store.ReadTodoDoc(todoID)
	if err == nil {
		document.Exists = true
		document.ReportedMilestones = extractTodoProgress(content, 3)
		return document
	}
	if !os.IsNotExist(err) {
		document.Error = compactContextError(err)
	}
	return document
}

func resolveContextCWD(override string, bindings []TodoSessionBinding) (string, string, error) {
	if value := strings.TrimSpace(override); value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", "", readInvalidArgument("cannot resolve context cwd", "cwd", value)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			appErr := readInvalidArgument("cannot inspect context cwd", "cwd", absolute)
			appErr.Cause = err
			return "", "", appErr
		}
		if !info.IsDir() {
			return "", "", readInvalidArgument("context cwd is not a directory", "cwd", absolute)
		}
		return absolute, "flag", nil
	}

	activeWorktrees := make(map[string]struct{})
	for _, binding := range bindings {
		if binding.UnboundAt != nil {
			continue
		}
		if value := NormalizeContextCWD(binding.CWD); value != "" {
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
		err := application.NewError(application.CodeConflict,
			"todo has active bindings in multiple worktrees; pass --cwd explicitly: "+strings.Join(values, ", "))
		err.Details = map[string]any{"worktrees": values}
		return "", "", err
	}

	for _, binding := range bindings {
		if value := NormalizeContextCWD(binding.CWD); value != "" {
			return value, "latest_binding", nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", readApplicationError("resolve current directory", err)
	}
	return cwd, "current_directory", nil
}

// NormalizeContextCWD canonicalizes a binding worktree for comparisons. Task
// dispatch reuses the same rule so two read paths cannot disagree about whether
// bindings point at one workspace or several.
func NormalizeContextCWD(value string) string {
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

func collectContextImplementation(ctx context.Context, cwd, workspaceSource string) ContextImplementation {
	result := ContextImplementation{
		Source:          "git",
		WorkspaceSource: workspaceSource,
		CWD:             cwd,
		Staged:          []string{},
		Unstaged:        []string{},
		Untracked:       []string{},
	}
	root, err := contextGitText(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		result.Error = compactContextError(err)
		return result
	}
	result.Available = true
	result.Root = root
	if branch, branchErr := contextGitText(ctx, cwd, "branch", "--show-current"); branchErr == nil {
		result.Branch = branch
	}
	if head, headErr := contextGitText(ctx, cwd, "rev-parse", "--verify", "HEAD"); headErr == nil {
		result.Head = head
	}

	errors := []string{}
	if result.Staged, err = contextGitPaths(ctx, cwd, "diff", "--cached", "--name-only", "-z", "--"); err != nil {
		errors = append(errors, "staged: "+compactContextError(err))
		result.Staged = []string{}
	}
	if result.Unstaged, err = contextGitPaths(ctx, cwd, "diff", "--name-only", "-z", "--"); err != nil {
		errors = append(errors, "unstaged: "+compactContextError(err))
		result.Unstaged = []string{}
	}
	if result.Untracked, err = contextGitPaths(ctx, cwd, "ls-files", "--others", "--exclude-standard", "-z"); err != nil {
		errors = append(errors, "untracked: "+compactContextError(err))
		result.Untracked = []string{}
	}
	result.ChangedFiles = changedContextFileCount(result.Staged, result.Unstaged, result.Untracked)
	result.Error = strings.Join(errors, "; ")
	return result
}

func contextGitText(ctx context.Context, cwd string, args ...string) (string, error) {
	data, err := contextGitOutput(ctx, cwd, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func contextGitPaths(ctx context.Context, cwd string, args ...string) ([]string, error) {
	data, err := contextGitOutput(ctx, cwd, args...)
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

func contextGitOutput(parent context.Context, cwd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, contextGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	data, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git command timed out after %s", contextGitTimeout)
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

func changedContextFileCount(groups ...[]string) int {
	unique := make(map[string]struct{})
	for _, group := range groups {
		for _, path := range group {
			unique[path] = struct{}{}
		}
	}
	return len(unique)
}

func compactContextError(err error) string {
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
