package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/config"
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
	if !guardExecSupported() {
		return fmt.Errorf("the outbound action gate is not supported on this platform")
	}
	tools := args
	if len(tools) == 0 {
		for tool, toolConfig := range guard.Tools() {
			if len(toolConfig.Rules) > 0 {
				tools = append(tools, tool)
			}
		}
		sort.Strings(tools)
	}
	if len(tools) == 0 {
		return fmt.Errorf("no tools have guard rules")
	}
	if guardInstallBin != "" && len(tools) != 1 {
		return fmt.Errorf("--bin applies to one tool at a time")
	}

	atmPath := ""
	if mode == "install" {
		resolved, err := atmExecutablePath()
		if err != nil {
			return err
		}
		atmPath = resolved
	}

	states := []guard.ShimState{}
	var failures []string
	for _, tool := range tools {
		binPath, err := guard.Resolve(tool, guardInstallBin)
		if err != nil {
			// status describes what is there; install and uninstall were asked to do
			// something they cannot. Only the latter is a failure.
			if mode == "status" {
				states = append(states, guard.ShimState{Tool: tool, Rules: len(guard.Rules(tool))})
				continue
			}
			failures = append(failures, fmt.Sprintf("%s: %v", tool, err))
			continue
		}
		var state guard.ShimState
		switch mode {
		case "install":
			state, err = guard.Install(tool, binPath, atmPath)
		case "uninstall":
			state, err = guard.Uninstall(tool, binPath)
		default:
			state, err = guard.Status(tool, binPath)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", tool, err))
			continue
		}
		if mode == "install" && state.Installed {
			// Remember where it went, or a tool that is not on PATH becomes invisible
			// to status and doctor the moment this process exits — and with it the
			// checks for a shim that was overwritten or is being walked around.
			if err := config.SaveGuardToolBin(tool, binPath); err != nil {
				failures = append(failures, fmt.Sprintf(
					"%s: 闸门装好了，但没能把安装位置写进 %s（%v）；"+
						"status 和 doctor 之后看不到它，请手工补 guard.tools.%s.bin",
					tool, config.ConfigPath, err, tool))
			}
		}
		states = append(states, state)
	}

	if jsonOutput {
		output.JSON(states)
	} else {
		printGuardStates(states, mode)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
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
