package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
)

var (
	sessionToolsFailed bool
	sessionToolsDays   int
	sessionToolsSince  string
	sessionToolsLimit  int
	sessionToolsOffset int
)

func init() {
	sessionToolsCmd.Flags().BoolVar(&sessionToolsFailed, "failed", false, "show only failed CLI invocations")
	sessionToolsCmd.Flags().IntVar(&sessionToolsDays, "days", 7, "look back over today plus the previous N-1 days")
	sessionToolsCmd.Flags().StringVar(&sessionToolsSince, "since", "", "show invocations since RFC3339 timestamp or YYYY-MM-DD")
	sessionToolsCmd.Flags().IntVar(&sessionToolsLimit, "limit", 100, "maximum number of invocations to return")
	sessionToolsCmd.Flags().IntVar(&sessionToolsOffset, "offset", 0, "number of invocations to skip")
	sessionToolsCmd.MarkFlagsMutuallyExclusive("since", "days")
	sessionCmd.AddCommand(sessionToolsCmd)
}

var sessionToolsCmd = &cobra.Command{
	Use:   "tools [session-id]",
	Short: "Inspect content-free ATM CLI invocation telemetry",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSessionTools,
}

func runSessionTools(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	sessionID := ""
	if len(args) > 0 {
		sessionID = args[0]
	}
	days := sessionToolsDays
	if strings.TrimSpace(sessionToolsSince) != "" {
		// --days has a useful non-zero default for help and normal invocations,
		// but an explicitly supplied --since owns the window.
		days = 0
	}
	result, err := currentSessionService().Tools(commandContext(cmd), sessionapp.ToolsInput{
		SessionID: sessionID,
		Agent:     agent,
		Since:     sessionToolsSince,
		Days:      days,
		Failed:    sessionToolsFailed,
		Limit:     sessionToolsLimit,
		Offset:    sessionToolsOffset,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(struct {
			SchemaVersion int                         `json:"schema_version"`
			Total         int                         `json:"total"`
			Matched       int                         `json:"matched"`
			Returned      int                         `json:"returned"`
			Succeeded     int                         `json:"succeeded"`
			Failed        int                         `json:"failed"`
			Truncated     bool                        `json:"truncated"`
			Offset        int                         `json:"offset"`
			Limit         int                         `json:"limit"`
			Invocations   []sessionapp.ToolInvocation `json:"invocations"`
		}{
			SchemaVersion: sessionCLIOutputSchemaVersion,
			Total:         result.Total, Matched: result.Matched, Returned: len(result.Invocations),
			Succeeded: result.Succeeded, Failed: result.Failed,
			Truncated: result.Offset+len(result.Invocations) < result.Matched,
			Offset:    result.Offset, Limit: result.Limit, Invocations: result.Invocations,
		})
		return nil
	}

	filter := "all"
	if sessionToolsFailed {
		filter = "failed"
	}
	fmt.Printf("ATM CLI invocations (%s): %d matched; %d total, %d succeeded, %d failed\n",
		filter, result.Matched, result.Total, result.Succeeded, result.Failed)
	fmt.Println(strings.Repeat("=", 90))
	if len(result.Invocations) == 0 {
		fmt.Println("\nNo CLI invocations found.")
		return nil
	}
	for _, invocation := range result.Invocations {
		status := "ok"
		failure := ""
		if !invocation.Success {
			status = "failed"
			failure = invocation.ErrorCode
			if invocation.CauseClass != "" {
				failure = strings.Trim(strings.Join([]string{failure, invocation.CauseClass}, "/"), "/")
			}
		}
		session := invocation.SessionID
		if session == "" {
			session = "-"
		} else {
			session = shortSessionID(session)
		}
		fmt.Printf("  %-25s %-7s %-9s %-8s %6dms  %-34s %s\n",
			invocation.OccurredAt, status, emptyAs(invocation.Agent, "human"), session,
			invocation.DurationMS, invocation.CommandPath, failure)
	}
	return nil
}
