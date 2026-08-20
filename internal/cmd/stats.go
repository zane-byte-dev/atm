package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	statsapp "github.com/zane-byte-dev/atm/internal/stats"

	"github.com/spf13/cobra"
)

func init() {
	statsCmd.Flags().IntVar(&statsDaysFlag, "days", 1, "number of days to look back (rolling window)")
	statsCmd.Flags().StringVar(&statsRangeFlag, "range", "", "named window: today, yesterday, this_week, last_week, this_month, last_7_days, last_30_days")
	statsCmd.Flags().StringVar(&statsByFlag, "by", "", "group by: model, model-day, model-hour, skill, session, session-usage, request, speed, day, hour, wrapped")
	statsCmd.Flags().StringVar(&statsSessionFlag, "session", "", "filter request stats by session id")
	statsCmd.MarkFlagsMutuallyExclusive("days", "range")
	rootCmd.AddCommand(statsCmd)
}

var (
	statsDaysFlag    int
	statsRangeFlag   string
	statsByFlag      string
	statsSessionFlag string
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show usage statistics",
	RunE:  runStats,
}

func runStats(cmd *cobra.Command, args []string) error {
	days := statsDaysFlag
	if statsRangeFlag != "" {
		// Cobra already makes --days and --range mutually exclusive. Zero here
		// distinguishes the default flag value from an explicitly requested rolling
		// window at the application boundary.
		days = 0
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := statsapp.Default.Query(ctx, cliApplicationCall("stats", ""), statsapp.Input{
		Days: days, Range: statsRangeFlag, Group: statsByFlag, Agent: agentFlag,
		SessionID: statsSessionFlag, Sync: syncBeforeRead,
	})
	if err != nil {
		return err
	}
	if result.SyncedFiles > 0 && !jsonOutput {
		output.Progress("Synced %d files.", result.SyncedFiles)
	}
	switch result.Group {
	case statsapp.GroupModel:
		return runModelStats(result)
	case statsapp.GroupModelDay:
		return runModelDayStats(result)
	case statsapp.GroupModelHour:
		return runModelHourStats(result)
	case statsapp.GroupSkill:
		return runSkillStats(result)
	case statsapp.GroupSession:
		return runSessionStats(result)
	case statsapp.GroupSessionUsage:
		return runSessionUsageStats(result)
	case statsapp.GroupRequest:
		return runRequestStats(result)
	case statsapp.GroupSpeed:
		return runSpeedStats(result)
	case statsapp.GroupDay:
		return runDayStats(result)
	case statsapp.GroupHour:
		return runHourStats(result)
	case statsapp.GroupWrapped:
		return runWrapped(result)
	}

	results := result.Projects
	ok := statsSection(results, "Statistics", result.Window.Label, 60, "No activity recorded.")
	if !ok {
		return nil
	}

	fmt.Printf("\n  %-20s %-11s %8s %8s %8s %10s %10s %8s\n",
		"Project", "Agent", "Sessions", "Queries", "Tools", "In", "Out", "Cost($)")
	statsSep := output.Dashes(20, 11, 8, 8, 8, 10, 10, 8)
	fmt.Printf("  %-20s %-11s %8s %8s %8s %10s %10s %8s\n", statsSep...)

	for _, r := range results {
		fmt.Printf("  %-20s %-11s %8d %8d %8d %10s %10s %8.2f\n",
			r.Project, r.Agent, r.Sessions, r.Queries, r.ToolCalls,
			fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), r.CostUSD)
	}
	fmt.Printf("  %-20s %-11s %8s %8s %8s %10s %10s %8s\n", statsSep...)
	fmt.Printf("  %-20s %-11s %8d %8d %8d %10s %10s %8.2f\n",
		"Total", "", result.Totals.Sessions, result.Totals.Queries, result.Totals.ToolCalls,
		fmtTokens(result.Totals.InputTokens), fmtTokens(result.Totals.OutputTokens), result.Totals.CostUSD)

	printSubscriptionSummary(result.Subscription)
	return nil
}

func runSkillStats(result statsapp.Result) error {
	results := result.Skills
	ok := statsSection(results, "Statistics by Skill", result.Window.Label, 60, "No skill activity recorded.")
	if !ok {
		return nil
	}
	fmt.Printf("\n  %-32s %8s %10s %8s\n", "Skill", "Calls", "Sessions", "Agents")
	for _, result := range results {
		fmt.Printf("  %-32s %8d %10d %8d\n", result.Skill, result.Calls, result.Sessions, result.Agents)
	}
	return nil
}

