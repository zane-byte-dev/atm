package cmd

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

func init() { sessionCmd.AddCommand(timelineCmd) }

var timelineCmd = &cobra.Command{Use: "timeline <session-id>", Short: "Show messages and model requests in time order", Args: cobra.ExactArgs(1), RunE: runTimeline}

func runTimeline(cmd *cobra.Command, args []string) error {
	return withDB(true, func(db *sql.DB) error {
		events, err := store.GetSessionTimeline(db, args[0])
		if err != nil {
			return fmt.Errorf("session not found: %s", args[0])
		}
		if jsonOutput {
			output.JSON(events)
			return nil
		}
		for _, e := range events {
			ts := time.Unix(e.TS, 0).In(config.Loc).Format("01-02 15:04:05")
			if e.Kind == "message" {
				content := strings.ReplaceAll(strings.TrimSpace(e.Content), "\n", " ")
				if len([]rune(content)) > 120 {
					content = truncLine(content, 120) + "…"
				}
				fmt.Printf("%s  %-9s %s\n", ts, e.Role, content)
			} else {
				fmt.Printf("%s  request   %-24s in=%s out=%s cache=%s cost=$%.4f\n", ts, e.Model, fmtTokens(e.InputTokens), fmtTokens(e.OutputTokens), fmtTokens(e.CacheTokens), e.CostUSD)
			}
		}
		return nil
	})
}
