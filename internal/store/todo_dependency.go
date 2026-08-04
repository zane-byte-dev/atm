package store

import (
	"fmt"
	"sort"
	"strings"
)

type TodoDependencyIssue struct {
	TodoID     string `json:"todo_id"`
	DependsOn  string `json:"depends_on,omitempty"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

type TodoWakeEvent struct {
	TodoID       string   `json:"todo_id"`
	Dependencies []string `json:"dependencies"`
	Reason       string   `json:"reason"`
}

func AddTodoDependency(tf *TodoFile, todoID, dependencyID string) error {
	todo := FindTodo(tf, todoID)
	if todo == nil {
		return TodoNotFoundError(tf, todoID)
	}
	// An archived dependency is allowed: it names finished work, which is the
	// most a dependency can ever be. UnmetTodoDependencies treats it as met.
	if _, archived := ArchivedStatus(tf, dependencyID); !archived && FindTodo(tf, dependencyID) == nil {
		return fmt.Errorf("dependency todo not found: %s", dependencyID)
	}
	if todoID == dependencyID {
		return fmt.Errorf("todo %s cannot depend on itself", todoID)
	}
	for _, existing := range todo.DependsOn {
		if existing == dependencyID {
			return nil
		}
	}
	todo.DependsOn = append(todo.DependsOn, dependencyID)
	sort.Strings(todo.DependsOn)
	if dependencyPathExists(tf, dependencyID, todoID, map[string]bool{}) {
		todo.DependsOn = removeDependency(todo.DependsOn, dependencyID)
		return fmt.Errorf("dependency %s -> %s would create a cycle", todoID, dependencyID)
	}
	return nil
}

func RemoveTodoDependency(tf *TodoFile, todoID, dependencyID string) (bool, error) {
	todo := FindTodo(tf, todoID)
	if todo == nil {
		return false, TodoNotFoundError(tf, todoID)
	}
	before := len(todo.DependsOn)
	todo.DependsOn = removeDependency(todo.DependsOn, dependencyID)
	return len(todo.DependsOn) != before, nil
}

func removeDependency(values []string, target string) []string {
	result := values[:0]
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func dependencyPathExists(tf *TodoFile, from, target string, visited map[string]bool) bool {
	if from == target {
		return true
	}
	if visited[from] {
		return false
	}
	visited[from] = true
	todo := FindTodo(tf, from)
	if todo == nil {
		return false
	}
	for _, next := range todo.DependsOn {
		if dependencyPathExists(tf, next, target, visited) {
			return true
		}
	}
	return false
}

// UnmetTodoDependencies lists the dependencies that are not done yet. Archiving
// a dependency must not strand its dependents: an archived todo is closed, so it
// counts by the status it closed at, exactly as if it were still in the working
// set.
func UnmetTodoDependencies(tf *TodoFile, todo Todo) []string {
	var unmet []string
	for _, id := range todo.DependsOn {
		if dependency := FindTodo(tf, id); dependency != nil {
			if dependency.Status != TodoStatusDone {
				unmet = append(unmet, id)
			}
			continue
		}
		if status, archived := ArchivedStatus(tf, id); !archived || status != TodoStatusDone {
			unmet = append(unmet, id)
		}
	}
	sort.Strings(unmet)
	return unmet
}

func TodoDependencyWakeCondition(todo Todo) string {
	if len(todo.DependsOn) == 0 {
		return ""
	}
	dependencies := append([]string(nil), todo.DependsOn...)
	sort.Strings(dependencies)
	return "waiting for todos: " + strings.Join(dependencies, ", ")
}

// ReconcileTodoDependencies makes waiting todos open once every structured
// dependency is done. Dependency completion means work is ready to start; it
// is not evidence that the dependent work has been submitted for review.
// Free-form wake text is never interpreted.
func ReconcileTodoDependencies(tf *TodoFile) []TodoWakeEvent {
	var events []TodoWakeEvent
	for i := range tf.Items {
		todo := &tf.Items[i]
		if todo.Status != TodoStatusWaiting || len(todo.DependsOn) == 0 {
			continue
		}
		if len(UnmetTodoDependencies(tf, *todo)) != 0 {
			continue
		}
		dependencies := append([]string(nil), todo.DependsOn...)
		todo.Status = TodoStatusOpen
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		events = append(events, TodoWakeEvent{
			TodoID:       todo.ID,
			Dependencies: dependencies,
			Reason:       "all dependencies completed: " + strings.Join(dependencies, ", "),
		})
	}
	return events
}

func AuditTodoDependencies(tf *TodoFile) []TodoDependencyIssue {
	var issues []TodoDependencyIssue
	for _, todo := range tf.Items {
		for _, id := range todo.DependsOn {
			dependency := FindTodo(tf, id)
			if dependency == nil {
				// Not in the working set: either archived (its closing status
				// still counts) or genuinely gone.
				if status, archived := ArchivedStatus(tf, id); archived {
					dependency = &Todo{ID: id, Status: status}
				}
			}
			switch {
			case dependency == nil:
				issues = append(issues, TodoDependencyIssue{TodoID: todo.ID, DependsOn: id, Code: "dependency_missing", Detail: "referenced todo does not exist", Suggestion: "remove the dependency or restore the referenced todo"})
			case dependency.Status == TodoStatusDropped && TodoIsActive(todo):
				issues = append(issues, TodoDependencyIssue{TodoID: todo.ID, DependsOn: id, Code: "dependency_dropped", Detail: "active todo depends on a dropped todo", Suggestion: "replace the dependency or explicitly drop/unblock the dependent todo"})
			}
		}
		for _, id := range todo.DependsOn {
			if dependencyPathExists(tf, id, todo.ID, map[string]bool{}) {
				issues = append(issues, TodoDependencyIssue{TodoID: todo.ID, DependsOn: id, Code: "dependency_cycle", Detail: "todo dependency graph contains a cycle", Suggestion: "remove one edge in the cycle"})
				break
			}
		}
	}
	return issues
}
