package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/output"
)

func init() {
	daySourcesCmd.AddCommand(daySourcesListCmd)
	dayCmd.AddCommand(dayBadgeCmd, daySourcesCmd, dayExportCmd)
}

var dayBadgeCmd = &cobra.Command{Use: "badge <id>", Short: "Show badge progress and evidence", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	badge, err := aiday.Default.Badge(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(badge)
		return nil
	}
	fmt.Printf("%s · L%d · 累计 %d 天\n%s\n", badge.Name, badge.Level, badge.QualifiedDays, badge.Description)
	if badge.NextLevelDays > badge.QualifiedDays {
		fmt.Printf("距 L%d 还差 %d 天（%d/%d）\n", badge.Level+1, badge.NextLevelDays-badge.QualifiedDays, badge.QualifiedDays, badge.NextLevelDays)
	}
	if badge.CooldownUntil != "" {
		fmt.Printf("即时徽章冷却至 %s\n", badge.CooldownUntil)
	}
	for _, evidence := range badge.Evidence {
		fmt.Printf("  · %s %s\n", formatEvidenceValue(evidence), dayEvidenceLabel(evidence.Metric))
	}
	if len(badge.QualifiedDates) > 0 {
		shown := badge.QualifiedDates
		if len(shown) > 8 {
			shown = shown[:8]
		}
		fmt.Printf("最近达成：%s\n", strings.Join(shown, "  "))
	}
	return nil
}}

var daySourcesCmd = &cobra.Command{Use: "sources", Short: "Inspect AI Day source state", Args: noSubcommandArgs, RunE: showHelp}
var daySourcesListCmd = &cobra.Command{Use: "list", Short: "List source permissions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	sources, err := aiday.Default.Sources(cmd.Context())
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(sources)
		return nil
	}
	for _, source := range sources {
		state := "enabled"
		if !source.Enabled {
			state = "paused"
		}
		fmt.Printf("%-16s %-8s semantic=%t events=%d\n", source.Source, state, source.SemanticEnabled, source.EventCount)
	}
	return nil
}}

var dayExportCmd = &cobra.Command{Use: "export", Short: "Export derived AI Day data for recovery or support", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
	exported, err := aiday.Default.Export(cmd.Context())
	if err != nil {
		return err
	}
	output.JSON(exported)
	return nil
}}
