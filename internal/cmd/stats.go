package cmd

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

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
	validGroups := map[string]bool{
		"": true, "model": true, "model-day": true, "model-hour": true,
		"skill": true, "session": true, "session-usage": true, "request": true, "speed": true,
		"day": true, "hour": true, "wrapped": true,
	}
	if !validGroups[statsByFlag] {
		return fmt.Errorf("unknown stats group %q (use model, model-day, model-hour, skill, session, session-usage, request, speed, day, hour, or wrapped)", statsByFlag)
	}

	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	days := statsDaysFlag
	if days < 1 {
		days = 1
	}
	// --range names a calendar window; --days is the rolling one it has always
	// been. Mutually exclusive, because a caller who passes both has no way to
	// know which they got.
	var namedRange config.MetricsRange
	if statsRangeFlag != "" {
		namedRange, err = config.ParseMetricsRange(statsRangeFlag)
		if err != nil {
			return err
		}
	}

	return withDB(true, func(db *sql.DB) error {
		now := time.Now().In(config.Loc)
		start := startOfDayWindow(now, days)
		end := now
		label := dayRangeLabel(days)
		if namedRange != "" {
			// end is the window's exclusive upper bound, so a period that has
			// already closed — yesterday, last week — stops where it ended instead
			// of running to now and absorbing everything since.
			start, end = namedRange.Bounds(now)
			days = namedRange.Days(now)
			label = string(namedRange)
		}
		startTS, endTS := start.Unix(), end.Unix()

		if statsByFlag == "model" {
			return runModelStats(db, startTS, endTS, agent, label, days)
		}
		if statsByFlag == "model-day" {
			return runModelDayStats(db, startTS, endTS, agent, label)
		}
		if statsByFlag == "model-hour" {
			return runModelHourStats(db, startTS, endTS, agent, label)
		}
		if statsByFlag == "skill" {
			return runSkillStats(db, startTS, endTS, agent, label)
		}
		if statsByFlag == "session" {
			return runSessionStats(db, startTS, endTS, agent, label, days)
		}
		if statsByFlag == "session-usage" {
			return runSessionUsageStats(db, startTS, endTS, agent, label)
		}
		if statsByFlag == "request" {
			return runRequestStats(db, startTS, endTS, agent, statsSessionFlag, label)
		}
		if statsByFlag == "speed" {
			return runSpeedStats(db, startTS, endTS, agent, label)
		}
		if statsByFlag == "day" {
			return runDayStats(db, startTS, endTS, agent, label, days)
		}
		if statsByFlag == "hour" {
			return runHourStats(db, startTS, endTS, agent, label, days)
		}
		if statsByFlag == "wrapped" {
			return runWrapped(db, startTS, endTS, agent, label, days)
		}

		results, err := store.GetStats(db, startTS, endTS, agent)
		ok, sectionErr := statsSection(results, err, "Statistics", label, 60, "No activity recorded.")
		if !ok {
			return sectionErr
		}

		fmt.Printf("\n  %-20s %-10s %8s %8s %8s %10s %10s %8s\n",
			"Project", "Agent", "Sessions", "Queries", "Tools", "In", "Out", "Cost($)")
		statsSep := output.Dashes(20, 10, 8, 8, 8, 10, 10, 8)
		fmt.Printf("  %-20s %-10s %8s %8s %8s %10s %10s %8s\n", statsSep...)

		var totalSessions, totalQueries, totalTools int
		var totalIn, totalOut int64
		var totalCost float64
		for _, r := range results {
			fmt.Printf("  %-20s %-10s %8d %8d %8d %10s %10s %8.2f\n",
				r.Project, r.Agent, r.Sessions, r.Queries, r.ToolCalls,
				fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), r.CostUSD)
			totalSessions += r.Sessions
			totalQueries += r.Queries
			totalTools += r.ToolCalls
			totalIn += r.InputTokens
			totalOut += r.OutputTokens
			totalCost += r.CostUSD
		}
		fmt.Printf("  %-20s %-10s %8s %8s %8s %10s %10s %8s\n", statsSep...)
		fmt.Printf("  %-20s %-10s %8d %8d %8d %10s %10s %8.2f\n",
			"Total", "", totalSessions, totalQueries, totalTools, fmtTokens(totalIn), fmtTokens(totalOut), totalCost)

		printSubscriptionSummary(totalCost, days)
		return nil
	})
}

