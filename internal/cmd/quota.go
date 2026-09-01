package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	quotaapp "github.com/zane-byte-dev/atm/internal/quota"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(quotaCmd)
}

var quotaCmd = &cobra.Command{
	Use:   "quota",
	Short: "Show AI agent rate limit / quota status",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		if args[0] == "disable" || args[0] == "enable" {
			value := "false"
			if args[0] == "enable" {
				value = "true"
			}
			return fmt.Errorf("quota has no %q action; run `atm config set grok_live_quota %s`", args[0], value)
		}
		return cobra.NoArgs(cmd, args)
	},
	Example: `  atm quota
  atm quota --agent codex --json

Quota is read-only. To disable live Grok quota fetching, run:
  atm config set grok_live_quota false`,
	RunE: runQuota,
}

func runQuota(cmd *cobra.Command, args []string) error {
	snapshot, err := quotaapp.Default.Snapshot(commandContext(cmd), cliApplicationCall("quota", ""), quotaapp.Input{
		Agent: agentFlag, Live: config.GrokLiveQuota,
	})
	if err != nil {
		return err
	}
	// Provider failures are warnings, not errors: the agent-log readings in this
	// same snapshot are still worth printing.
	for _, warning := range snapshot.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", warning)
	}

	if jsonOutput {
		output.JSON(snapshot.Agents)
		return nil
	}

	now := time.Now()
	printed := false
	for _, agent := range snapshot.Order {
		printQuotaAgent(snapshot.Agents[agent], now)
		printed = true
	}
	if printQuotaProviderCards(snapshot) {
		printed = true
	}
	if !printed {
		if agentFlag != "" {
			fmt.Printf("No quota data found for %s.\n", agentFlag)
		} else {
			fmt.Println("No quota data found. (Codex rate_limits, Grok billing log or a running Antigravity not present)")
		}
	}
	return nil
}

func printQuotaAgent(agent *quotaapp.AgentQuota, now time.Time) {
	if agent == nil {
		return
	}
	fmt.Printf("%s Quota\n", agent.DisplayName)
	fmt.Println(strings.Repeat("─", 46))
	for _, window := range agent.Windows() {
		printQuotaWindow(window, now)
	}
	for _, product := range agent.Products {
		fmt.Printf("    %-12s %5.1f%% of pool\n", product.Name, product.UsedPercent)
	}
	if agent.Plan != "" {
		fmt.Printf("  Plan: %s\n", agent.Plan)
	}
	if agent.Source != "" {
		fmt.Printf("  Source: %s\n", agent.Source)
	}
	fmt.Println()
}

func printQuotaWindow(window *quotaapp.Window, now time.Time) {
	resetStr := ""
	if window.ResetPending {
		resetStr = fmt.Sprintf("reset %s  (%s)", formatResetTime(window.ResetsAt), window.ResetsIn)
	}
	fmt.Printf("  %-6s %5.1f%% used   %s\n",
		formatQuotaWindow(window.WindowMinutes), window.DisplayPercent, resetStr)
	if window.Trend != nil {
		fmt.Printf("  %-6s %s\n", "", formatQuotaTrend(*window.Trend, now))
	}
}

// printQuotaProviderCards renders the agents ATM only sees through a provider,
// and the extra cards on agents it also reads directly. Sorted by agent so the
// grid does not reshuffle between runs.
func printQuotaProviderCards(snapshot quotaapp.Snapshot) bool {
	agents := make([]string, 0, len(snapshot.Agents))
	for agent, entry := range snapshot.Agents {
		if entry != nil && len(entry.ProviderCards) > 0 {
			agents = append(agents, agent)
		}
	}
	sort.Strings(agents)
	printed := false
	for _, agent := range agents {
		for _, card := range snapshot.Agents[agent].ProviderCards {
			fmt.Printf("%s / %s Quota\n", titleWord(agent), titleWord(card.Provider))
			fmt.Println(strings.Repeat("─", 46))
			title := card.Title
			if card.Period != "" {
				title += " · " + card.Period
			}
			fmt.Printf("  %s\n", title)
			if card.Unavailable {
				fmt.Printf("  %s\n", quotaProviderUnavailableText(card))
			}
			for _, metric := range card.Metrics {
				unit := metric.Unit
				if metric.Currency != "" {
					unit = " " + metric.Currency
				}
				fmt.Printf("  %-12s %.*f / %.*f%s  (%5.1f%%)\n",
					metric.Label, metric.Precision, metric.Used,
					metric.Precision, metric.Limit, unit, metric.UsedPercent)
			}
			if card.ObservedAt != "" || card.Source != "" {
				observed := "Observed"
				if card.Unavailable {
					observed = "Last observed"
				}
				fmt.Printf("  %s: %s", observed, card.ObservedAt)
				if card.Source != "" {
					fmt.Printf("  Source: %s", card.Source)
				}
				fmt.Println()
			}
			if card.URL != "" {
				fmt.Printf("  Page: %s\n", card.URL)
			}
			fmt.Println()
			printed = true
		}
	}
	return printed
}

// titleWord capitalizes a provider or agent token for a card heading. The tokens
// are validated lowercase ASCII identifiers, so this needs no Unicode casing.
func titleWord(token string) string {
	if token == "" {
		return ""
	}
	return strings.ToUpper(token[:1]) + token[1:]
}

// A placeholder card still has to say why it is empty: a provider that failed
// and a provider with nothing to report look identical once the numbers are gone.
func quotaProviderUnavailableText(card quotaapp.ProviderCard) string {
	if card.UnavailableReason == quotaapp.ProviderReasonError {
		return "no data (provider failed)"
	}
	return "no data (provider reported nothing)"
}

func formatResetTime(epoch int64) string {
	return time.Unix(epoch, 0).In(time.Local).Format("01-02 15:04")
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

// formatQuotaTrend renders a rate as a direction a person can act on. Below this
// threshold the number is sampling jitter, not movement, and calling it "rising"
// would make a resting quota look like a problem.
const quotaTrendFlatPercentPerHour = 0.5

func formatQuotaTrend(trend quotaapp.Trend, now time.Time) string {
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
		text += fmt.Sprintf("  ⚠ full in %s, before reset", quotaapp.Countdown(trend.FullAt, now))
	}
	return text
}

func formatQuotaSpan(minutes int) string {
	if minutes >= 60 {
		return fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}
