package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoBulkProjectFlag string
	todoBulkStatusFlag  string
	todoBulkLaneFlag    string
	todoBulkReasonFlag  string
)

var todoBulkCmd = &cobra.Command{
	Use:   "bulk <done|drop|move|edit> <todo-id>...",
	Short: "Apply one lifecycle or metadata change to multiple todos",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runTodoBulk,
}

func init() {
	todoBulkCmd.Flags().StringVar(&todoBulkProjectFlag, "project", "", "target project for move/edit")
	todoBulkCmd.Flags().StringVar(&todoBulkStatusFlag, "status", "", "target status for edit")
	todoBulkCmd.Flags().StringVar(&todoBulkLaneFlag, "lane", "", "target lane for edit")
	todoBulkCmd.Flags().StringVar(&todoBulkReasonFlag, "reason", "", "reason recorded for done/drop")
	todoCmd.AddCommand(todoBulkCmd)
}

func runTodoBulk(cmd *cobra.Command, args []string) error {
	action := strings.ToLower(args[0])
	ids := uniqueStrings(args[1:])
	if action != "done" && action != "drop" && action != "move" && action != "edit" {
		return fmt.Errorf("unsupported bulk action %q (use done, drop, move, or edit)", action)
	}
	if action == "move" && strings.TrimSpace(todoBulkProjectFlag) == "" {
		return fmt.Errorf("bulk move requires --project")
	}
	if action == "edit" && strings.TrimSpace(todoBulkProjectFlag) == "" && strings.TrimSpace(todoBulkStatusFlag) == "" && !cmd.Flags().Changed("lane") {
		return fmt.Errorf("bulk edit requires --project, --status, or --lane")
	}
	if todoBulkStatusFlag != "" {
		if err := validateWorkStatus(todoBulkStatusFlag); err != nil {
			return err
		}
	}
	lane := todoBulkLaneFlag
	if cmd.Flags().Changed("lane") {
		var err error
		lane, err = normalizeLane(todoBulkLaneFlag)
		if err != nil {
			return err
		}
	}
	var tf *store.TodoFile
	var selected []*store.Todo
	var awakened []store.TodoWakeEvent
	changedStatus := map[string]bool{}
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		if todoBulkReasonFlag != "" && (action == "done" || action == "drop") {
			status := action
			if action == "drop" {
				status = "dropped"
			}
			message := fmt.Sprintf("[%s] %s", status, todoBulkReasonFlag)
			if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
				return err
			}
			if err := validateTodoLogReferences(tf, message); err != nil {
				return err
			}
		}
		selected = make([]*store.Todo, 0, len(ids))
		for _, id := range ids {
			todo := store.FindTodo(tf, id)
			if todo == nil {
				return store.TodoNotFoundError(tf, id)
			}
			selected = append(selected, todo)
		}
		targetWaiting := todoBulkStatusFlag == store.TodoStatusWaiting
		if action == "edit" && targetWaiting {
			for _, todo := range selected {
				if todo.WakeCondition == "" && todo.ReviewAt == "" && len(todo.DependsOn) == 0 {
					return fmt.Errorf("todo %s cannot enter waiting without a wake condition, review date, or dependency", todo.ID)
				}
			}
		}
		now := time.Now().In(config.Loc).Unix()
		today := store.Today()
		for _, todo := range selected {
			switch action {
			case "done", "drop":
				status := "done"
				if action == "drop" {
					status = "dropped"
				}
				if todo.Status == status {
					continue
				}
				changedStatus[todo.ID] = true
				todo.Status = status
				todo.Closed = &today
				todo.DoneTS = &now
				if todoBulkReasonFlag != "" {
					reason := todoBulkReasonFlag
					todo.ClosedReason = &reason
				}
			case "move":
				todo.Project = strings.TrimSpace(todoBulkProjectFlag)
			case "edit":
				if strings.TrimSpace(todoBulkProjectFlag) != "" {
					todo.Project = strings.TrimSpace(todoBulkProjectFlag)
				}
				if todoBulkStatusFlag != "" {
					todo.Status = todoBulkStatusFlag
				}
				if todo.Status != store.TodoStatusWaiting {
					todo.WakeCondition = ""
					todo.ReviewAt = ""
				}
				if cmd.Flags().Changed("lane") {
					todo.Lane = lane
				}
			}
		}
		if action == "done" {
			awakened = store.ReconcileTodoDependencies(tf)
		}
		for _, todo := range selected {
			if todo.Status == store.TodoStatusInProgress {
				continue
			}
			switch action {
			case "done", "drop":
				if _, err := transaction.UnbindTodoSessions(todo.ID, "bulk:"+todo.Status); err != nil {
					return fmt.Errorf("unbind bulk-closed todo %s before status change: %w", todo.ID, err)
				}
			case "edit":
				if todoBulkStatusFlag != "" {
					if _, err := transaction.UnbindTodoSessions(todo.ID, "bulk-status:"+todo.Status); err != nil {
						return fmt.Errorf("unbind bulk-edited todo %s before status change: %w", todo.ID, err)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, todo := range selected {
		if changedStatus[todo.ID] && todoBulkReasonFlag != "" && (action == "done" || action == "drop") {
			_, _ = store.AppendTodoLog(todo, fmt.Sprintf("[%s] %s", todo.Status, todoBulkReasonFlag), "")
		}
	}
	appendTodoWakeLogs(tf, awakened)
	syncIDs := wakeEventTodoIDs(awakened)
	for _, todo := range selected {
		syncIDs = append(syncIDs, todo.ID)
	}
	if err := syncExistingTodoDocs(tf, syncIDs...); err != nil {
		return err
	}
	values := make([]store.Todo, 0, len(selected))
	for _, todo := range selected {
		values = append(values, *todo)
	}
	if jsonOutput {
		output.JSON(map[string]any{"action": action, "todos": values, "awakened": awakened})
		return nil
	}
	fmt.Printf("Bulk %s updated %d todos\n", action, len(values))
	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
