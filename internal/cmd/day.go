package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

var (
	dayFromFlag string
	dayToFlag   string
)

func init() {
	dayRebuildCmd.Flags().StringVar(&dayFromFlag, "from", "", "first local day to rebuild (YYYY-MM-DD; default today)")
	dayRebuildCmd.Flags().StringVar(&dayToFlag, "to", "", "last local day to rebuild, inclusive (YYYY-MM-DD; default --from)")
	dayCmd.AddCommand(dayTodayCmd, dayShowCmd, dayRebuildCmd)
	rootCmd.AddCommand(dayCmd)
}

var dayCmd = &cobra.Command{
	Use:   "day",
	Short: "Build and inspect AI Day concepts",
	Args:  cobra.NoArgs,
	RunE:  showHelp,
}

var dayTodayCmd = &cobra.Command{
	Use:   "today",
	Short: "Build the recent baseline and show today's AI Day concept",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDB(false, func(db *sql.DB) error {
			if err := syncDaySources(db); err != nil {
				return err
			}
			// Zero-configuration first use still backfills the baseline window, but
			// only for days that have never been built. Rewriting already-built days
			// on every read let a routine refresh silently change last month's
			// badges; that stays an explicit `atm day rebuild`.
			summary, err := aiday.Refresh(cmd.Context(), db, time.Now(), config.Loc, 30)
			if err != nil {
				return err
			}
			printDay(summary.Days[len(summary.Days)-1])
			return nil
		})
	},
}

var dayShowCmd = &cobra.Command{
	Use:   "show <YYYY-MM-DD>",
	Short: "Show a previously built AI Day concept",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		day, err := parseLocalDay(args[0])
		if err != nil {
			return err
		}
		return withDB(true, func(db *sql.DB) error {
			result, err := aiday.Load(cmd.Context(), db, day.Format(time.DateOnly))
			if errors.Is(err, aiday.ErrDayNotBuilt) {
				return fmt.Errorf("AI Day %s has not been built; run `atm day rebuild --from %s`", args[0], args[0])
			}
			if err != nil {
				return err
			}
			printDay(result)
			return nil
		})
	},
}

var dayRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild AI Day projections for an inclusive date range",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		from, to, err := dayRebuildRange(time.Now().In(config.Loc), dayFromFlag, dayToFlag)
		if err != nil {
			return err
		}
		return withDB(false, func(db *sql.DB) error {
			if err := syncDaySources(db); err != nil {
				return err
			}
			summary, err := aiday.Rebuild(cmd.Context(), db, from, to, config.Loc)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(summary)
				return nil
			}
			fmt.Printf("Rebuilt %d AI Day projection(s), %s to %s.\n", summary.Count, summary.From, summary.To)
			for _, result := range summary.Days {
				printDayLine(result)
			}
			return nil
		})
	},
}

func parseLocalDay(value string) (time.Time, error) {
	day, err := time.ParseInLocation(time.DateOnly, value, config.Loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day %q (use YYYY-MM-DD)", value)
	}
	return day, nil
}

func dayRebuildRange(now time.Time, fromValue, toValue string) (time.Time, time.Time, error) {
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, config.Loc)
	var err error
	if fromValue != "" {
		from, err = parseLocalDay(fromValue)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	to := from
	if toValue != "" {
		to, err = parseLocalDay(toValue)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("--to %s is before --from %s", to.Format(time.DateOnly), from.Format(time.DateOnly))
	}
	return from, to, nil
}

func syncDaySources(db *sql.DB) error {
	if !syncBeforeRead {
		return nil
	}
	n, err := store.SyncAll(db)
	if err != nil {
		return fmt.Errorf("sync before AI Day rebuild: %w", err)
	}
	if n > 0 && !jsonOutput {
		output.Progress("Synced %d files.", n)
	}
	return nil
}

func printDay(result aiday.Result) {
	if jsonOutput {
		output.JSON(result)
		return
	}
	printDayLine(result)
	if result.Concept == nil {
		fmt.Println("  No indexed AI activity for this day.")
		return
	}
	fmt.Printf("  %s\n", result.Concept.Explanation)
	// The evidence is the point of the feature; printing only generic aggregates
	// left the one thing the card is supposed to justify off the screen.
	for _, evidence := range result.Concept.Evidence {
		line := fmt.Sprintf("  · %s %s", formatEvidenceValue(evidence), dayEvidenceLabel(evidence.Metric))
		if evidence.Comparison != "" {
			line += fmt.Sprintf(" (%s)", strings.Replace(evidence.Comparison, "recent_p", "近 30 日 P", 1))
		}
		fmt.Println(line)
	}
	if result.Concept.Origin == "user_corrected" {
		fmt.Printf("  由你修正 · 引擎原判断：%s\n", result.Concept.ComputedTitle)
	}
	fmt.Printf("  %d sessions · %d turns · %d tool calls · %s work tokens (%s incl. cache) · baseline %d days\n",
		result.Features.SessionCount, result.Features.TurnCount, result.Features.ToolCalls,
		formatDayTokens(result.Features.WorkTokens()), formatDayTokens(result.Features.TotalTokens()),
		result.BaselineDays)
	fmt.Printf("  confidence %.0f%% (evidence %.0f%%)", result.Concept.Confidence*100, result.Concept.EvidenceStrength*100)
	if result.Feedback != nil {
		fmt.Printf(" · feedback: %s", result.Feedback.Verdict)
	}
	fmt.Println()
	if result.Provisional {
		fmt.Printf("  ⧗ 今天还没结束，结论会随数据到达变化（数据截至 %s，未与历史比较）\n", formatDayClock(result.Coverage))
	}
	if result.Coverage != nil && !result.Coverage.Complete {
		fmt.Printf("  ⚠ 数据可能不完整：近 7 天活跃的 %s 今天还没有事件\n", strings.Join(result.Coverage.MissingSources, ", "))
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

func formatDayClock(coverage *aiday.Coverage) string {
	if coverage == nil || coverage.DataThrough == 0 {
		return "未知"
	}
	return time.Unix(coverage.DataThrough, 0).In(config.Loc).Format("15:04")
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

func formatDayTokens(value int64) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}