func runSkillStats(db *sql.DB, startTS, endTS int64, agent, label string) error {
	results, err := store.GetSkillStats(db, startTS, endTS, agent)
	ok, sectionErr := statsSection(results, err, "Statistics by Skill", label, 60, "No skill activity recorded.")
	if !ok {
		return sectionErr
	}
	fmt.Printf("\n  %-32s %8s %10s %8s\n", "Skill", "Calls", "Sessions", "Agents")
	for _, result := range results {
		fmt.Printf("  %-32s %8d %10d %8d\n", result.Skill, result.Calls, result.Sessions, result.Agents)
	}
	return nil
}

func runRequestStats(db *sql.DB, startTS, endTS int64, agent, session, label string) error {
	results, err := store.GetRequestStats(db, startTS, endTS, agent, session)
	ok, sectionErr := statsSection(results, err, "Statistics by Request", label, 100, "No request-level usage recorded.")
	if !ok {
		return sectionErr
	}
	// Req shows model-call multiplicity (×N when a row aggregates several calls,
	// as Grok turn_completed does). Tokens/cost on the row are the full total.
	fmt.Printf("\n  %-16s %-8s %-12s %-24s %5s %8s %8s %8s %8s\n", "Time", "Agent", "Session", "Model", "Req", "In", "Out", "Cache", "Cost($)")
	var totalCalls int
	for _, r := range results {
		model := r.Model
		if len(model) > 24 {
			model = model[:24]
		}
		calls := r.RequestCount
		if calls <= 0 {
			calls = 1
		}
		totalCalls += calls
		fmt.Printf("  %-16s %-8s %-12s %-24s %5s %8s %8s %8s %8.4f\n",
			time.Unix(r.TS, 0).In(config.Loc).Format("01-02 15:04:05"),
			r.Agent, r.SessionID, model, fmtRequestCount(calls),
			fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), fmtTokens(r.CacheTokens), r.CostUSD)
	}
	if len(results) > 0 && totalCalls != len(results) {
		fmt.Printf("\n  %d rows · %d model calls\n", len(results), totalCalls)
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
func runSpeedStats(db *sql.DB, startTS, endTS int64, agent, label string) error {
	report, err := store.GetSpeedStats(db, startTS, endTS, agent)
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	if jsonOutput {
		output.JSON(report)
		return nil
	}
	fmt.Printf("Statistics by Speed (%s)\n", label)
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

func runSessionStats(db *sql.DB, startTS, endTS int64, agent, label string, days int) error {
	results, err := store.GetSessionStats(db, startTS, endTS, agent)
	ok, sectionErr := statsSection(results, err, "Statistics by Session", label, 80, "No activity recorded.")
	if !ok {
		return sectionErr
	}

	var totalIn, totalOut, totalCache int64
	var totalCost float64
	for _, r := range results {
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalCache += r.CacheTokens
		totalCost += r.CostUSD
	}

	fmt.Printf("\n  %-3s %-10s %-16s %-16s %4s %8s %8s %8s %8s %5s\n",
		"#", "Session", "Project", "Model", "Req", "In", "Out", "Cache", "Cost($)", "%")
	sep := strings.Repeat("-", 88)
	fmt.Printf("  %s\n", sep)

	totalTokens := totalIn + totalOut + totalCache
	var totalReqs int
	for i, r := range results {
		model := r.Model
		if len(model) > 16 {
			model = model[:16]
		}
		project := r.Project
		if len(project) > 16 {
			project = project[:16]
		}
		pct := 0.0
		if totalTokens > 0 {
			pct = float64(r.InputTokens+r.OutputTokens+r.CacheTokens) / float64(totalTokens) * 100
		}
		totalReqs += r.Queries
		fmt.Printf("  %-3d %-10s %-16s %-16s %4d %8s %8s %8s %8.2f %4.0f%%\n",
			i+1, r.ShortID, project, model, r.Queries,
			fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), fmtTokens(r.CacheTokens),
			r.CostUSD, pct)
	}
	fmt.Printf("  %s\n", sep)
	fmt.Printf("  %-3s %-10s %-16s %-16s %4d %8s %8s %8s %8.2f\n",
		"", "Total", "", "", totalReqs,
		fmtTokens(totalIn), fmtTokens(totalOut), fmtTokens(totalCache), totalCost)

	printSubscriptionSummary(totalCost, days)
	return nil
}

// runSessionUsageStats uses each request's event timestamp rather than the
// session creation date. The desktop loads this independently when its Today
// Sessions tab opens, so the default dashboard never pays for this aggregation.
func runSessionUsageStats(db *sql.DB, startTS, endTS int64, agent, label string) error {
	results, err := store.GetSessionUsageStats(db, startTS, endTS, agent)
	ok, sectionErr := statsSection(
		results,
		err,
		"Usage by Session",
		label,
		94,
		"No request-level session usage recorded.",
	)
	if !ok {
		return sectionErr
	}

	fmt.Printf("\n  %-3s %-10s %-16s %-16s %4s %8s %8s %8s %8s %5s\n",
		"#", "Session", "Project", "Model", "Req", "In", "Out", "Cache", "Cost($)", "%")
	fmt.Printf("  %s\n", strings.Repeat("-", 94))
	var totalRequests int
	var totalInput, totalOutput, totalCache int64
	var totalCost float64
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
		totalRequests += result.Requests
		totalInput += result.InputTokens
		totalOutput += result.OutputTokens
		totalCache += cache
		totalCost += result.CostUSD
		fmt.Printf("  %-3d %-10s %-16s %-16s %4d %8s %8s %8s %8.2f %4.0f%%\n",
			index+1, result.ShortID, project, model, result.Requests,
			fmtTokens(result.InputTokens), fmtTokens(result.OutputTokens), fmtTokens(cache),
			result.CostUSD, result.Share*100)
	}
	fmt.Printf("  %s\n", strings.Repeat("-", 94))
	fmt.Printf("  %-3s %-10s %-16s %-16s %4d %8s %8s %8s %8.2f\n",
		"", "Total", "", "", totalRequests,
		fmtTokens(totalInput), fmtTokens(totalOutput), fmtTokens(totalCache), totalCost)
	return nil
}

