package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func runTodoStart(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("start")
	result, err := workapp.Default.Start(cmd.Context(), call, workapp.StartInput{TodoID: args[0]})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	fmt.Printf("Started %s: %s\n", result.Todo.ID, result.Todo.Title)
	return nil
}

// Accepting work as done is the only closing transition left on this path:
// abandoning became archival, which the retention use case owns. This used to
// branch on a `done` flag to serve `todo drop` as well.
func runTodoDone(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("done")
	id, sessionID := todoTransitionTarget(args)
	result, err := workapp.Default.Done(cmd.Context(), call, workapp.CloseInput{
		TodoID: id, SessionID: sessionID, Reason: todoReasonFlag,
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if result.AlreadyClosed {
		if jsonOutput {
			output.JSON(result.Todo)
		} else {
			fmt.Printf("%s %s already %s: %s\n", result.Todo.Status, result.Todo.ID, result.Todo.Status, result.Todo.Title)
		}
		return nil
	}
	if jsonOutput {
		output.JSON(result.Todo)
		for _, event := range result.Awakened {
			fmt.Fprintf(os.Stderr, "awakened %s: %s\n", event.TodoID, event.Reason)
		}
		return nil
	}
	fmt.Printf("%s %s: %s\n", result.Todo.Status, result.Todo.ID, result.Todo.Title)
	for _, event := range result.Awakened {
		fmt.Printf("awakened %s: %s\n", event.TodoID, event.Reason)
	}
	return nil
}
