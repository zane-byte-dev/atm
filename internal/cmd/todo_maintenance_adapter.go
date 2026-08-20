package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func runTodoMaintain(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("maintain")
	result, err := workapp.Default.Maintain(cmd.Context(), call, workapp.MaintainInput{
		TodoID: args[0],
		Limit:  todoMaintainLimitFlag,
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
	fmt.Printf("Maintaining %s (limit %d): %s\n", result.Todo.ID, result.Todo.MaintenanceLimit, result.Todo.Title)
	return nil
}