func runWrapped(db *sql.DB, startTS, endTS int64, agent, label string, days int) error {
	stats, err := store.GetStats(db, startTS, endTS, agent)
	if err != nil {
		return err
	}
	modelStats, err := store.GetModelStats(db, startTS, endTS, agent)
	if err != nil {
		return err
	}
	dayStats, err := store.GetDayStats(db, startTS, endTS, agent, config.Loc)
	if err != nil {
		return err
	}

	if len(stats) == 0 {
		fmt.Println("No activity recorded.")
		return nil
	}

	var totalSessions, totalQueries, totalTools int
	var totalIn, totalOut int64
	var totalCost float64
	projectCost := map[string]float64{}
	for _, r := range stats {
		totalSessions += r.Sessions
		totalQueries += r.Queries
		totalTools += r.ToolCalls
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalCost += r.CostUSD
		projectCost[r.Project] += r.CostUSD
	}

	topModel := ""
	if len(modelStats) > 0 {
		topModel = modelStats[0].Model
		if modelStats[0].Client != "" {
			topModel += " · " + modelStats[0].Client
		}
	}

	topProject := ""
	var topProjectCost float64
	for p, c := range projectCost {
		if c > topProjectCost {
			topProject = p
			topProjectCost = c
		}
	}

	var activeDays, peakIdx int
	var peakCost float64
	for i, d := range dayStats {
		if d.Sessions > 0 {
			activeDays++
		}
		if d.CostUSD > peakCost {
			peakCost = d.CostUSD
			peakIdx = i
		}
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"period":        label,
			"days":          days,
			"active_days":   activeDays,
			"sessions":      totalSessions,
			"queries":       totalQueries,
			"tool_calls":    totalTools,
			"input_tokens":  totalIn,
			"output_tokens": totalOut,
			"cost_usd":      totalCost,
			"top_model":     topModel,
			"top_project":   topProject,
			"peak_day":      dayStats[peakIdx].Date,
			"peak_cost":     peakCost,
		})
		return nil
	}

	fmt.Printf("\n  Wrapped (%s)\n", label)
	fmt.Printf("  %s\n\n", strings.Repeat("─", 40))
	fmt.Printf("  Total Cost        $%.2f\n", totalCost)
	fmt.Printf("  Sessions          %d\n", totalSessions)
	fmt.Printf("  Queries           %d\n", totalQueries)
	fmt.Printf("  Tool Calls        %d\n", totalTools)
	fmt.Printf("  Tokens In         %s\n", fmtTokens(totalIn))
	fmt.Printf("  Tokens Out        %s\n", fmtTokens(totalOut))
	fmt.Printf("  Active Days       %d / %d\n", activeDays, days)
	fmt.Printf("  Avg Cost/Day      $%.2f\n", totalCost/float64(days))
	fmt.Printf("  Avg Sessions/Day  %.1f\n", float64(totalSessions)/float64(days))
	fmt.Println()
	fmt.Printf("  Top Model         %s\n", topModel)
	fmt.Printf("  Top Project       %s ($%.2f)\n", topProject, topProjectCost)
	if len(dayStats) > 0 {
		fmt.Printf("  Peak Day          %s ($%.2f)\n", dayStats[peakIdx].Date, peakCost)
	}

	printSubscriptionSummary(totalCost, days)
	return nil
}