func runRequestStats(result statsapp.Result) error {
	results := result.Requests
	ok := statsSection(results, "Statistics by Request", result.Window.Label, 100, "No request-level usage recorded.")
	if !ok {
		return nil
	}
	// Req shows model-call multiplicity (×N when a row aggregates several calls,
	// as Grok turn_completed does). Tokens/cost on the row are the full total.
	fmt.Printf("\n  %-16s %-11s %-12s %-24s %5s %8s %8s %8s %8s\n", "Time", "Agent", "Session", "Model", "Req", "In", "Out", "Cache", "Cost($)")
	for _, r := range results {
		model := r.Model
		if len(model) > 24 {
			model = model[:24]
		}
		calls := r.RequestCount
		if calls <= 0 {
			calls = 1
		}
		fmt.Printf("  %-16s %-11s %-12s %-24s %5s %8s %8s %8s %8.4f\n",
			time.Unix(r.TS, 0).In(config.Loc).Format("01-02 15:04:05"),
			r.Agent, r.SessionID, model, fmtRequestCount(calls),
			fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), fmtTokens(r.CacheTokens), r.CostUSD)
	}
	if len(results) > 0 && result.Totals.Requests != len(results) {
		fmt.Printf("\n  %d rows · %d model calls\n", len(results), result.Totals.Requests)
	}
	return nil
}

// fmtRequestCount renders 1 as "1" and n>1 as "×N" so aggregated turns (Grok)
// are obvious without cluttering single-call agents.
func fmtRequestCount(n int) string {
	if n <= 1 {
		return "1"
	}
	return fmt.Sprintf("×%d", n)
}

// runSpeedStats shows the two speeds separately, because they answer different
// questions: how fast the model writes, and how long a turn keeps the user
// waiting. Both are derived from transcript timestamps rather than logged by any
// agent, so the closing lines say how many requests could not be measured — an
// agent missing from the first table is unmeasurable, not idle.
func runSpeedStats(result statsapp.Result) error {
	report := result.Speed
	if jsonOutput {
		output.JSON(report)
		return nil
	}
	fmt.Printf("Statistics by Speed (%s)\n", result.Window.Label)
	fmt.Println(strings.Repeat("=", 88))
	if len(report.Models) == 0 && len(report.Turns) == 0 {
		fmt.Println("\nNo activity recorded.")
		return nil
	}

	fmt.Printf("\n  Output speed — model generation only, tool time excluded\n")
	fmt.Printf("  %-10s %-26s %7s %7s %7s %7s %8s %8s\n",
		"Client", "Model", "Reqs", "Timed", "tok/s", "p90", "gen p50", "gen p90")
	fmt.Printf("  %-10s %-26s %7s %7s %7s %7s %8s %8s\n",
		output.Dashes(10, 26, 7, 7, 7, 7, 8, 8)...)
	for _, r := range report.Models {
		model := r.Model
		if len(model) > 26 {
			model = model[:26]
		}
		if r.Sampled == 0 {
			fmt.Printf("  %-10s %-26s %7d %7d %7s %7s %8s %8s\n",
				r.Client, model, r.Requests, 0, "-", "-", "-", "-")
			continue
		}
		fmt.Printf("  %-10s %-26s %7d %7d %7.1f %7.1f %8s %8s\n",
			r.Client, model, r.Requests, r.Sampled,
			r.TokensPerSecondP50, r.TokensPerSecondP90,
			fmtSeconds(r.DurationP50Seconds), fmtSeconds(r.DurationP90Seconds))
	}

	if len(report.Turns) > 0 {
		fmt.Printf("\n  Turn wait — human message to the last reply, tools and every internal request included\n")
		fmt.Printf("  %-10s %7s %9s %9s %9s %10s\n",
			"Client", "Turns", "p50", "p90", "longest", "reqs/turn")
		fmt.Printf("  %-10s %7s %9s %9s %9s %10s\n",
			output.Dashes(10, 7, 9, 9, 9, 10)...)
		for _, t := range report.Turns {
			fmt.Printf("  %-10s %7d %9s %9s %9s %10.1f\n",
				t.Agent, t.Turns,
				fmtSeconds(t.WaitP50Seconds), fmtSeconds(t.WaitP90Seconds),
				fmtSeconds(t.WaitMaxSeconds), t.RequestsPerTurn)
		}
	}

	if report.Untimed > 0 || report.OutOfWindow > 0 {
		fmt.Printf("\n  Left out: %d requests the transcript does not time, %d outside the sample window.\n",
			report.Untimed, report.OutOfWindow)
		fmt.Printf("  `atm doctor` breaks the first number down by agent.\n")
	}
	return nil
}

