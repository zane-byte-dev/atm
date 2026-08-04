package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoWakeStatusFlag string
	todoWakeReasonFlag string
)

var todoDependCmd = &cobra.Command{
	Use:   "depend",
	Short: "Manage structured dependencies between todos",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var todoDependAddCmd = &cobra.Command{
	Use:   "add <todo-id> <dependency-id>",
	Short: "Make a todo wait for another todo to complete",
	Example: `  # t77 waits until t76 is done
  atm todo depend add t77 t76`,
	Args: cobra.ExactArgs(2),
	RunE: runTodoDependAdd,
}

var todoDependRemoveCmd = &cobra.Command{
	Use:   "remove <todo-id> <dependency-id>",
	Short: "Remove a todo dependency",
	Args:  cobra.ExactArgs(2),
	RunE:  runTodoDependRemove,
}

var todoDependListCmd = &cobra.Command{
	Use:   "list <todo-id>",
	Short: "List dependency status for a todo",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoDependList,
}

var todoWakeCmd = &cobra.Command{
	Use:   "wake <todo-id>",
	Short: "Wake a waiting todo from an external event or manual decision",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoWake,
}

var todoReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Wake satisfied dependencies and audit the dependency graph",
	Args:  cobra.NoArgs,
	RunE:  runTodoReconcile,
}

func init() {
	todoDependCmd.AddCommand(todoDependAddCmd, todoDependRemoveCmd, todoDependListCmd)
	todoWakeCmd.Flags().StringVar(&todoWakeStatusFlag, "status", store.TodoStatusOpen, "status after waking: open or review (use todo submit for normal review submission)")
	todoWakeCmd.Flags().StringVar(&todoWakeReasonFlag, "reason", "external wake event", "reason recorded in todo progress")
	todoCmd.AddCommand(todoDependCmd, todoWakeCmd, todoReconcileCmd)
}

func runTodoDependAdd(cmd *cobra.Command, args []string) error {
	var events []store.TodoWakeEvent
	tf, todo, err := mutateTodo(args[0], func(todo *store.Todo, tf *store.TodoFile, transaction *workapp.Transaction) error {
		if err := store.AddTodoDependency(tf, args[0], args[1]); err != nil {
			return err
		}
		if store.TodoIsActive(*todo) && len(store.UnmetTodoDependencies(tf, *todo)) > 0 {
			todo.Status = store.TodoStatusWaiting
			todo.WakeCondition = store.TodoDependencyWakeCondition(*todo)
		}
		events = store.ReconcileTodoDependencies(tf)
		if todo.Status != store.TodoStatusInProgress {
			if _, err := transaction.UnbindTodoSessions(todo.ID, "dependency:"+todo.Status); err != nil {
				return fmt.Errorf("unbind todo sessions before dependency wait: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	appendTodoWakeLogs(tf, events)
	if err := syncExistingTodoDocs(tf, append([]string{todo.ID}, wakeEventTodoIDs(events)...)...); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"todo": todo, "awakened": events})
		return nil
	}
	fmt.Printf("Added dependency: %s waits for %s\n", args[0], args[1])
	for _, event := range events {
		fmt.Printf("Awakened %s: %s\n", event.TodoID, event.Reason)
	}
	return nil
}

func runTodoDependRemove(cmd *cobra.Command, args []string) error {
	var removed bool
	var events []store.TodoWakeEvent
	tf, todo, err := mutateTodo(args[0], func(todo *store.Todo, tf *store.TodoFile, _ *workapp.Transaction) error {
		var err error
		removed, err = store.RemoveTodoDependency(tf, args[0], args[1])
		if err != nil {
			return err
		}
		if removed && todo.Status == store.TodoStatusWaiting && strings.HasPrefix(todo.WakeCondition, "waiting for todos: ") {
			if len(todo.DependsOn) == 0 {
				todo.Status = store.TodoStatusOpen
				todo.WakeCondition = ""
				todo.ReviewAt = ""
				events = append(events, store.TodoWakeEvent{TodoID: todo.ID, Dependencies: []string{}, Reason: "all structured dependencies removed"})
			} else {
				todo.WakeCondition = store.TodoDependencyWakeCondition(*todo)
			}
		}
		events = append(events, store.ReconcileTodoDependencies(tf)...)
		return nil
	})
	if err != nil {
		return err
	}
	appendTodoWakeLogs(tf, events)
	syncIDs := wakeEventTodoIDs(events)
	if todo != nil {
		syncIDs = append(syncIDs, todo.ID)
	}
	if err := syncExistingTodoDocs(tf, syncIDs...); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"removed": removed, "todo": todo, "awakened": events})
		return nil
	}
	if !removed {
		fmt.Printf("Dependency was not present: %s -> %s\n", args[0], args[1])
		return nil
	}
	fmt.Printf("Removed dependency: %s -> %s\n", args[0], args[1])
	return nil
}

