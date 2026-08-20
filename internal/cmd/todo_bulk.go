package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoBulkProjectFlag string
	todoBulkStatusFlag  string
	todoBulkReasonFlag  string
)

var todoBulkCmd = &cobra.Command{
	Use:   "bulk <done|move|edit> <todo-id>...",
	Short: "Apply one lifecycle or metadata change to multiple todos",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runTodoBulk,
}

func init() {
	todoBulkCmd.Flags().StringVar(&todoBulkProjectFlag, "project", "", "target project for move/edit")
	todoBulkCmd.Flags().StringVar(&todoBulkStatusFlag, "status", "", "target status for edit")
	todoBulkCmd.Flags().StringVar(&todoBulkReasonFlag, "reason", "", "reason recorded for done")
	todoCmd.AddCommand(todoBulkCmd)
}

func runTodoBulk(cmd *cobra.Command, args []string) error {
	action := workapp.BulkAction(args[0])
	call := todoWorkflowCLICall("bulk-" + string(action))
	result, err := workapp.Default.Bulk(cmd.Context(), call, workapp.BulkInput{
		Action:    action,
		TodoIDs:   args[1:],
		Project:   todoBulkProjectFlag,
		Status:    todoBulkStatusFlag,
		Reason:    todoBulkReasonFlag,
		Confirmed: true,
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"action": result.Action, "todos": result.Todos, "awakened": result.Awakened})
		return nil
	}
	fmt.Printf("Bulk %s updated %d todos\n", result.Action, len(result.Todos))
	return nil
}