// fmtSeconds keeps sub-minute durations precise — the difference between a 4s and
// a 9s response is the whole point — and hands longer ones to the shared
// short-duration format.
func fmtSeconds(seconds float64) string {
	if seconds <= 0 {
		return "-"
	}
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	return formatShortDuration(int64(seconds))
}

func runSessionStats(result statsapp.Result) error {
	results := result.Sessions
	ok := statsSection(results, "Statistics by Session", result.Window.Label, 80, "No activity recorded.")
	if !ok {
		return nil
	}

	fmt.Printf("\n  %-3s %-10s %-16s %-16s %4s %8s %8s %8s %8s %5s\n",
		"#", "Session", "Project", "Model", "Req", "In", "Out", "Cache", "Cost($)", "%")
	sep := strings.Repeat("-", 88)
	fmt.Printf("  %s\n", sep)

	for i, r := range results {
		model := r.Model
		if len(model) > 16 {
			model = model[:16]
		}
		project := r.Project
		if len(project) > 16 {
			project = project[:16]
		}
		fmt.Printf("  %-3d %-10s %-16s %-16s %4d %8s %8s %8s %8.2f %4.0f%%\n",
			i+1, r.ShortID, project, model, r.Queries,
			fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), fmtTokens(r.CacheTokens),
			r.CostUSD, r.Share*100)
	}
	fmt.Printf("  %s\n", sep)
	fmt.Printf("  %-3s %-10s %-16s %-16s %4d %8s %8s %8s %8.2f\n",
		"", "Total", "", "", result.Totals.Queries,
		fmtTokens(result.Totals.InputTokens), fmtTokens(result.Totals.OutputTokens),
		fmtTokens(result.Totals.CacheTokens), result.Totals.CostUSD)

	printSubscriptionSummary(result.Subscription)
	return nil
}

// runSessionUsageStats uses each request's event timestamp rather than the
// session creation date. The desktop loads this independently when its Today
// Sessions tab opens, so the default dashboard never pays for this aggregation.
func runSessionUsageStats(result statsapp.Result) error {
	results := result.SessionUsage
	ok := statsSection(
		results,
		"Usage by Session",
		result.Window.Label,
		94,
		"No request-level session usage recorded.",
	)
	if !ok {
		return nil
	}

	fmt.Printf("\n  %-3s %-10s %-16s %-16s %4s %8s %8s %8s %8s %5s\n",
		"#", "Session", "Project", "Model", "Req", "In", "Out", "Cache", "Cost($)", "%")
	fmt.Printf("  %s\n", strings.Repeat("-", 94))
	for index, result := range results {
		model := result.Model
		if len(model) > 16 {
			model = model[:16]
		}
		project := result.Project
		if len(project) > 16 {
			project = project[:16]
		}
		cache := result.CacheCreateTokens + result.CacheReadTokens
		fmt.Printf("  %-3d %-10s %-16s %-16s %4d %8s %8s %8s %8.2f %4.0f%%\n",
			index+1, result.ShortID, project, model, result.Requests,
			fmtTokens(result.InputTokens), fmtTokens(result.OutputTokens), fmtTokens(cache),
			result.CostUSD, result.Share*100)
	}
	fmt.Printf("  %s\n", strings.Repeat("-", 94))
	fmt.Printf("  %-3s %-10s %-16s %-16s %4d %8s %8s %8s %8.2f\n",
		"", "Total", "", "", result.Totals.Requests,
		fmtTokens(result.Totals.InputTokens), fmtTokens(result.Totals.OutputTokens),
		fmtTokens(result.Totals.CacheTokens), result.Totals.CostUSD)
	return nil
}