func runDayStats(db *sql.DB, startTS, endTS int64, agent, label string, days int) error {
	results, err := store.GetDayStats(db, startTS, endTS, agent, config.Loc)
	ok, sectionErr := statsSection(results, err, "Statistics by Day", label, 72, "No activity recorded.")
	if !ok {
		return sectionErr
	}

	var maxCost float64
	var totalSessions, totalQueries int
	var totalIn, totalOut int64
	var totalCost float64
	for _, r := range results {
		if r.CostUSD > maxCost {
			maxCost = r.CostUSD
		}
		totalSessions += r.Sessions
		totalQueries += r.Queries
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalCost += r.CostUSD
	}

	fmt.Printf("\n  %-12s %5s %5s %8s %8s %8s  %s\n",
		"Date", "Sess", "Query", "In", "Out", "Cost($)", "")
	sep := strings.Repeat("-", 72)
	fmt.Printf("  %s\n", sep)

	barWidth := 24
	for _, r := range results {
		bar := ""
		if maxCost > 0 {
			n := int(r.CostUSD / maxCost * float64(barWidth))
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
		"Total", totalSessions, totalQueries,
		fmtTokens(totalIn), fmtTokens(totalOut), totalCost)

	printSubscriptionSummary(totalCost, days)
	return nil
}

func runHourStats(db *sql.DB, startTS, endTS int64, agent, label string, days int) error {
	results, err := store.GetHourStats(db, startTS, endTS, agent, config.Loc)
	// This was the one section with no empty-state line, printing a bare column
	// header instead. Hour stats fill every gap in the window, so the case is all
	// but unreachable; it now reads like its siblings if it ever happens.
	ok, sectionErr := statsSection(results, err, "Statistics by Hour", label, 76, "No activity recorded.")
	if !ok {
		return sectionErr
	}
	var totalSessions, totalQueries int
	var totalIn, totalOut int64
	var totalCost float64
	fmt.Printf("\n  %-18s %5s %5s %8s %8s %8s\n", "Hour", "Sess", "Query", "In", "Out", "Cost($)")
	for _, r := range results {
		fmt.Printf("  %-18s %5d %5d %8s %8s %8.2f\n",
			r.Date, r.Sessions, r.Queries, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens), r.CostUSD)
		totalSessions += r.Sessions
		totalQueries += r.Queries
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalCost += r.CostUSD
	}
	fmt.Printf("  %-18s %5d %5d %8s %8s %8.2f\n",
		"Total", totalSessions, totalQueries, fmtTokens(totalIn), fmtTokens(totalOut), totalCost)
	printSubscriptionSummary(totalCost, days)
	return nil
}

// statsSection prints the opening every `atm stats` table shares: fail on the
// query error, hand JSON callers the rows untouched, then the banner — or say
// nothing was recorded. It reports false when there is no table left to print,
// so a caller returns the error it hands back, which is nil for the JSON and
// empty cases.
func statsSection[T any](rows []T, queryErr error, title, label string, width int, empty string) (bool, error) {
	if queryErr != nil {
		return false, fmt.Errorf("query error: %w", queryErr)
	}
	if jsonOutput {
		output.JSON(rows)
		return false, nil
	}
	fmt.Printf("%s (%s)\n", title, label)
	fmt.Println(strings.Repeat("=", width))
	if len(rows) == 0 {
		fmt.Printf("\n%s\n", empty)
		return false, nil
	}
	return true, nil
}

