package cmd

import (
	"fmt"

	"github.com/zane-byte-dev/atm/internal/output"
	syncapp "github.com/zane-byte-dev/atm/internal/sync"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(syncStatusCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync session data to local database",
	Args:  noSubcommandArgs,
	RunE:  runSync,
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show session index freshness and last sync result",
	Args:  cobra.NoArgs,
	RunE:  runSyncStatus,
}

func runSync(cmd *cobra.Command, args []string) error {
	result, err := syncapp.Default.Run(
		commandContext(cmd),
		cliApplicationCall("sync", ""),
		syncapp.RunInput{Agent: agentFlag},
	)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		output.Progress("%s", warning)
	}
	if jsonOutput {
		output.JSON(map[string]any{"synced": result.SyncedFiles})
	} else {
		output.Progress("Synced %d files.", result.SyncedFiles)
	}
	return nil
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	report, err := syncapp.Default.Status(
		commandContext(cmd),
		cliApplicationCall("sync-status", ""),
		syncapp.StatusInput{Scope: agentFlag, Sync: syncBeforeRead},
	)
	if err != nil {
		return err
	}
	return printSyncStatus(report)
}

func printSyncStatus(report syncapp.StatusReport) error {
	if jsonOutput {
		output.JSON(report)
		return nil
	}

	fmt.Printf("Session index: %s\n", report.Sync.Status)
	fmt.Printf("  path: %s\n", report.Index.Path)
	fmt.Printf("  sessions: %d", report.Index.IndexedSessions)
	if report.Index.RetainedSessions > 0 {
		fmt.Printf(" (%d retained after their source was removed)", report.Index.RetainedSessions)
	}
	fmt.Println()
	if report.Sync.LastSuccessAt != nil {
		fmt.Printf("  last success: %s", *report.Sync.LastSuccessAt)
		if report.Sync.AgeSeconds != nil {
			fmt.Printf(" (%s ago)", formatShortDuration(*report.Sync.AgeSeconds))
		}
		fmt.Println()
	} else {
		fmt.Println("  last success: never")
	}
	if report.Sync.LastError != "" {
		fmt.Printf("  last error: %s\n", report.Sync.LastError)
	}
	return nil
}
