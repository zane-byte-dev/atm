package cmd

import (
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"

	"github.com/spf13/cobra"
)

var (
	daysFlag            int
	projectFlag         string
	sessionSinceFlag    string
	sessionReviewFlag   string
	sessionListAllFlag  bool
	sessionListLimit    int
	sessionListOffset   int
	sessionListOrder    string
	sessionListEnvelope bool
)

const defaultSessionListLimit = 200

func init() {
	listCmd.Flags().IntVar(&daysFlag, "days", 1, "number of days to look back")
	listCmd.Flags().StringVar(&projectFlag, "project", "", "filter by project name (case-insensitive substring)")
	listCmd.Flags().StringVar(&sessionSinceFlag, "since", "", "look back from RFC3339 timestamp or YYYY-MM-DD (overrides --days)")
	listCmd.Flags().StringVar(&sessionReviewFlag, "review", "all", "memory review state: all, pending, or reviewed")
	listCmd.Flags().BoolVar(&sessionListAllFlag, "all", false, "list every indexed session, ignoring the time window")
	listCmd.Flags().StringVar(&sessionListOrder, "order", "activity-desc", "sort order: activity-desc (latest activity first), asc, or desc (by start time)")
	listCmd.Flags().IntVar(&sessionListLimit, "limit", defaultSessionListLimit, "maximum number of sessions (0 means all)")
	listCmd.Flags().IntVar(&sessionListOffset, "offset", 0, "number of sessions to skip")
	listCmd.Flags().BoolVar(&sessionListEnvelope, "envelope", false, "wrap JSON rows with schema and pagination metadata")
	sessionCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent sessions",
	Long: `List indexed sessions in latest-activity order with a bounded default page.

Use --offset to fetch the next page, --limit 0 for an explicitly unbounded
result, or --all to ignore the time window. Historical --json output remains a
top-level array; --envelope adds schema and pagination metadata.`,
	Example: `  atm session list --days 7
  atm session list --all --limit 200 --offset 200 --json --envelope`,
	Args: cobra.NoArgs,
	RunE: runList,
}

func runList(cmd *cobra.Command, args []string) error {
	if sessionListEnvelope && !jsonOutput {
		return fmt.Errorf("--envelope requires --json")
	}
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	result, err := currentSessionService().List(cmd.Context(), sessionapp.ListInput{
		Agent: agent, Project: projectFlag, Days: daysFlag, Since: sessionSinceFlag,
		Review: sessionReviewFlag, All: sessionListAllFlag, Order: sessionListOrder,
		Limit: sessionListLimit, Offset: sessionListOffset, SyncBeforeRead: syncBeforeRead,
	})
	if err != nil {
		return err
	}
	renderSessionReadMeta(result.Meta)

	if jsonOutput {
		type jsonSession struct {
			ID                 string             `json:"id"`
			ShortID            string             `json:"short_id"`
			Agent              string             `json:"agent"`
			Project            string             `json:"project"`
			CreatedAt          string             `json:"created_at"`
			LastAt             string             `json:"last_at,omitempty"`
			QCount             int                `json:"q_count"`
			LocalUserTurnCount int                `json:"local_user_turn_count"`
			Summary            string             `json:"summary,omitempty"`
			FirstQ             string             `json:"first_q,omitempty"`
			ResumeID           string             `json:"resume_id,omitempty"`
			RootSessionID      string             `json:"root_session_id,omitempty"`
			ParentSessionID    string             `json:"parent_session_id,omitempty"`
			AgentPath          string             `json:"agent_path,omitempty"`
			AgentNickname      string             `json:"agent_nickname,omitempty"`
			SubagentDepth      int                `json:"subagent_depth,omitempty"`
			IsSubagent         bool               `json:"is_subagent,omitempty"`
			ParserVersion      int                `json:"parser_version"`
			ContentState       string             `json:"content_state"`
			ResultStatus       string             `json:"result_status"`
			LatestProgress     string             `json:"latest_progress,omitempty"`
			FinalResult        string             `json:"final_result,omitempty"`
			Review             *sessionapp.Review `json:"memory_review,omitempty"`
		}
		var sessions []jsonSession
		for _, row := range result.Sessions {
			sessions = append(sessions, jsonSession{
				ID: row.ID, ShortID: row.ShortID, Agent: row.Agent, Project: row.Project,
				CreatedAt: row.CreatedAt, LastAt: row.LastAt, QCount: row.QuestionCount,
				LocalUserTurnCount: row.LocalUserTurnCount, Summary: row.Summary,
				FirstQ: truncLine(row.FirstQuestion, 200), ResumeID: row.ResumeID,
				RootSessionID: row.RootSessionID, ParentSessionID: row.ParentSessionID,
				AgentPath: row.AgentPath, AgentNickname: row.AgentNickname,
				SubagentDepth: row.SubagentDepth, IsSubagent: row.IsSubagent,
				ParserVersion: row.ParserVersion, ContentState: row.ContentState,
				ResultStatus: row.ResultStatus, LatestProgress: row.LatestProgress,
				FinalResult: row.FinalResult, Review: row.Review,
			})
		}
		if sessionListEnvelope {
			output.JSON(map[string]any{
				"schema_version": sessionCLIOutputSchemaVersion,
				"total":          result.Total,
				"returned":       len(sessions),
				"truncated":      result.Offset+len(sessions) < result.Total,
				"limit":          result.Limit,
				"offset":         result.Offset,
				"order":          sessionListOrder,
				"sessions":       sessions,
			})
		} else {
			// Preserve the historical top-level array for scripts and older clients.
			// New integrations can opt into the common metadata envelope above.
			output.JSON(sessions)
		}
		return nil
	}

	label := "today"
	switch {
	case result.All:
		label = "all"
	case result.Days > 1:
		label = fmt.Sprintf("last %d days", result.Days)
	}
	fmt.Printf("Sessions (%s, %d total)\n", label, result.Total)
	fmt.Println(strings.Repeat("=", 60))

	if len(result.Sessions) == 0 {
		fmt.Println("\nNo sessions found.")
		return nil
	}
	if len(result.Sessions) < result.Total {
		fmt.Printf("Showing %d-%d\n", result.Offset+1, result.Offset+len(result.Sessions))
	}
	for _, row := range result.Sessions {
		description := row.Summary
		if description == "" {
			description = truncLine(row.FirstQuestion, 200)
		}
		created := row.IndexedCreated
		if created == "" {
			created = row.CreatedAt
		}
		fmt.Printf("  %-10s %-12s %-11s %-20s  Q:%-3d  %s\n",
			row.ShortID, created, row.Agent, row.Project, row.QuestionCount, description)
	}
	return nil
}