func runTodoDependList(cmd *cobra.Command, args []string) error {
	tf, todo, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	type dependencyView struct {
		ID     string `json:"id"`
		Title  string `json:"title,omitempty"`
		Status string `json:"status"`
		Met    bool   `json:"met"`
	}
	dependencies := make([]dependencyView, 0, len(todo.DependsOn))
	for _, id := range todo.DependsOn {
		view := dependencyView{ID: id, Status: "missing"}
		if dependency := store.FindTodo(tf, id); dependency != nil {
			view.Title = dependency.Title
			view.Status = dependency.Status
			view.Met = dependency.Status == "done"
		}
		dependencies = append(dependencies, view)
	}
	if jsonOutput {
		output.JSON(dependencies)
		return nil
	}
	if len(dependencies) == 0 {
		fmt.Println("No dependencies.")
		return nil
	}
	for _, dependency := range dependencies {
		fmt.Printf("%-6s %-12s %s\n", dependency.ID, dependency.Status, dependency.Title)
	}
	return nil
}

func runTodoWake(cmd *cobra.Command, args []string) error {
	targetStatus := todoWakeStatusFlag
	if targetStatus != store.TodoStatusReview && targetStatus != store.TodoStatusOpen {
		return fmt.Errorf("--status must be review or open")
	}
	message := "[wake] " + todoWakeReasonFlag
	tf, todo, err := mutateTodo(args[0], func(todo *store.Todo, tf *store.TodoFile, _ *workapp.Transaction) error {
		if todo.Status != store.TodoStatusWaiting {
			return fmt.Errorf("cannot wake todo %s with status %s", todo.ID, todo.Status)
		}
		if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
			return err
		}
		if err := validateTodoLogReferences(tf, message); err != nil {
			return err
		}
		todo.Status = targetStatus
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := store.AppendTodoLog(todo, message, ""); err != nil {
		return err
	}
	return finishTodoMutation(tf, todo,
		fmt.Sprintf("Awakened %s to %s: %s", todo.ID, todo.Status, todo.Title))
}

func runTodoReconcile(cmd *cobra.Command, args []string) error {
	var tf *store.TodoFile
	var events []store.TodoWakeEvent
	var issues []store.TodoDependencyIssue
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		events = store.ReconcileTodoDependencies(tf)
		issues = store.AuditTodoDependencies(tf)
		return nil
	})
	if err != nil {
		return err
	}
	if len(events) > 0 {
		appendTodoWakeLogs(tf, events)
		if err := syncExistingTodoDocs(tf, wakeEventTodoIDs(events)...); err != nil {
			return err
		}
	}
	if jsonOutput {
		output.JSON(map[string]any{"awakened": events, "issues": issues})
		return nil
	}
	fmt.Printf("Reconciled dependencies: awakened=%d issues=%d\n", len(events), len(issues))
	for _, event := range events {
		fmt.Printf("  wake  %-6s %s\n", event.TodoID, event.Reason)
	}
	for _, issue := range issues {
		fmt.Printf("  issue %-6s %-20s %s\n", issue.TodoID, issue.Code, issue.Detail)
	}
	return nil
}

func appendTodoWakeLogs(tf *store.TodoFile, events []store.TodoWakeEvent) {
	for _, event := range events {
		if todo := store.FindTodo(tf, event.TodoID); todo != nil {
			_, _ = store.AppendTodoLog(todo, "[wake] "+event.Reason, "")
		}
	}
}

func wakeEventTodoIDs(events []store.TodoWakeEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.TodoID)
	}
	return ids
}
