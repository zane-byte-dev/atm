package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/output"
)

var (
	dayFromFlag string
	dayToFlag   string
)

func init() {
	dayRebuildCmd.Flags().StringVar(&dayFromFlag, "from", "", "first local day to rebuild (YYYY-MM-DD; default today)")
	dayRebuildCmd.Flags().StringVar(&dayToFlag, "to", "", "last local day to rebuild, inclusive (YYYY-MM-DD; default --from)")
	dayCmd.AddCommand(dayRebuildCmd)
	rootCmd.AddCommand(dayCmd)
}

var dayCmd = &cobra.Command{
	Use:   "day",
	Short: "Repair and diagnose AI Day projections",
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var dayRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild AI Day projections for an inclusive date range",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		summary, meta, err := aiday.Default.Rebuild(cmd.Context(), aiday.RebuildInput{
			From: dayFromFlag,
			To:   dayToFlag,
			Sync: syncBeforeRead,
		})
		if err != nil {
			return err
		}
		printDaySync(meta)
		if jsonOutput {
			output.JSON(summary)
			return nil
		}
		fmt.Printf("Rebuilt %d AI Day projection(s), %s to %s.\n", summary.Count, summary.From, summary.To)
		for _, result := range summary.Days {
			printDayLine(result)
		}
		return nil
	},
}

func printDaySync(meta aiday.OperationMeta) {
	if meta.SyncedFiles > 0 && !jsonOutput {
		output.Progress("Synced %d files.", meta.SyncedFiles)
	}
}

func printDayLine(result aiday.Result) {
	title := "No indexed activity"
	if result.Concept != nil {
		title = result.Concept.Title
	}
	suffix := ""
	if result.Provisional {
		suffix = "  (provisional)"
	}
	fmt.Printf("%s  %s%s\n", result.Day, title, suffix)
}

func formatEvidenceValue(evidence aiday.Evidence) string {
	if evidence.Value == float64(int64(evidence.Value)) {
		return fmt.Sprintf("%d%s", int64(evidence.Value), evidenceUnitSuffix(evidence.Unit))
	}
	return fmt.Sprintf("%.1f%s", evidence.Value, evidenceUnitSuffix(evidence.Unit))
}

func evidenceUnitSuffix(unit string) string {
	if unit == "%" {
		return "%"
	}
	return ""
}

func dayEvidenceLabel(metric string) string {
	labels := map[string]string{
		"source_count": "AI 来源", "session_count": "会话", "turn_count": "对话轮次",
		"tool_calls": "工具调用", "total_tokens": "Token", "work_tokens": "有效 Token",
		"generation_seconds": "生成秒数", "code_events": "代码事件", "visual_events": "视觉事件",
		"quality_loops": "质检循环", "refinements": "细化", "detail_turns": "细节追问",
		"modality_count": "任务模态", "corrections": "纠正", "acceptances": "直接确认",
		"consecutive_days": "连续使用", "modality_share": "模态占比", "loop_share": "质检占比",
		"detail_share": "追问占比", "correction_share": "纠正占比", "acceptance_share": "确认占比",
	}
	if label, ok := labels[metric]; ok {
		return label
	}
	return metric
}
