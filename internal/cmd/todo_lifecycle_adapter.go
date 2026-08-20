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

func runTodoDone(cmd *cobra.Command, args []string) error {
	return runTodoClose(cmd, args, true)
}

func runTodoDrop(cmd *cobra.Command, args []string) error {
	return runTodoClose(cmd, args, false)
}

func runTodoClose(cmd *cobra.Command, args []string, done bool) error {
	action := "drop"
	if done {
		action = "done"
	}
	call := todoWorkflowCLICall(action)
	id, sessionID := todoTransitionTarget(args)
	input := workapp.CloseInput{TodoID: id, SessionID: sessionID, Reason: todoReasonFlag}
	var (
		result workapp.CloseResult
		err    error
	)
	if done {
		result, err = workapp.Default.Done(cmd.Context(), call, input)
	} else {
		result, err = workapp.Default.Drop(cmd.Context(), call, input)
	}
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

func runTodoWake(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("wake")
	result, err := workapp.Default.Wake(cmd.Context(), call, workapp.WakeInput{
		TodoID: args[0],
		Status: todoWakeStatusFlag,
		Reason: todoWakeReasonFlag,
	})
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
	fmt.Printf("Awakened %s to %s: %s\n", result.Todo.ID, result.Todo.Status, result.Todo.Title)
	return nil
}

func runTodoReconcile(cmd *cobra.Command, _ []string) error {
	call := todoWorkflowCLICall("reconcile")
	result, err := workapp.Default.Reconcile(cmd.Context(), call, workapp.ReconcileInput{})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"awakened": result.Awakened, "issues": result.Issues})
		return nil
	}
	fmt.Printf("Reconciled dependencies: awakened=%d issues=%d\n", len(result.Awakened), len(result.Issues))
	for _, event := range result.Awakened {
		fmt.Printf("  wake  %-6s %s\n", event.TodoID, event.Reason)
	}
	for _, issue := range result.Issues {
		fmt.Printf("  issue %-6s %-20s %s\n", issue.TodoID, issue.Code, issue.Detail)
	}
	return nil
}
