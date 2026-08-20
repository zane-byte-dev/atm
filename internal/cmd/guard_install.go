package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/output"
)

var guardInstallCmd = &cobra.Command{
	Use:   "install [tool...]",
	Short: "Put a gate in front of a tool's own binary",
	Long: `Moves the tool's binary aside and takes its place, so every agent on this
machine goes through the gate without any of them being configured.

Interposition happens at whatever PATH resolves the tool to, unless --bin says
otherwise. That default matters: several of these tools exist in more than one
place, and gating the copy PATH does not use gates nothing at all while looking
like it worked. Run status afterwards.

With no arguments, installs every tool that has rules.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGuardShim(cmd, args, "install")
	},
}

var guardUninstallCmd = &cobra.Command{
	Use:   "uninstall [tool...]",
	Short: "Remove the gate and restore the tool's own binary",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGuardShim(cmd, args, "uninstall")
	},
}

var guardStatusCmd = &cobra.Command{
	Use:   "status [tool...]",
	Short: "Show which tools are gated, and which are being bypassed",
	Long: `Reports each tool's shim, and two problems worth knowing about separately.

clobbered — something overwrote the shim, usually the tool upgrading itself.
shadowed  — PATH finds a different copy of the tool first, so invocations by
            bare name never reach the gate.

It also states what the gate does not cover: an outbound action taken through an
MCP tool rather than a command is invisible to it.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGuardShim(cmd, args, "status")
	},
}

func runGuardShim(cmd *cobra.Command, args []string, mode string) error {
	input := guard.ShimInput{Tools: args, Bin: guardInstallBin}
	var result guard.ShimResult
	var err error
	switch mode {
	case "install":
		result, err = guard.Default.InstallTools(cmd.Context(), guardCLICall(), input)
	case "uninstall":
		result, err = guard.Default.UninstallTools(cmd.Context(), guardCLICall(), input)
	case "status":
		result, err = guard.Default.StatusTools(cmd.Context(), guardCLICall(), input)
	default:
		return fmt.Errorf("unknown Guard shim operation: %s", mode)
	}
	// The service can return successful states alongside per-tool failures. Keep
	// rendering that useful partial result before Cobra reports the typed error.
	if result.States != nil {
		if jsonOutput {
			output.JSON(result.States)
		} else {
			printGuardStates(result.States, mode)
		}
	}
	return err
}

func printGuardStates(states []guard.ShimState, mode string) {
	for _, state := range states {
		if state.BinPath == "" {
			// Not on PATH and no --bin. Normal for a tool only ever invoked by
			// absolute path, so say what to do rather than treating it as broken.
			fmt.Printf("%-10s 未找到（%d 条规则）\n", state.Tool, state.Rules)
			fmt.Printf("           PATH 上没有，需要 atm guard install %s --bin <绝对路径>\n", state.Tool)
			continue
		}
		fmt.Printf("%-10s %s\n", state.Tool, guardShimSummary(state))
		fmt.Printf("           %s\n", state.BinPath)
		if state.ShadowedBy != "" {
			// The single most important line this command prints: the gate is
			// installed and being walked around.
			fmt.Printf("           ⚠️  PATH 先找到 %s，走这条路的调用不经过闸门\n", state.ShadowedBy)
		}
		if state.Installed && state.Rules == 0 {
			fmt.Printf("           ⚠️  没有任何规则，这个工具的调用全部直通\n")
		}
	}
	if mode == "status" {
		fmt.Println("\n闸门只看命令执行。通过 MCP 工具完成的外发动作不经过这里，" +
			"ATM 也拦不到 —— 用 atm doctor 检查是否配置了 MCP server。")
	}
}

func guardShimSummary(state guard.ShimState) string {
	switch {
	case state.Clobbered && state.Installed:
		return fmt.Sprintf("shim 在位但真身丢了（%d 条规则）· 需要修复", state.Rules)
	case state.Clobbered:
		return fmt.Sprintf("被覆盖：不是 ATM 的 shim，但真身还在旁边（%d 条规则）· atm guard install 可修复", state.Rules)
	case state.Installed:
		return fmt.Sprintf("已启用（%d 条规则）", state.Rules)
	default:
		return fmt.Sprintf("未启用（%d 条规则）", state.Rules)
	}
}
