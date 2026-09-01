package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"
)

// resolveTodoWorkspace picks the directory Codex should open for a Todo.
// An explicit --cwd wins; otherwise an unambiguous active binding, then the
// latest binding that still exists, then a project-named directory, then the
// current working directory.
func resolveTodoWorkspace(todo *store.Todo, override string) (string, string, error) {
	if value := strings.TrimSpace(override); value != "" {
		return validateTodoWorkspace(value, "flag")
	}
	bindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		return "", "", err
	}
	active := map[string]struct{}{}
	for _, binding := range bindings {
		if binding.UnboundAt == nil {
			if value := cleanReviewContextCWD(binding.CWD); value != "" {
				active[value] = struct{}{}
			}
		}
	}
	if len(active) > 1 {
		values := make([]string, 0, len(active))
		for value := range active {
			values = append(values, value)
		}
		sort.Strings(values)
		return "", "", fmt.Errorf("todo has active bindings in multiple worktrees; pass --cwd explicitly: %s", strings.Join(values, ", "))
	}
	for value := range active {
		return validateTodoWorkspace(value, "active_binding")
	}
	for index := len(bindings) - 1; index >= 0; index-- {
		if value := strings.TrimSpace(bindings[index].CWD); value != "" {
			if cwd, source, err := validateTodoWorkspace(value, "latest_binding"); err == nil {
				return cwd, source, nil
			}
		}
	}
	project := strings.TrimSpace(todo.Project)
	if project != "" {
		home, homeErr := os.UserHomeDir()
		candidates := []string{}
		if filepath.IsAbs(project) {
			candidates = append(candidates, project)
		}
		if homeErr == nil {
			candidates = append(candidates,
				filepath.Join(home, "mox", project),
				filepath.Join(home, "work", project),
				filepath.Join(home, project),
			)
		}
		for _, candidate := range candidates {
			if cwd, source, err := validateTodoWorkspace(candidate, "project"); err == nil {
				return cwd, source, nil
			}
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return validateTodoWorkspace(cwd, "current_directory")
}

func validateTodoWorkspace(value, source string) (string, string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", "", fmt.Errorf("resolve todo workspace %s: %w", value, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", "", fmt.Errorf("inspect todo workspace %s: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("todo workspace is not a directory: %s", absolute)
	}
	return absolute, source, nil
}
