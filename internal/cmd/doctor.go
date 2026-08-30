package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	doctorapp "github.com/zane-byte-dev/atm/internal/doctor"
	"github.com/zane-byte-dev/atm/internal/output"
)

var doctorDaysFlag int

func init() {
	doctorCmd.Flags().IntVar(&doctorDaysFlag, "days", 0, "request coverage window in rolling days (0 = all history)")
	rootCmd.AddCommand(doctorCmd)
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check data sources and request-level coverage",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	report, err := doctorapp.Default.Check(commandContext(cmd), cliApplicationCall("doctor", ""), doctorapp.Input{Days: doctorDaysFlag})
	if err != nil {
		return err
	}
	printDoctorReport(report)
	return nil
}

func printDoctorReport(report doctorapp.Report) {
	if jsonOutput {
		output.JSON(report)
		return
	}
	fmt.Println("ATM Doctor")
	fmt.Println(strings.Repeat("=", 72))
	fmt.Println("\nData sources")
	for _, s := range report.Sources {
		fmt.Printf("  %-11s %-29s files=%-5d indexed=%-5d retained=%-5d %s\n",
			s.Agent, s.Status, s.Files, s.IndexedSessions, s.RetainedSessions, s.Path)
	}
	fmt.Println("\nRequest detail coverage")
	if report.CoverageWindow.Mode == "rolling" {
		fmt.Printf("  window: last %d days (%s to %s)\n", report.CoverageWindow.Days,
			report.CoverageWindow.Start, report.CoverageWindow.End)
	}
	for _, c := range report.Coverage {
		fmt.Printf("  %-11s %-12s sessions=%-5d detailed=%-6d reported=%-6d coverage=%6.1f%% unknown_model=%-4d timed=%6.1f%%\n",
			c.Agent, c.CoverageStatus, c.Sessions, c.DetailedRequests, c.ReportedRequests,
			c.CoveragePercent, c.UnknownModels, c.TimedPercent)
	}
	fmt.Println("  timed = share of requests whose speed could be measured from the transcript")
	if len(report.ModelPricing) > 0 {
		fmt.Println("\nCost rates")
		for _, p := range report.ModelPricing {
			fmt.Printf("  %-30s %-8s cost=%10.2f requests=%-7d\n", p.Model, p.Source, p.CostUSD, p.Requests)
		}
		fmt.Println("  exact = the model's own rate | family = its family's rate | default = no match, Opus-tier upper bound")
		fmt.Println("  family and default are estimates; pin exact rates in ~/.atm/pricing.json")
	}
	fmt.Printf("\nIssues (%d)\n", report.Summary.Issues)
	if len(report.Issues) == 0 {
		fmt.Println("  none")
	}
	for _, issue := range report.Issues {
		fmt.Printf("  %-7s %-10s %-32s %s\n", issue.Severity, issue.Domain, issue.Code, issue.Subject)
		fmt.Printf("           %s\n", issue.Detail)
		fmt.Printf("           next: %s\n", issue.Suggestion)
	}
}
