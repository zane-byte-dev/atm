package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/output"
)

var guardRuleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Register and manage which commands the gate stops",
	Long: `A gated tool with no rules passes every invocation straight through, so
registering a CLI means adding at least one rule as well as installing the shim.

ATM ships rules for the outbound-communication commands its own skills tell agents
to run. Those can be switched off but not deleted; anything you add here can be
edited or removed.`,
	Args: cobra.NoArgs,
	RunE: showHelp,
}

var guardRuleListCmd = &cobra.Command{
	Use:   "list [tool]",
	Short: "List rules, including the ones switched off",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tools := guard.ToolNames()
		if len(args) == 1 {
			tools = []string{args[0]}
		}
		views := []guard.RuleView{}
		for _, tool := range tools {
			views = append(views, guard.RuleViews(tool)...)
		}
		if jsonOutput {
			output.JSON(views)
			return nil
		}
		if len(views) == 0 {
			fmt.Println("No guard rules.")
			return nil
		}
		for _, view := range views {
			state := "启用"
			if !view.Enabled {
				state = "已关闭"
			}
			origin := "自定义"
			switch {
			case view.Builtin && view.Overridden:
				origin = "内置（已改）"
			case view.Builtin:
				origin = "内置"
			}
			fmt.Printf("%-10s %-18s %-6s %-12s %s\n", view.Tool, view.ID, state, origin,
				guardRuleMatcherText(view))
			if view.Label != "" {
				fmt.Printf("           %s\n", view.Label)
			}
		}
		return nil
	},
}

var guardRuleSetCmd = &cobra.Command{
	Use:   "set <tool>",
	Short: "Add or edit a rule, reading its JSON from stdin",
	Long: `Reads one rule object from stdin and stores it under the tool.

	echo '{"id":"chat-send","label":"发送钉钉消息","path":["chat","message","send"],
	       "target":{"flags":["--group","--user"]},"body":{"flags":["--text"]}}' \
	  | atm guard rule set dws

Stdin rather than flags because a rule is a nested object, and because this is the
path the settings UI uses. Switching a built-in off needs only its id:

	echo '{"id":"mr-remind","enabled":false}' | atm guard rule set a1`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		payload, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(payload))) == 0 {
			return fmt.Errorf("no rule on stdin")
		}
		var rule config.GuardRule
		decoder := json.NewDecoder(strings.NewReader(string(payload)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rule); err != nil {
			return fmt.Errorf("rule is not valid: %w", err)
		}
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("rule needs an id")
		}
		tool := strings.TrimSpace(args[0])
		// A rule with no matcher is only meaningful as a patch onto a built-in of
		// the same id. Accepting one for an id nothing ships would store something
		// that can never match and would make every call to that tool fail closed.
		if !rule.HasMatcher() {
			known := false
			for _, view := range guard.RuleViews(tool) {
				if view.ID == rule.ID && view.Builtin {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf(
					"rule %q has no path or argv_pattern, and there is no built-in %q to patch",
					rule.ID, rule.ID)
			}
		}
		if err := config.SaveGuardRule(tool, rule); err != nil {
			return err
		}
		return guardReportRules(tool)
	},
}

var guardRuleRemoveCmd = &cobra.Command{
	Use:     "remove <tool> <rule-id>",
	Short:   "Remove a rule you added, or drop an override of a built-in",
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tool, ruleID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		// Removing an override of a built-in restores the built-in, which is not
		// what someone who wants the action to stop being gated means. Say so rather
		// than let them find out by having it keep firing.
		for _, view := range guard.RuleViews(tool) {
			if view.ID == ruleID && view.Builtin && !view.Overridden {
				return fmt.Errorf(
					"%s/%s is built in and cannot be removed; switch it off instead: "+
						`echo '{"id":%q,"enabled":false}' | atm guard rule set %s`,
					tool, ruleID, ruleID, tool)
			}
		}
		if err := config.RemoveGuardRule(tool, ruleID); err != nil {
			return err
		}
		return guardReportRules(tool)
	},
}

var guardToolRemoveCmd = &cobra.Command{
	Use:   "forget <tool>",
	Short: "Forget a registered tool: its rules and its recorded install path",
	Long: `Removes everything the config says about a tool.

It does not touch the filesystem. Uninstall the shim first — forgetting where a
shim is while leaving it in place is worse than either on its own.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tool := strings.TrimSpace(args[0])
		binPath, err := guard.Resolve(tool, "")
		if err == nil {
			state, statusErr := guard.Status(tool, binPath)
			if statusErr == nil && state.Installed {
				return fmt.Errorf(
					"%s still has a shim at %s; run `atm guard uninstall %s` first",
					tool, state.BinPath, tool)
			}
		}
		if err := config.RemoveGuardTool(tool); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"tool": tool, "forgotten": true})
			return nil
		}
		fmt.Printf("已忘记 %s（配置里的规则和安装位置都清掉了）\n", tool)
		return nil
	},
}

func guardReportRules(tool string) error {
	views := guard.RuleViews(tool)
	if jsonOutput {
		output.JSON(views)
		return nil
	}
	active := 0
	for _, view := range views {
		if view.Enabled {
			active++
		}
	}
	fmt.Printf("%s：%d 条规则，%d 条启用\n", tool, len(views), active)
	if active == 0 {
		fmt.Println("没有启用的规则，这个工具的调用会全部直通。")
	}
	return nil
}

func guardRuleMatcherText(view guard.RuleView) string {
	parts := []string{}
	if len(view.Path) > 0 {
		parts = append(parts, strings.Join(view.Path, " "))
	}
	if view.ArgvPattern != "" {
		parts = append(parts, "~ "+view.ArgvPattern)
	}
	sort.Strings(parts)
	return strings.Join(parts, " · ")
}