func printSubscriptionSummary(totalCost float64, days int) {
	if len(config.Subscriptions) == 0 || totalCost == 0 {
		return
	}
	// Sorted because Subscriptions is a map: unsorted, the same data reorders
	// between runs and anything diffing or grepping this line sees a change that
	// is not one.
	var subTotal float64
	names := make([]string, 0, len(config.Subscriptions))
	for name := range config.Subscriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		subTotal += config.Subscriptions[name]
		parts = append(parts, fmt.Sprintf("%s $%.0f", name, config.Subscriptions[name]))
	}
	if subTotal == 0 {
		return
	}
	monthlyAPI := totalCost / float64(days) * 30
	ratio := monthlyAPI / subTotal
	fmt.Printf("\n  API equivalent: $%.0f/mo | Subscription: %s = $%.0f/mo | %.1fx value\n",
		monthlyAPI, strings.Join(parts, " + "), subTotal, ratio)
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

func runModelStats(db *sql.DB, startTS, endTS int64, agent, label string, days int) error {
	results, err := store.GetModelStats(db, startTS, endTS, agent)
	ok, sectionErr := statsSection(results, err, "Statistics by Model and Client", label, 60, "No activity recorded.")
	if !ok {
		return sectionErr
	}

	fmt.Printf("\n  %-12s %-30s %8s %10s %10s %8s\n",
		"Client", "Model", "Sessions", "In", "Out", "Cost($)")
	modelSep := output.Dashes(12, 30, 8, 10, 10, 8)
	fmt.Printf("  %-12s %-30s %8s %10s %10s %8s\n", modelSep...)

	var totalSessions int
	var totalIn, totalOut int64
	var totalCost, estimatedCost float64
	anyEstimated := false
	for _, r := range results {
		fmt.Printf("  %-12s %-30s %8d %10s %10s %8s\n",
			r.Client, r.Model, r.Sessions, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			fmtCost(r.CostUSD, r.CostEstimated))
		totalSessions += r.Sessions
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalCost += r.CostUSD
		if r.CostEstimated {
			anyEstimated = true
			estimatedCost += r.CostUSD
		}
	}
	fmt.Printf("  %-12s %-30s %8s %10s %10s %8s\n", modelSep...)
	fmt.Printf("  %-12s %-30s %8d %10s %10s %8s\n",
		"", "Total", totalSessions, fmtTokens(totalIn), fmtTokens(totalOut), fmtCost(totalCost, anyEstimated))
	printEstimatedCostLegend(totalCost, estimatedCost, anyEstimated)

	printSubscriptionSummary(totalCost, days)
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

func runModelDayStats(db *sql.DB, startTS, endTS int64, agent, label string) error {
	results, err := store.GetModelDayStats(db, startTS, endTS, agent, config.Loc)
	ok, sectionErr := statsSection(results, err, "Statistics by Model, Client, and Day", label, 84, "No activity recorded.")
	if !ok {
		return sectionErr
	}

	fmt.Printf("\n  %-16s %-10s %-28s %8s %10s %10s %8s\n",
		"Date", "Client", "Model", "Sessions", "In", "Out", "Cost($)")
	var totalCost, estimatedCost float64
	anyEstimated := false
	for _, r := range results {
		fmt.Printf("  %-16s %-10s %-28s %8d %10s %10s %8s\n",
			r.Date, r.Client, r.Model, r.Sessions, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			fmtCost(r.CostUSD, r.CostEstimated))
		totalCost += r.CostUSD
		if r.CostEstimated {
			anyEstimated = true
			estimatedCost += r.CostUSD
		}
	}
	printEstimatedCostLegend(totalCost, estimatedCost, anyEstimated)
	return nil
}

func runModelHourStats(db *sql.DB, startTS, endTS int64, agent, label string) error {
	results, err := store.GetModelHourStats(db, startTS, endTS, agent, config.Loc)
	ok, sectionErr := statsSection(results, err, "Statistics by Model and Hour", label, 90, "No activity recorded.")
	if !ok {
		return sectionErr
	}
	fmt.Printf("\n  %-18s %-28s %8s %10s %10s %8s\n",
		"Hour", "Model", "Sessions", "In", "Out", "Cost($)")
	var totalCost, estimatedCost float64
	anyEstimated := false
	for _, r := range results {
		fmt.Printf("  %-18s %-28s %8d %10s %10s %8s\n",
			r.Date, r.Model, r.Sessions, fmtTokens(r.InputTokens), fmtTokens(r.OutputTokens),
			fmtCost(r.CostUSD, r.CostEstimated))
		totalCost += r.CostUSD
		if r.CostEstimated {
			anyEstimated = true
			estimatedCost += r.CostUSD
		}
	}
	printEstimatedCostLegend(totalCost, estimatedCost, anyEstimated)
	return nil
}
