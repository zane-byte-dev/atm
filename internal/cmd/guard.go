package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

var (
	guardExecTool     string
	guardExecWait     time.Duration
	guardExecExpire   time.Duration
	guardListStatus   string
	guardListLimit    int
	guardDecideReason string
	guardDecideBy     string
	guardApproveRun   bool
	guardInstallBin   string
)

var guardCmd = &cobra.Command{
	Use:   "guard",
	Short: "Gate outbound actions an agent runs through a local CLI",
	Long: `Stops a command that reaches somebody else from running unreviewed.

An agent calls a gated CLI as usual. Reads pass straight through, untouched. A
command that matches a rule — sending a DingTalk message, pushing to a group,
nudging a reviewer — becomes a request here instead, ATM notifies you, and
nothing is sent until you approve it. Approving runs the command, so the agent
does not have to still be around.

This exists because the skills that drive these CLIs pass the tool's own
"skip confirmation" flag, which leaves no prompt anywhere.`,
	Args: noSubcommandArgs,
	RunE: showHelp,
}

func init() {
	guardExecCmd.Flags().StringVar(&guardExecTool, "tool", "",
		"registered tool name whose rules apply (required)")
	guardExecCmd.Flags().DurationVar(&guardExecWait, "wait", 0,
		"how long to wait for a decision before reporting the request still pending")
	guardExecCmd.Flags().DurationVar(&guardExecExpire, "expire", 0,
		"how long the request stays decidable after the wait gives up")
	guardExecCmd.MarkFlagRequired("tool")
	// The gated tool's own flags must reach it verbatim, so cobra stops parsing at
	// the first non-flag argument.
	guardExecCmd.Flags().SetInterspersed(false)

	guardListCmd.Flags().StringVar(&guardListStatus, "status", "pending",
		"filter by status: pending, approved, running, done, denied, expired, or all")
	guardListCmd.Flags().IntVar(&guardListLimit, "limit", 50, "maximum rows")

	guardApproveCmd.Flags().StringVar(&guardDecideReason, "reason", "", "why, for the record")
	guardApproveCmd.Flags().StringVar(&guardDecideBy, "by", "cli", "which surface decided")
	guardApproveCmd.Flags().BoolVar(&guardApproveRun, "run", true,
		"run the command now; --run=false records the approval and leaves it to whoever is waiting")
	guardDenyCmd.Flags().StringVar(&guardDecideReason, "reason", "", "why, for the record")
	guardDenyCmd.Flags().StringVar(&guardDecideBy, "by", "cli", "which surface decided")

	guardInstallCmd.Flags().StringVar(&guardInstallBin, "bin", "",
		"path to the real binary (default: whatever PATH resolves the tool to)")
	guardUninstallCmd.Flags().StringVar(&guardInstallBin, "bin", "",
		"path the shim was installed at (default: whatever PATH resolves the tool to)")
	guardStatusCmd.Flags().StringVar(&guardInstallBin, "bin", "",
		"path to check (default: whatever PATH resolves the tool to)")

	guardRuleCmd.AddCommand(guardRuleListCmd, guardRuleSetCmd, guardRuleRemoveCmd)
	guardCmd.AddCommand(guardExecCmd, guardListCmd, guardShowCmd, guardApproveCmd,
		guardDenyCmd, guardInstallCmd, guardUninstallCmd, guardStatusCmd,
		guardRuleCmd, guardToolRemoveCmd)
	rootCmd.AddCommand(guardCmd)
}
