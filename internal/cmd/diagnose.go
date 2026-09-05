package cmd

import (
	"fmt"
	"sort"
	"strings"

	diagnoseapp "github.com/zane-byte-dev/atm/internal/diagnose"
	"github.com/zane-byte-dev/atm/internal/output"

	"github.com/spf13/cobra"
)

func init() {
	diagnoseCmd.Flags().BoolVar(&diagnoseBundleFlag, "bundle", false, "write the report to a file that can be attached to a bug report")
	diagnoseCmd.Flags().StringVarP(&diagnoseOutputFlag, "output", "o", "", "bundle path (default: ./atm-diagnose-<timestamp>.json)")
	rootCmd.AddCommand(diagnoseCmd)
}

var (
	diagnoseBundleFlag bool
	diagnoseOutputFlag string
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Collect the facts needed to debug an ATM problem",
	Long: `Collect the facts needed to debug an ATM problem.

ATM has no telemetry, so nothing about a broken install reaches anyone unless a
person sends it. ` + "`atm diagnose --bundle`" + ` writes a single file holding the
versions, schema versions, doctor findings, data source presence and last sync
error, which can be attached to a bug report as-is.

It reads local state only and never uploads anything. Session text, todo, memory
and knowledge content are never included, and paths under your home directory
are rewritten to ~ so the file does not carry your username.

For the full data-source and coverage tables, use ` + "`atm doctor`" + `.`,
	Args: cobra.NoArgs,
	RunE: runDiagnose,
}

// diagnoseService is composed per call rather than once at package scope: the ATM
// version arrives through cmd.SetVersion after main starts, and a package
// variable would capture the empty string that is there before it.
func diagnoseService() diagnoseapp.Service {
	return diagnoseapp.NewService(diagnoseapp.ServiceOptions{Version: rootCmd.Version})
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx := commandContext(cmd)
	call := cliApplicationCall("diagnose", "")
	service := diagnoseService()

	if diagnoseBundleFlag {
		result, err := service.WriteBundle(ctx, call, diagnoseapp.BundleInput{Path: diagnoseOutputFlag})
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"bundle": result.Path, "bytes": result.Bytes})
			return nil
		}
		fmt.Printf("Wrote %s (%s)\n", result.Path, formatBytes(int64(result.Bytes)))
		fmt.Println("  contains: versions, schema versions, doctor findings, data source presence, last sync error")
		fmt.Println("  excludes: session text, todo/memory/knowledge content, credentials")
		fmt.Println("  attach it to the bug report as-is; nothing was uploaded")
		return nil
	}

	report, err := service.Report(ctx, call, diagnoseapp.Input{})
	if err != nil {
		return err
	}
	if jsonOutput {
		redacted, err := service.RedactedJSON(report)
		if err != nil {
			return err
		}
		// Already-marshalled JSON, so it is written directly rather than
		// re-encoded through output.JSON.
		fmt.Println(string(redacted))
		return nil
	}
	printDiagnoseSummary(report)
	return nil
}

func printDiagnoseSummary(report diagnoseapp.Report) {
	fmt.Println("ATM Diagnose")
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("  atm %s (schema v%d)\n", report.ATM.Version, report.ATM.SchemaVersion)
	if report.ATM.DatabaseExists {
		fmt.Printf("  database: v%d, %s\n", report.ATM.DatabaseSchemaVersion, formatBytes(report.ATM.DatabaseBytes))
		if report.ATM.DatabaseSchemaVersion > report.ATM.SchemaVersion {
			fmt.Println("            newer than this build — upgrade atm")
		}
		if report.ATM.DatabaseError != "" {
			fmt.Printf("            unreadable: %s\n", report.ATM.DatabaseError)
		}
	} else {
		fmt.Println("  database: absent — run `atm sync`")
	}
	fmt.Printf("  platform: %s/%s, %s\n", report.Platform.OS, report.Platform.Arch, report.Platform.GoVersion)
	fmt.Printf("  sync: %s", report.Sync.Sync.Status)
	if report.Sync.Sync.LastError != "" {
		fmt.Printf(" — last error: %s", report.Sync.Sync.LastError)
	}
	fmt.Println()

	counts := map[string]int{}
	for _, issue := range report.Doctor.Issues {
		counts[issue.Severity]++
	}
	severities := make([]string, 0, len(counts))
	for severity := range counts {
		severities = append(severities, severity)
	}
	sort.Strings(severities)
	if len(severities) == 0 {
		fmt.Println("  doctor: no issues")
	} else {
		parts := make([]string, 0, len(severities))
		for _, severity := range severities {
			parts = append(parts, fmt.Sprintf("%d %s", counts[severity], severity))
		}
		fmt.Printf("  doctor: %s (run `atm doctor` for detail)\n", strings.Join(parts, ", "))
	}
	fmt.Println("\nRun `atm diagnose --bundle` to write a file you can attach to a bug report.")
}
