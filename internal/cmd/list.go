package cmd

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

var (
	daysFlag           int
	projectFlag        string
	sessionSinceFlag   string
	sessionReviewFlag  string
	sessionListAllFlag bool
	sessionListLimit   int
	sessionListOffset  int
	sessionListOrder   string
)

func init() {
	listCmd.Flags().IntVar(&daysFlag, "days", 1, "number of days to look back")
	listCmd.Flags().StringVar(&projectFlag, "project", "", "filter by project name (substring match)")
	listCmd.Flags().StringVar(&sessionSinceFlag, "since", "", "look back from RFC3339 timestamp or YYYY-MM-DD (overrides --days)")
	listCmd.Flags().StringVar(&sessionReviewFlag, "review", "all", "memory review state: all, pending, or reviewed")
	listCmd.Flags().BoolVar(&sessionListAllFlag, "all", false, "list every indexed session, ignoring the time window")
	listCmd.Flags().StringVar(&sessionListOrder, "order", "asc", "sort by start time: asc (oldest first) or desc (newest first)")
	listCmd.Flags().IntVar(&sessionListLimit, "limit", 0, "maximum number of sessions (0 means all)")
	listCmd.Flags().IntVar(&sessionListOffset, "offset", 0, "number of sessions to skip")
	sessionCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent sessions",
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}

	days := daysFlag
	if days < 1 {
		days = 1
	}
	return withDB(true, func(db *sql.DB) error {
		now := time.Now().In(config.Loc)
		start := startOfDayWindow(now, days)
		if sessionSinceFlag != "" {
			start, err = parseSessionSince(sessionSinceFlag)
			if err != nil {
				return err
			}
		}
		if sessionListAllFlag {
			// The whole index, for browsing rather than triage: a recent-activity
			// window cannot reach a session once it scrolls out of it, and search
			// only helps when you already know what to search for.
			start = time.Unix(0, 0).In(config.Loc)
		}
		if sessionReviewFlag != "all" && sessionReviewFlag != "pending" && sessionReviewFlag != "reviewed" {
			return fmt.Errorf("invalid --review %q: use all, pending, or reviewed", sessionReviewFlag)
		}

		results, err := store.ListSessions(db, start.Unix(), now.Unix(), agent, projectFlag)
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}

		reviews, err := knowledge.SessionReviews()
		if err != nil {
			return fmt.Errorf("read session review state: %w", err)
		}
		filtered := results[:0]
		for _, result := range results {
			_, reviewed := reviews[result.FullID]
			if sessionReviewFlag == "pending" && reviewed {
				continue
			}
			if sessionReviewFlag == "reviewed" && !reviewed {
				continue
			}
			filtered = append(filtered, result)
		}
		switch sessionListOrder {
		case "asc":
		case "desc":
			// Paging a browsing list only makes sense against a stable order, and
			// the first page a reader wants is the most recent work.
			for left, right := 0, len(filtered)-1; left < right; left, right = left+1, right-1 {
				filtered[left], filtered[right] = filtered[right], filtered[left]
			}
		default:
			return fmt.Errorf("invalid --order %q: use asc or desc", sessionListOrder)
		}
		matched := len(filtered)
		results, err = paginate(filtered, sessionListOffset, sessionListLimit)
		if err != nil {
			return err
		}

		if jsonOutput {
			type jsonSession struct {
				ID        string                   `json:"id"`
				ShortID   string                   `json:"short_id"`
				Agent     string                   `json:"agent"`
				Project   string                   `json:"project"`
				CreatedAt string                   `json:"created_at"`
				LastAt    string                   `json:"last_at,omitempty"`
				QCount    int                      `json:"q_count"`
				Summary   string                   `json:"summary,omitempty"`
				FirstQ    string                   `json:"first_q,omitempty"`
				Review    *knowledge.SessionReview `json:"memory_review,omitempty"`
			}
			var sessions []jsonSession
			for _, r := range results {
				ts := r.CreatedAt
				if r.CreatedTS > 0 {
					ts = time.Unix(r.CreatedTS, 0).In(config.Loc).Format(time.RFC3339)
				}
				lastAt := ""
				if r.LastTS > 0 {
					lastAt = time.Unix(r.LastTS, 0).In(config.Loc).Format(time.RFC3339)
				}
				var review *knowledge.SessionReview
				if value, ok := reviews[r.FullID]; ok {
					copy := value
					review = &copy
				}
				sessions = append(sessions, jsonSession{
					ID:        r.FullID,
					ShortID:   r.ShortID,
					Agent:     r.Agent,
					Project:   r.Project,
					CreatedAt: ts,
					LastAt:    lastAt,
					QCount:    r.QCount,
					Summary:   r.Summary,
					FirstQ:    truncLine(cleanMsg(r.FirstQ), 200),
					Review:    review,
				})
			}
			output.JSON(sessions)
			return nil
		}

		label := "today"
		switch {
		case sessionListAllFlag:
			label = "all"
		case days > 1:
			label = fmt.Sprintf("last %d days", days)
		}
		// The count is what the window matched, not what this page shows, so a
		// limited page never reads as "that is all there is".
		fmt.Printf("Sessions (%s, %d total)\n", label, matched)
		fmt.Println(strings.Repeat("=", 60))

		if len(results) == 0 {
			fmt.Println("\nNo sessions found.")
			return nil
		}
		if len(results) < matched {
			fmt.Printf("Showing %d-%d\n", sessionListOffset+1, sessionListOffset+len(results))
		}

		for _, r := range results {
			desc := r.Summary
			if desc == "" {
				desc = truncLine(cleanMsg(r.FirstQ), 200)
			}
			fmt.Printf("  %-10s %-12s %-11s %-20s  Q:%-3d  %s\n", r.ShortID, r.CreatedAt, r.Agent, r.Project, r.QCount, desc)
		}
		return nil
	})
}

func parseSessionSince(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(config.Loc), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, config.Loc); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: use RFC3339 or YYYY-MM-DD", value)
}
