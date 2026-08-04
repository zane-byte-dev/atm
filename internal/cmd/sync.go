package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(syncStatusCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync session data to local database",
	Args:  cobra.NoArgs,
	RunE:  runSync,
}

type syncStatusIndex struct {
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
	SchemaVersion int    `json:"schema_version"`
	// IndexedSessions counts every session the index holds, including the
	// RetainedSessions whose transcript is no longer on disk. It grows
	// monotonically, so it is not a count of what an agent currently stores.
	IndexedSessions  int `json:"indexed_sessions"`
	RetainedSessions int `json:"retained_sessions"`
}

type syncStatusState struct {
	Scope             string  `json:"scope"`
	Status            string  `json:"status"`
	RunStatus         string  `json:"run_status"`
	LastAttemptAt     *string `json:"last_attempt_at"`
	LastSuccessAt     *string `json:"last_success_at"`
	AgeSeconds        *int64  `json:"age_seconds"`
	StaleAfterSeconds int64   `json:"stale_after_seconds"`
	LastError         string  `json:"last_error"`
	LastSyncedFiles   int     `json:"last_synced_files"`
}

type syncStatusReport struct {
	GeneratedAt string          `json:"generated_at"`
	Index       syncStatusIndex `json:"index"`
	Sync        syncStatusState `json:"sync"`
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show session index freshness and last sync result",
	Args:  cobra.NoArgs,
	RunE:  runSyncStatus,
}

func runSync(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	return withDB(false, func(db *sql.DB) error {
		var count int
		if agent != "" {
			count, err = store.SyncAgent(db, agent)
		} else {
			count, err = store.SyncAll(db)
		}
		if err != nil {
			return fmt.Errorf("sync error: %w", err)
		}
		// Sampling rides on sync rather than a timer of its own: the desktop app
		// already syncs every few minutes, which is resolution enough for an
		// hourly rate. A failure here must not fail the sync — history is a
		// convenience, the session index is the point.
		if sampleErr := recordQuotaSamples(db, time.Now()); sampleErr != nil {
			output.Progress("quota history not recorded: %v", sampleErr)
		}
		if jsonOutput {
			output.JSON(map[string]any{"synced": count})
		} else {
			output.Progress("Synced %d files.", count)
		}
		return nil
	})
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	scope := agent
	if scope == "" {
		scope = store.SyncScopeAll
	}
	report, err := buildSyncStatusReport(scope)
	if err != nil {
		return err
	}
	return printSyncStatus(report)
}

func buildSyncStatusReport(scope string) (syncStatusReport, error) {
	if _, statErr := os.Stat(config.AtmDB); statErr != nil && !syncBeforeRead {
		if !os.IsNotExist(statErr) {
			return syncStatusReport{}, fmt.Errorf("inspect session index: %w", statErr)
		}
		return syncStatusReport{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Index: syncStatusIndex{
				Path:   config.AtmDB,
				Exists: false,
			},
			Sync: syncStatusState{
				Scope:             scope,
				Status:            "missing",
				RunStatus:         "never",
				StaleAfterSeconds: int64(store.DefaultSyncStaleAfter.Seconds()),
			},
		}, nil
	}

	var report syncStatusReport
	err := withDB(true, func(db *sql.DB) error {
		var err error
		report, err = syncStatusReportFromDB(db, scope)
		return err
	})
	return report, err
}

func syncStatusReportFromDB(db *sql.DB, scope string) (syncStatusReport, error) {
	health, err := store.ReadSyncHealth(db, scope, time.Now(), store.DefaultSyncStaleAfter)
	if err != nil {
		return syncStatusReport{}, err
	}
	return syncStatusReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Index: syncStatusIndex{
			Path:             config.AtmDB,
			Exists:           true,
			SchemaVersion:    health.SchemaVersion,
			IndexedSessions:  health.IndexedSessions,
			RetainedSessions: health.RetainedSessions,
		},
		Sync: syncStatusState{
			Scope:             health.Scope,
			Status:            health.Status,
			RunStatus:         health.RunStatus,
			LastAttemptAt:     health.LastAttemptAt,
			LastSuccessAt:     health.LastSuccessAt,
			AgeSeconds:        health.AgeSeconds,
			StaleAfterSeconds: health.StaleAfterSeconds,
			LastError:         health.LastError,
			LastSyncedFiles:   health.LastSyncedFiles,
		},
	}, nil
}

func printSyncStatus(report syncStatusReport) error {
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
