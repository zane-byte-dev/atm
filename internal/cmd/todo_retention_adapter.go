package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func runTodoDelete(cmd *cobra.Command, args []string) error {
	selector := workapp.DeleteSelector{Project: todoDeleteProjectFlag}
	if selector.Project == "" {
		if len(args) == 0 {
			return fmt.Errorf("provide a todo ID or use --project to batch delete")
		}
		selector.TodoID = args[0]
	}
	call := todoWorkflowCLICall("delete")
	plan, err := workapp.Default.PlanDelete(cmd.Context(), call, selector)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf("Permanently delete todo %s?", plan.TodoIDs[0])
	if selector.Project != "" {
		prompt = fmt.Sprintf("Permanently delete %d todos from project %s?", len(plan.TodoIDs), plan.Selector.Project)
	}
	confirmed, err := confirmDestructive(cmd, todoDeleteYesFlag, prompt)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
		return nil
	}
	result, err := workapp.Default.Delete(cmd.Context(), call, workapp.DeleteInput{Plan: plan, Confirmed: true})
	if err != nil {
		return err
	}
	if selector.Project != "" {
		fmt.Printf("Deleted %d todos from project %s\n", len(result.Deleted), plan.Selector.Project)
		return nil
	}
	fmt.Printf("Deleted %s\n", result.Deleted[0])
	return nil
}

func runTodoArchive(cmd *cobra.Command, args []string) error {
	return runTodoRetention(cmd, args, workapp.RetentionArchive, "archived", "Archived")
}

func runTodoUnarchive(cmd *cobra.Command, args []string) error {
	return runTodoRetention(cmd, args, workapp.RetentionUnarchive, "unarchived", "Unarchived")
}

func runTodoTrash(cmd *cobra.Command, args []string) error {
	return runTodoRetention(cmd, args, workapp.RetentionTrash, "trashed", "Trashed")
}

func runTodoRestore(cmd *cobra.Command, args []string) error {
	return runTodoRetention(cmd, args, workapp.RetentionRestore, "restored", "Restored")
}

func runTodoRetention(
	cmd *cobra.Command,
	args []string,
	action workapp.RetentionAction,
	jsonKey string,
	verb string,
) error {
	call := todoWorkflowCLICall(string(action))
	input := workapp.RetentionInput{TodoIDs: args}
	var (
		result workapp.RetentionResult
		err    error
	)
	switch action {
	case workapp.RetentionArchive:
		result, err = workapp.Default.Archive(cmd.Context(), call, input)
	case workapp.RetentionUnarchive:
		result, err = workapp.Default.Unarchive(cmd.Context(), call, input)
	case workapp.RetentionTrash:
		result, err = workapp.Default.Trash(cmd.Context(), call, input)
	case workapp.RetentionRestore:
		result, err = workapp.Default.Restore(cmd.Context(), call, input)
	default:
		return fmt.Errorf("unknown retention action %q", action)
	}
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{jsonKey: result.Moved})
		return nil
	}
	fmt.Printf("%s %s\n", verb, strings.Join(result.Moved, ", "))
	return nil
}
