package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoBulkProjectFlag string
	todoBulkReasonFlag  string
)

var todoBulkCmd = &cobra.Command{
	Use:   "bulk <done|move> <todo-id>...",
	Short: "Apply one lifecycle or metadata change to multiple todos",
	Long: `Apply one explicit operation to an exact Todo set.

Bulk done is human acceptance and requires concrete --reason evidence. Agent
implementation completion is intentionally not a bulk done operation; submit
each independently reviewable Todo instead.`,
	Example: `  atm todo bulk done t10 t11 --reason "verified release checks and rollback"
	  atm todo bulk move t10 t11 --project atm`,
	Args: cobra.MinimumNArgs(2),
	RunE: runTodoBulk,
}

func init() {
	todoBulkCmd.Flags().StringVar(&todoBulkProjectFlag, "project", "", "target project for move")
	todoBulkCmd.Flags().StringVar(&todoBulkReasonFlag, "reason", "", "acceptance evidence for bulk done (required on the first completion)")
	todoCmd.AddCommand(todoBulkCmd)
}

func runTodoBulk(cmd *cobra.Command, args []string) error {
	action := workapp.BulkAction(args[0])
	switch action {
	case workapp.BulkDone:
		if strings.TrimSpace(todoBulkProjectFlag) != "" {
			return fmt.Errorf("bulk done only accepts --reason; use `atm todo bulk done <id>... --reason \"<acceptance evidence>\"`")
		}
	case workapp.BulkMove:
		if strings.TrimSpace(todoBulkReasonFlag) != "" {
			return fmt.Errorf("bulk move only accepts --project; use `atm todo bulk move <id>... --project <repo>`")
		}
	}
	call := todoWorkflowCLICall("bulk-" + string(action))
	result, err := workapp.Default.Bulk(cmd.Context(), call, workapp.BulkInput{
		Action:    action,
		TodoIDs:   args[1:],
		Project:   todoBulkProjectFlag,
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
