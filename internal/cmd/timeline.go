package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
)

func init() { sessionCmd.AddCommand(timelineCmd) }

var timelineCmd = &cobra.Command{Use: "timeline <session-id>", Short: "Show messages and model requests in time order", Args: cobra.ExactArgs(1), RunE: runTimeline}

func runTimeline(cmd *cobra.Command, args []string) error {
	result, err := currentSessionService().Timeline(cmd.Context(), sessionapp.TimelineInput{
		SessionID: args[0], SyncBeforeRead: syncBeforeRead,
	})
	if err != nil {
		return err
	}
	renderSessionReadMeta(result.Meta)
	if jsonOutput {
		output.JSON(result.Events)
		return nil
	}
	for _, event := range result.Events {
		ts := time.Unix(event.TS, 0).In(config.Loc).Format("01-02 15:04:05")
		if event.Kind == "message" {
			content := strings.ReplaceAll(strings.TrimSpace(event.Content), "\n", " ")
			if len([]rune(content)) > 120 {
				content = truncLine(content, 120) + "…"
			}
			fmt.Printf("%s  %-9s %s\n", ts, event.Role, content)
		} else {
			fmt.Printf("%s  request   %-24s in=%s out=%s cache=%s cost=$%.4f\n",
				ts, event.Model, fmtTokens(event.InputTokens), fmtTokens(event.OutputTokens),
				fmtTokens(event.CacheTokens), event.CostUSD)
		}
	}
	return nil
}
