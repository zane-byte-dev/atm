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

var guardListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List gated actions and their decisions",
	Aliases: []string{"ls"},
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return withDB(true, func(db *sql.DB) error {
			statuses, err := guardStatusFilter(guardListStatus)
			if err != nil {
				return err
			}
			now := time.Now().In(config.Loc).Unix()
			approvals, err := store.ListApprovals(db, statuses, now, guardListLimit)
			if err != nil {
				return err
			}
			if jsonOutput {
				output.JSON(approvals)
				return nil
			}
			if len(approvals) == 0 {
				fmt.Println("No gated actions.")
				return nil
			}
			for _, approval := range approvals {
				fmt.Printf("%s  %-8s %s\n", approval.ID, approval.Effective,
					guardActionLine(approval))
				if body := guardOneLine(approval.PreviewBody); body != "" {
					fmt.Printf("            %s\n", body)
				}
			}
			pending := 0
			for _, approval := range approvals {
				if approval.Effective == store.ApprovalPending {
					pending++
				}
			}
			if pending > 0 {
				fmt.Printf("\n%d 待授权 · atm guard approve <id> | atm guard deny <id>\n", pending)
			}
			return nil
		})
	},
}

var guardShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one gated action in full, including its outcome",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDB(true, func(db *sql.DB) error {
			now := time.Now().In(config.Loc).Unix()
			approval, err := store.GetApproval(db, args[0], now)
			if err != nil {
				return err
			}
			if approval == nil {
				return fmt.Errorf("approval not found: %s", args[0])
			}
			if jsonOutput {
				output.JSON(approval)
				return nil
			}
			fmt.Printf("%s  %s\n", approval.ID, approval.Effective)
			fmt.Printf("动作:   %s\n", guardActionLine(*approval))
			if approval.PreviewTitle != "" {
				fmt.Printf("标题:   %s\n", approval.PreviewTitle)
			}
			if approval.PreviewBody != "" {
				fmt.Printf("正文:\n%s\n", approval.PreviewBody)
			}
			fmt.Printf("命令:   %s\n", strings.Join(append([]string{approval.RealBin}, approval.Argv...), " "))
			if approval.CWD != "" {
				fmt.Printf("目录:   %s\n", approval.CWD)
			}
			if approval.EnvAgent != "" {
				fmt.Printf("来源:   %s\n", approval.EnvAgent)
			}
			fmt.Printf("请求于: %s\n", guardTime(approval.RequestedAt))
			fmt.Printf("有效至: %s\n", guardTime(approval.ExpiresAt))
			if approval.AttachCount > 1 {
				fmt.Printf("重试:   %d 次（等待预算可能太短）\n", approval.AttachCount-1)
			}
			if approval.DecidedAt != nil {
				fmt.Printf("决定于: %s（%s）\n", guardTime(*approval.DecidedAt), approval.DecidedBy)
			}
			if approval.Reason != "" {
				fmt.Printf("理由:   %s\n", approval.Reason)
			}
			if approval.RanBy != "" {
				fmt.Printf("执行者: %s\n", approval.RanBy)
			}
			if approval.ExitCode != nil {
				fmt.Printf("退出码: %d\n", *approval.ExitCode)
			}
			if approval.Output != "" {
				fmt.Printf("输出:\n%s\n", approval.Output)
			}
			// A request stuck in running is the one state nothing may recover from,
			// so say plainly that the answer is not in ATM.
			if approval.Status == store.ApprovalRunning {
				fmt.Println("\n执行中且未回报结果。ATM 无法判断消息是否已经发出 —— " +
					"请自己到目标群/会话确认，不要重跑。")
			}
			return nil
		})
	},
}

// guardStatusFilter turns the --status flag into the effective statuses to match.
func guardStatusFilter(value string) ([]string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "all" {
		return nil, nil
	}
	valid := map[string]bool{
		store.ApprovalPending: true, store.ApprovalApproved: true,
		store.ApprovalRunning: true, store.ApprovalDone: true,
		store.ApprovalDenied: true, store.ApprovalExpired: true,
	}
	statuses := []string{}
	for _, status := range strings.Split(value, ",") {
		status = strings.TrimSpace(status)
		if status == "" {
			continue
		}
		if !valid[status] {
			return nil, fmt.Errorf("unknown status: %s", status)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func guardTime(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).In(config.Loc).Format("2006-01-02 15:04")
}

// guardOneLine collapses a message body for a list row. The list is for deciding
// which request to open, not for reading the message.
func guardOneLine(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " "))
	runes := []rune(value)
	if len(runes) <= 72 {
		return value
	}
	return string(runes[:72]) + "…"
}
