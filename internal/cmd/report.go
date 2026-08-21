package cmd

import (
	"fmt"
	"sort"
	"strings"

	reportapp "github.com/zane-byte-dev/atm/internal/report"

	"github.com/spf13/cobra"
)

func init() {
	reportCmd.Flags().BoolVar(&reportVerbose, "verbose", false, "show filtered question and answer details")
	rootCmd.AddCommand(reportCmd)
}

var reportVerbose bool

var reportCmd = &cobra.Command{
	Use:   "report [date]",
	Short: "Generate daily report (default: today)",
	Long: `Generate a daily activity report.

Date supports: today, yesterday, or YYYY-MM-DD format.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReport,
}

// reportPromptPreview is how many prompts a non-verbose session lists before it
// says how many more there were. The point of the digest is to recognize the
// session, not to reread it.
const reportPromptPreview = 2

func runReport(cmd *cobra.Command, args []string) error {
	date := ""
	if len(args) > 0 {
		date = args[0]
	}
	report, err := reportapp.Default.Daily(
		commandContext(cmd),
		cliApplicationCall("report", ""),
		reportapp.Input{Date: date, Agent: agentFlag, Verbose: reportVerbose},
		reportapp.SyncInput{SyncBeforeRead: syncBeforeRead},
	)
	if err != nil {
		return err
	}

	fmt.Printf("Date: %s\n", report.Date)
	if report.Empty() {
		fmt.Println("\nNo AI coding activity recorded.")
		return nil
	}

	for _, section := range report.Agents {
		fmt.Printf("\n== %s ==\n", section.DisplayName)
		for _, session := range section.Sessions {
			printReportSession(session)
		}
		fmt.Println()
	}
	return nil
}

func printReportSession(session reportapp.Session) {
	if summary := truncLine(session.Summary, 120); summary != "" {
		fmt.Printf("\n[%s] %s — %s\n", session.Project, session.ShortID, summary)
	} else {
		fmt.Printf("\n[%s] %s\n", session.Project, session.ShortID)
	}
	if reportVerbose {
		for _, exchange := range session.Exchanges {
			fmt.Printf("  Q: %s\n", truncLine(exchange.Question, 200))
			if answer := truncLine(exchange.Answer, 200); answer != "" {
				fmt.Printf("  A: %s\n", answer)
			}
		}
		if len(session.Tools) > 0 {
			fmt.Printf("  Tools: %s\n", formatTools(session.Tools))
		}
		return
	}
	fmt.Printf("  Activity: %d prompts · %d tool calls\n", len(session.Prompts), session.ToolCalls)
	for index, prompt := range session.Prompts {
		if index == reportPromptPreview {
			fmt.Printf("  + %d more prompts\n", len(session.Prompts)-index)
			break
		}
		fmt.Printf("  • %s\n", truncLine(prompt, 140))
	}
}

func formatTools(tools map[string]int) string {
	var pairs []string
	for k, v := range tools {
		pairs = append(pairs, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}
