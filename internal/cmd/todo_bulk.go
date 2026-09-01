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
	todoBulkStatusFlag  string
	todoBulkReasonFlag  string
)

var todoBulkCmd = &cobra.Command{
	Use:   "bulk <done|move|edit> <todo-id>...",
	Short: "Apply one lifecycle or metadata change to multiple todos",
	Long: `Apply one explicit operation to an exact Todo set.

Bulk done is human acceptance and requires concrete --reason evidence. Agent
implementation completion is intentionally not a bulk done operation; submit
each independently reviewable Todo instead.`,
	Example: `  atm todo bulk done t10 t11 --reason "verified release checks and rollback"
  atm todo bulk move t10 t11 --project atm
  atm todo bulk edit t10 t11 --status open`,
	Args: cobra.MinimumNArgs(2),
	RunE: runTodoBulk,
}

func init() {
	todoBulkCmd.Flags().StringVar(&todoBulkProjectFlag, "project", "", "target project for move/edit")
	todoBulkCmd.Flags().StringVar(&todoBulkStatusFlag, "status", "", "target status for edit")
	todoBulkCmd.Flags().StringVar(&todoBulkReasonFlag, "reason", "", "acceptance evidence for bulk done (required on the first completion)")
	todoCmd.AddCommand(todoBulkCmd)
}

func runTodoBulk(cmd *cobra.Command, args []string) error {
	action := workapp.BulkAction(args[0])
	switch action {
	case workapp.BulkDone:
		if strings.TrimSpace(todoBulkProjectFlag) != "" || strings.TrimSpace(todoBulkStatusFlag) != "" {
			return fmt.Errorf("bulk done only accepts --reason; use `atm todo bulk done <id>... --reason \"<acceptance evidence>\"`")
		}
	case workapp.BulkMove:
		if strings.TrimSpace(todoBulkStatusFlag) != "" || strings.TrimSpace(todoBulkReasonFlag) != "" {
			return fmt.Errorf("bulk move only accepts --project; use `atm todo bulk move <id>... --project <repo>`")
		}
	case workapp.BulkEdit:
		if strings.TrimSpace(todoBulkReasonFlag) != "" {
			return fmt.Errorf("bulk edit does not accept --reason; use --project and/or --status open")
		}
	}
	if action == workapp.BulkEdit && strings.TrimSpace(todoBulkStatusFlag) != "" {
		if err := validateBulkEditStatusCLI(todoBulkStatusFlag, args[1:]); err != nil {
			return err
		}
	}
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

func validateBulkEditStatusCLI(status string, ids []string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		normalized = append(normalized, canonicalTodoIDForHint(id))
	}
	joined := strings.Join(normalized, " ")
	first := "<id>"
	if len(normalized) > 0 {
		first = normalized[0]
	}
	switch status {
	case "", "open":
		return nil
	case "in_progress":
		return fmt.Errorf("bulk edit cannot start work; run `atm todo start %s` for each Todo", first)
	case "review":
		return fmt.Errorf("bulk edit cannot submit work; run `atm todo submit %s --reason \"<result and evidence>\"` for each Todo", first)
	case "done":
		return fmt.Errorf("bulk edit cannot accept work; a human must run `atm todo bulk done %s --reason \"<acceptance evidence>\"`", joined)
	case "archived":
		return fmt.Errorf("bulk edit cannot archive work; run `atm todo archive %s`", joined)
	case "waiting", "blocked":
		return fmt.Errorf("%s is not a lifecycle status; set each in-progress Todo's wait with `atm todo edit %s --wake \"<observable condition>\"`", status, first)
	default:
		return nil // Work returns the authoritative closed-vocabulary validation.
	}
}
