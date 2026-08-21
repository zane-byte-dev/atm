package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/output"
)

var guardApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a gated action and run it",
	Long: `Approves the request and, unless a waiting agent still owns it, runs the command.

ATM runs it itself because by the time you decide, the agent that asked has
usually been told not to retry and has moved on. Pass --run=false to record the
approval without running anything.

Only what came through an installed shim can be run this way, and a command whose
input arrived on a pipe is refused rather than replayed against different content.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGuardDecision(cmd, args[0], true)
	},
}

var guardDenyCmd = &cobra.Command{
	Use:   "deny <id>",
	Short: "Refuse a gated action",
	Long: `Records a refusal. The waiting agent is told to hand you the content instead.

The refusal also answers for a short while: an agent re-running the identical
command is refused from the record rather than raising the same request again.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGuardDecision(cmd, args[0], false)
	},
}

func runGuardDecision(cmd *cobra.Command, id string, approve bool) error {
	result, err := guard.Default.Decide(cmd.Context(), guardCLICall(), guard.DecisionInput{
		ID: id, Approve: approve, Run: guardApproveRun,
		Reason: guardDecideReason, DecidedBy: guardDecideBy,
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Approval)
		return nil
	}
	switch result.Outcome {
	case guard.OutcomeDenied:
		fmt.Printf("已拒绝 %s：%s\n", id, guard.ActionLine(result.Approval))
	case guard.OutcomeApproved:
		fmt.Printf("已批准 %s（未执行）：%s\n", id, guard.ActionLine(result.Approval))
	case guard.OutcomeApprovedGateRun:
		fmt.Printf("已批准 %s，由正在等待的调用方执行：%s\n", id, guard.ActionLine(result.Approval))
	case guard.OutcomeApprovedAndRan:
		fmt.Printf("已批准并执行 %s：%s\n", id, guard.ActionLine(result.Approval))
	}
	return nil
}
