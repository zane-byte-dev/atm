package cmd

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"

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

func runReport(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	dateStr := time.Now().In(config.Loc).Format("2006-01-02")
	if len(args) > 0 {
		dateStr = args[0]
	}

	start, end, err := config.DateRange(dateStr)
	if err != nil {
		return fmt.Errorf("invalid date: %s", dateStr)
	}

	return withDB(true, func(db *sql.DB) error {
		results, err := store.GetReport(db, start.Unix(), end.Unix(), agent)
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}

		fmt.Printf("Date: %s\n", start.Format("2006-01-02"))

		if len(results) == 0 {
			fmt.Println("\nNo AI coding activity recorded.")
			return nil
		}

		grouped := map[string][]store.ReportResult{}
		for _, r := range results {
			grouped[r.Agent] = append(grouped[r.Agent], r)
		}

		agentOrder := []string{"pi", "codex", "claude", "copilot", "qoder", "qodercli", "qoderwork", "grokbuild", "antigravity"}
		for _, a := range agentOrder {
			sessions := grouped[a]
			if len(sessions) == 0 {
				continue
			}
			title := store.AgentDisplayName(a)
			fmt.Printf("\n== %s ==\n", title)
			for _, s := range sessions {
				questions := meaningfulInputs(s.Inputs)
				summary := truncLine(cleanMsg(s.Summary), 120)
				if summary == "" && len(questions) > 0 {
					summary = truncLine(questions[0], 120)
				}
				if len(questions) == 0 && (len(s.Tools) == 0 || (!reportVerbose && summary == "")) {
					continue
				}
				if summary != "" {
					fmt.Printf("\n[%s] %s — %s\n", s.Project, s.ShortID, summary)
				} else {
					fmt.Printf("\n[%s] %s\n", s.Project, s.ShortID)
				}
				if reportVerbose {
					for _, m := range formatMessages(s.Inputs, s.Outputs) {
						fmt.Println(m)
					}
					if len(s.Tools) > 0 {
						fmt.Printf("  Tools: %s\n", formatTools(s.Tools))
					}
					continue
				}
				toolCalls := 0
				for _, count := range s.Tools {
					toolCalls += count
				}
				fmt.Printf("  Activity: %d prompts · %d tool calls\n", len(questions), toolCalls)
				for index, question := range questions {
					if index == 2 {
						fmt.Printf("  + %d more prompts\n", len(questions)-index)
						break
					}
					fmt.Printf("  • %s\n", truncLine(question, 140))
				}
			}
			fmt.Println()
		}

		return nil
	})
}

func truncLine(s string, max int) string {
	s = strings.SplitN(s, "\n", 2)[0]
	runes := []rune(s)
	if len(runes) > max {
		s = string(runes[:max])
	}
	return s
}

func cleanMsg(s string) string { return parser.VisibleUserText(s) }

func meaningfulInputs(inputs []string) []string {
	var values []string
	for _, input := range inputs {
		if value := cleanMsg(input); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func formatMessages(inputs, outputs []string) []string {
	var lines []string
	for i, inp := range inputs {
		q := truncLine(cleanMsg(inp), 200)
		if q == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("  Q: %s", q))
		if i < len(outputs) {
			a := truncLine(cleanMsg(outputs[i]), 200)
			if a != "" {
				lines = append(lines, fmt.Sprintf("  A: %s", a))
			}
		}
	}
	return lines
}

func formatTools(tools map[string]int) string {
	var pairs []string
	for k, v := range tools {
		pairs = append(pairs, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}