func runWrapped(result statsapp.Result) error {
	wrapped := result.Wrapped
	if wrapped == nil {
		fmt.Println("No activity recorded.")
		return nil
	}

	if jsonOutput {
		output.JSON(wrapped)
		return nil
	}

	fmt.Printf("\n  Wrapped (%s)\n", wrapped.Period)
	fmt.Printf("  %s\n\n", strings.Repeat("─", 40))
	fmt.Printf("  Total Cost        $%.2f\n", wrapped.CostUSD)
	fmt.Printf("  Sessions          %d\n", wrapped.Sessions)
	fmt.Printf("  Queries           %d\n", wrapped.Queries)
	fmt.Printf("  Tool Calls        %d\n", wrapped.ToolCalls)
	fmt.Printf("  Tokens In         %s\n", fmtTokens(wrapped.InputTokens))
	fmt.Printf("  Tokens Out        %s\n", fmtTokens(wrapped.OutputTokens))
	fmt.Printf("  Active Days       %d / %d\n", wrapped.ActiveDays, wrapped.Days)
	fmt.Printf("  Avg Cost/Day      $%.2f\n", wrapped.CostUSD/float64(wrapped.Days))
	fmt.Printf("  Avg Sessions/Day  %.1f\n", float64(wrapped.Sessions)/float64(wrapped.Days))
	fmt.Println()
	fmt.Printf("  Top Model         %s\n", wrapped.TopModel)
	fmt.Printf("  Top Project       %s ($%.2f)\n", wrapped.TopProject, wrapped.TopProjectCost)
	if wrapped.PeakDay != "" {
		fmt.Printf("  Peak Day          %s ($%.2f)\n", wrapped.PeakDay, wrapped.PeakCost)
	}

	printSubscriptionSummary(result.Subscription)
	return nil
}

func runDayStats(result statsapp.Result) error {
	results := result.Periods
	ok := statsSection(results, "Statistics by Day", result.Window.Label, 72, "No activity recorded.")
	if !ok {
		return nil
	}

	fmt.Printf("\n  %-12s %5s %5s %8s %8s %8s  %s\n",
		"Date", "Sess", "Query", "In", "Out", "Cost($)", "")
	sep := strings.Repeat("-", 72)
	fmt.Printf("  %s\n", sep)

	barWidth := 24
	for _, r := range results {
		bar := ""
		if result.Totals.MaxCostUSD > 0 {
			n := int(r.CostUSD / result.Totals.MaxCostUSD * float64(barWidth))
			if n > 0 {
				bar = strings.Repeat("█", n)
			} else if r.CostUSD > 0 {
				bar = "▏"
			}
		}
		fmt.Printf("  %-12s %5d %5d %8s %8s %8.2f  %s\n",
			r.Date, r.Sessions, r.Queries,
			fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			r.CostUSD, bar)
	}
	fmt.Printf("  %s\n", sep)
	fmt.Printf("  %-12s %5d %5d %8s %8s %8.2f\n",
		"Total", result.Totals.Sessions, result.Totals.Queries,
		fmtTokens(result.Totals.InputTokens), fmtTokens(result.Totals.OutputTokens), result.Totals.CostUSD)

	printSubscriptionSummary(result.Subscription)
	return nil
}

func runHourStats(result statsapp.Result) error {
	results := result.Periods
	// This was the one section with no empty-state line, printing a bare column
	// header instead. Hour stats fill every gap in the window, so the case is all
	// but unreachable; it now reads like its siblings if it ever happens.
	ok := statsSection(results, "Statistics by Hour", result.Window.Label, 76, "No activity recorded.")
	if !ok {
		return nil
	}
	fmt.Printf("\n  %-18s %5s %5s %8s %8s %8s\n", "Hour", "Sess", "Query", "In", "Out", "Cost($)")
	for _, r := range results {
		fmt.Printf("  %-18s %5d %5d %8s %8s %8.2f\n",
			r.Date, r.Sessions, r.Queries, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), r.CostUSD)
	}
	fmt.Printf("  %-18s %5d %5d %8s %8s %8.2f\n",
		"Total", result.Totals.Sessions, result.Totals.Queries,
		fmtTokens(result.Totals.InputTokens), fmtTokens(result.Totals.OutputTokens), result.Totals.CostUSD)
	printSubscriptionSummary(result.Subscription)
	return nil
}

// statsSection renders the opening every `atm stats` table shares. Query errors
// have already been classified by the application service, so this edge only
// hands JSON callers the rows untouched or prints the text banner/empty state.
func statsSection[T any](rows []T, title, label string, width int, empty string) bool {
	if jsonOutput {
		output.JSON(rows)
		return false
	}
	fmt.Printf("%s (%s)\n", title, label)
	fmt.Println(strings.Repeat("=", width))
	if len(rows) == 0 {
		fmt.Printf("\n%s\n", empty)
		return false
	}
	return true
}

