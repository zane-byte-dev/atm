package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(quotaCmd)
}

var quotaCmd = &cobra.Command{
	Use:   "quota",
	Short: "Show AI agent rate limit / quota status",
	RunE:  runQuota,
}

func formatCountdown(epoch int64) string {
	d := time.Until(time.Unix(epoch, 0))
	if d <= 0 {
		return "resetting"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 24 {
		return fmt.Sprintf("%dd%dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatResetTime(epoch int64) string {
	return time.Unix(epoch, 0).In(time.Local).Format("01-02 15:04")
}

// quotaWindowJSON and printQuotaWindow render one rate-limit window. Codex
// reports a primary and a secondary one, and both were spelled out twice —
// once per window, once per output format. nil means the window is absent, so
// the caller omits the key rather than emitting an empty object.
func quotaWindowJSON(limit *parser.QuotaLimit, now time.Time, trends quotaTrendLookup) map[string]any {
	if limit == nil {
		return nil
	}
	window := map[string]any{
		"used_percent":   limit.UsedPercent,
		"window_minutes": limit.WindowMinutes,
		"resets_at":      limit.ResetsAt,
	}
	if quotaResetPending(limit, now) {
		window["resets_in"] = formatCountdown(limit.ResetsAt)
	}
	// Absent when history is too thin to divide, so a consumer can tell "not
	// enough data yet" from "not moving".
	if trend, ok := trends.lookup(limit.WindowMinutes); ok {
		window["trend"] = trend
	}
	return window
}

// quotaTrendLookup holds one agent's computed trends by window. It is a type
// rather than a bare map so the nil case — no database, or history disabled —
// reads as "no trend" at every call site instead of needing a guard at each one.
type quotaTrendLookup map[int]store.QuotaTrend

func (l quotaTrendLookup) lookup(windowMinutes int) (store.QuotaTrend, bool) {
	trend, ok := l[windowMinutes]
	return trend, ok
}

func printQuotaWindow(limit *parser.QuotaLimit, now time.Time, trends quotaTrendLookup) {
	if limit == nil {
		return
	}
	// A window whose reset time has passed has already refilled, so report it as
	// empty rather than showing the stale percentage the log still carries.
	pct := limit.UsedPercent
	if limit.ResetsAt > 0 && time.Unix(limit.ResetsAt, 0).Before(now) {
		pct = 0
	}
	resetStr := ""
	if quotaResetPending(limit, now) {
		resetStr = fmt.Sprintf("reset %s  (%s)", formatResetTime(limit.ResetsAt), formatCountdown(limit.ResetsAt))
	}
	fmt.Printf("  %-6s %5.1f%% used   %s\n", formatQuotaWindow(limit.WindowMinutes), pct, resetStr)
	if trend, ok := trends.lookup(limit.WindowMinutes); ok {
		fmt.Printf("  %-6s %s\n", "", formatQuotaTrend(trend, now))
	}
}

func quotaResetPending(limit *parser.QuotaLimit, now time.Time) bool {
	return limit.ResetsAt > 0 && time.Unix(limit.ResetsAt, 0).After(now)
}

func formatQuotaWindow(minutes int) string {
	if minutes <= 0 {
		return "Window"
	}
	if minutes%(7*24*60) == 0 {
		return fmt.Sprintf("%dw", minutes/(7*24*60))
	}
	if minutes%(24*60) == 0 {
		return fmt.Sprintf("%dd", minutes/(24*60))
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
}

func quotaAgentJSON(q *parser.QuotaInfo, now time.Time, trends quotaTrendLookup) map[string]any {
	if q == nil {
		return nil
	}
	out := map[string]any{}
	if q.Plan != "" {
		out["plan"] = q.Plan
	}
	if window := quotaWindowJSON(q.Primary, now, trends); window != nil {
		out["primary"] = window
	}
	if window := quotaWindowJSON(q.Secondary, now, trends); window != nil {
		out["secondary"] = window
	}
	if len(out) == 0 {
		return nil
	}
	// source/products only decorate an agent that already reported a window,
	// so older consumers keep seeing the exact shape they knew.
	if q.Source != "" {
		out["source"] = q.Source
	}
	if len(q.Products) > 0 {
		products := make([]map[string]any, 0, len(q.Products))
		for _, p := range q.Products {
			products = append(products, map[string]any{"product": p.Name, "used_percent": p.UsedPercent})
		}
		out["products"] = products
	}
	return out
}

func printQuotaAgent(name string, q *parser.QuotaInfo, now time.Time, trends quotaTrendLookup) {
	if q == nil {
		return
	}
	fmt.Printf("%s Quota\n", name)
	fmt.Println(strings.Repeat("─", 46))
	printQuotaWindow(q.Primary, now, trends)
	printQuotaWindow(q.Secondary, now, trends)
	for _, p := range q.Products {
		fmt.Printf("    %-12s %5.1f%% of pool\n", p.Name, p.UsedPercent)
	}
	if q.Plan != "" {
		fmt.Printf("  Plan: %s\n", q.Plan)
	}
	if q.Source != "" {
		fmt.Printf("  Source: %s\n", q.Source)
	}
	fmt.Println()
}

// Agents that expose a quota window. Keep this list in sync with the parsers
// wired in runQuota.
var quotaAgents = []string{"codex", "grokbuild"}

// recordQuotaSamples appends the current rate-limit readings to history. It reads
// only local sources — never the opt-in Grok live billing endpoint — because sync
// runs unattended on a timer and must not make network calls the user did not ask
// for on that path.
func recordQuotaSamples(db *sql.DB, now time.Time) error {
	var samples []store.QuotaSample
	add := func(agent string, q *parser.QuotaInfo) {
		if q == nil {
			return
		}
		for _, limit := range []*parser.QuotaLimit{q.Primary, q.Secondary} {
			if limit == nil || limit.WindowMinutes <= 0 {
				continue
			}
			// A window whose reset has passed has already refilled. Storing the
			// stale percentage would look like usage that never drained, so record
			// what is true now: empty, in the next period.
			percent := limit.UsedPercent
			if limit.ResetsAt > 0 && time.Unix(limit.ResetsAt, 0).Before(now) {
				percent = 0
			}
			samples = append(samples, store.QuotaSample{
				Agent: agent, WindowMinutes: limit.WindowMinutes,
				UsedPercent: percent, ResetsAt: limit.ResetsAt, TS: now.Unix(),
			})
		}
	}
	add("codex", parser.CodexQuota())
	add("grokbuild", parser.GrokQuota())
	return store.RecordQuotaSamples(db, samples, now)
}

// loadQuotaTrends reads the trend for each window the agent currently reports.
// Every failure degrades to no trend rather than an error: `atm quota` answers
// from live agent logs and worked before any history existed, so a missing
// database, a database this build cannot open read-only, or a run before the
// first two samples all have to leave the plain percentage intact.
func loadQuotaTrends(agent string, q *parser.QuotaInfo, now time.Time) quotaTrendLookup {
	if q == nil {
		return nil
	}
	windows := make([]int, 0, 2)
	for _, limit := range []*parser.QuotaLimit{q.Primary, q.Secondary} {
		if limit != nil && limit.WindowMinutes > 0 {
			windows = append(windows, limit.WindowMinutes)
		}
	}
	if len(windows) == 0 {
		return nil
	}
	if _, err := os.Stat(config.AtmDB); err != nil {
		return nil
	}
	trends := quotaTrendLookup{}
	// Read-only: showing a quota reading must never create or migrate a database.
	if err := withDB(true, func(db *sql.DB) error {
		for _, window := range windows {
			trend, ok, err := store.QuotaTrendFor(db, agent, window, now)
			if err != nil {
				return err
			}
			if ok {
				trends[window] = trend
			}
		}
		return nil
	}); err != nil {
		return nil
	}
	return trends
}

// formatQuotaTrend renders a rate as a direction a person can act on. Below this
// threshold the number is sampling jitter, not movement, and calling it "rising"
// would make a resting quota look like a problem.
const quotaTrendFlatPercentPerHour = 0.5

func formatQuotaTrend(trend store.QuotaTrend, now time.Time) string {
	if trend.PercentPerHour < quotaTrendFlatPercentPerHour &&
		trend.PercentPerHour > -quotaTrendFlatPercentPerHour {
		return fmt.Sprintf("→ flat over %s", formatQuotaSpan(trend.SpanMinutes))
	}
	arrow := "↑"
	if trend.PercentPerHour < 0 {
		arrow = "↓"
	}
	text := fmt.Sprintf("%s %+.1f%%/h over %s", arrow, trend.PercentPerHour, formatQuotaSpan(trend.SpanMinutes))
	if trend.FullBeforeReset && trend.FullAt > 0 {
		text += fmt.Sprintf("  ⚠ full in %s, before reset", formatCountdown(trend.FullAt))
	}
	return text
}

func formatQuotaSpan(minutes int) string {
	if minutes >= 60 {
		return fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}

func supportsQuota(agent string) bool {
	for _, a := range quotaAgents {
		if a == agent {
			return true
		}
	}
	return false
}

func runQuota(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	if agent != "" && !supportsQuota(agent) {
		fmt.Printf("Quota is currently only available for %s.\n", strings.Join(quotaAgents, " and "))
		return nil
	}

	now := time.Now()
	want := func(name string) bool { return agent == "" || agent == name }

	var codex, grok *parser.QuotaInfo
	if want("codex") {
		codex = parser.CodexQuota()
	}
	if want("grokbuild") {
		// Live is opt-in (grok_live_quota / ATM_GROK_LIVE_QUOTA); default stays
		// a local log read with no network traffic.
		grok = parser.GrokQuotaAuto(config.GrokLiveQuota)
	}

	codexTrends := loadQuotaTrends("codex", codex, now)
	grokTrends := loadQuotaTrends("grokbuild", grok, now)

	if jsonOutput {
		out := map[string]any{}
		if want("codex") {
			out["codex"] = quotaAgentJSON(codex, now, codexTrends)
		}
		if want("grokbuild") {
			out["grokbuild"] = quotaAgentJSON(grok, now, grokTrends)
		}
		output.JSON(out)
		return nil
	}

	printed := false
	if want("codex") && codex != nil {
		printQuotaAgent("Codex", codex, now, codexTrends)
		printed = true
	}
	if want("grokbuild") && grok != nil {
		printQuotaAgent("Grok Build", grok, now, grokTrends)
		printed = true
	}
	if !printed {
		if agent != "" {
			fmt.Printf("No quota data found for %s.\n", agent)
		} else {
			fmt.Println("No quota data found. (Codex rate_limits or Grok billing log not present)")
		}
	}
	return nil
}
