package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/output"
)

var guardListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List gated actions and their decisions",
	Aliases: []string{"ls"},
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		result, meta, err := guard.Default.List(cmd.Context(), guardCLICall(), guard.ListInput{
			Status: guardListStatus,
			Limit:  guardListLimit,
			Sync:   syncBeforeRead,
		})
		if err != nil {
			return err
		}
		if meta.SyncedFiles > 0 && !jsonOutput {
			output.Progress("Synced %d files.", meta.SyncedFiles)
		}
		if jsonOutput {
			output.JSON(result.Approvals)
			return nil
		}
		if len(result.Approvals) == 0 {
			fmt.Println("No gated actions.")
			return nil
		}
		for _, approval := range result.Approvals {
			fmt.Printf("%s  %-8s %s\n", approval.ID, approval.Effective,
				guardActionLine(approval))
			if body := guardOneLine(approval.PreviewBody); body != "" {
				fmt.Printf("            %s\n", body)
			}
		}
		pending := 0
		for _, approval := range result.Approvals {
			if approval.Effective == guard.ApprovalPending {
				pending++
			}
		}
		if pending > 0 {
			fmt.Printf("\n%d 待授权 · atm guard approve <id> | atm guard deny <id>\n", pending)
		}
		return nil
	},
}

var guardShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one gated action in full, including its outcome",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, meta, err := guard.Default.Show(cmd.Context(), guardCLICall(), guard.ShowInput{
			ID: args[0], Sync: syncBeforeRead,
		})
		if err != nil {
			return err
		}
		if meta.SyncedFiles > 0 && !jsonOutput {
			output.Progress("Synced %d files.", meta.SyncedFiles)
		}
		approval := result.Approval
		if jsonOutput {
			output.JSON(approval)
			return nil
		}
		fmt.Printf("%s  %s\n", approval.ID, approval.Effective)
		fmt.Printf("动作:   %s\n", guardActionLine(approval))
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
		if approval.Status == guard.ApprovalRunning {
			fmt.Println("\n执行中且未回报结果。ATM 无法判断消息是否已经发出 —— " +
				"请自己到目标群/会话确认，不要重跑。")
		}
		return nil
	},
}

func guardCLICall() application.Call {
	return cliApplicationCall("guard", "")
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