func printSubscriptionSummary(comparison *statsapp.SubscriptionComparison) {
	if comparison == nil {
		return
	}
	parts := make([]string, 0, len(comparison.Plans))
	for _, plan := range comparison.Plans {
		parts = append(parts, fmt.Sprintf("%s $%.0f", plan.Name, plan.MonthlyUSD))
	}
	fmt.Printf("\n  API equivalent: $%.0f/mo | Subscription: %s = $%.0f/mo | %.1fx value\n",
		comparison.APIEquivalentMonthlyUSD, strings.Join(parts, " + "),
		comparison.SubscriptionMonthlyUSD, comparison.ValueRatio)
}

func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func runModelStats(result statsapp.Result) error {
	results := result.Models
	ok := statsSection(results, "Statistics by Model and Client", result.Window.Label, 60, "No activity recorded.")
	if !ok {
		return nil
	}

	fmt.Printf("\n  %-12s %-30s %8s %10s %10s %8s\n",
		"Client", "Model", "Sessions", "In", "Out", "Cost($)")
	modelSep := output.Dashes(12, 30, 8, 10, 10, 8)
	fmt.Printf("  %-12s %-30s %8s %10s %10s %8s\n", modelSep...)

	for _, r := range results {
		fmt.Printf("  %-12s %-30s %8d %10s %10s %8s\n",
			r.Client, r.Model, r.Sessions, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			fmtCost(r.CostUSD, r.CostEstimated))
	}
	fmt.Printf("  %-12s %-30s %8s %10s %10s %8s\n", modelSep...)
	fmt.Printf("  %-12s %-30s %8d %10s %10s %8s\n",
		"", "Total", result.Totals.Sessions, fmtTokens(result.Totals.InputTokens),
		fmtTokens(result.Totals.OutputTokens), fmtCost(result.Totals.CostUSD, result.Totals.AnyEstimated))
	printEstimatedCostLegend(result.Totals.CostUSD, result.Totals.EstimatedCostUSD, result.Totals.AnyEstimated)

	printSubscriptionSummary(result.Subscription)
	return nil
}

// fmtCost prefixes a cost ATM had to guess the rate for with a tilde, so an
// estimate never reads as a quote. The column stays right-aligned at the same
// width, which is why this returns a string rather than taking a format verb.
func fmtCost(cost float64, estimated bool) string {
	if estimated {
		return fmt.Sprintf("~%.2f", cost)
	}
	return fmt.Sprintf("%.2f", cost)
}

// printEstimatedCostLegend explains the tilde and says how much of the total is
// riding on a guessed rate. The share is the part that matters: "some rows are
// estimated" is easy to skip past, "43% of this total is estimated" is not.
func printEstimatedCostLegend(totalCost, estimatedCost float64, anyEstimated bool) {
	if !anyEstimated {
		return
	}
	share := 0.0
	if totalCost > 0 {
		share = estimatedCost / totalCost * 100
	}
	fmt.Printf("\n  ~ = estimated rate: $%.2f (%.0f%%) of this total comes from models with no exact rate.\n", estimatedCost, share)
	fmt.Println("      Run `atm doctor` for the models, or set rates in ~/.atm/pricing.json.")
}

func runModelDayStats(result statsapp.Result) error {
	results := result.ModelPeriods
	ok := statsSection(results, "Statistics by Model, Client, and Day", result.Window.Label, 84, "No activity recorded.")
	if !ok {
		return nil
	}

	fmt.Printf("\n  %-16s %-10s %-28s %8s %10s %10s %8s\n",
		"Date", "Client", "Model", "Sessions", "In", "Out", "Cost($)")
	for _, r := range results {
		fmt.Printf("  %-16s %-10s %-28s %8d %10s %10s %8s\n",
			r.Date, r.Client, r.Model, r.Sessions, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			fmtCost(r.CostUSD, r.CostEstimated))
	}
	printEstimatedCostLegend(result.Totals.CostUSD, result.Totals.EstimatedCostUSD, result.Totals.AnyEstimated)
	return nil
}

func runModelHourStats(result statsapp.Result) error {
	results := result.ModelPeriods
	ok := statsSection(results, "Statistics by Model and Hour", result.Window.Label, 90, "No activity recorded.")
	if !ok {
		return nil
	}
	fmt.Printf("\n  %-18s %-28s %8s %10s %10s %8s\n",
		"Hour", "Model", "Sessions", "In", "Out", "Cost($)")
	for _, r := range results {
		fmt.Printf("  %-18s %-28s %8d %10s %10s %8s\n",
			r.Date, r.Model, r.Sessions, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			fmtCost(r.CostUSD, r.CostEstimated))
	}
	printEstimatedCostLegend(result.Totals.CostUSD, result.Totals.EstimatedCostUSD, result.Totals.AnyEstimated)
	return nil
}
