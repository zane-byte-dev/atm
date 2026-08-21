package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/store"
)

// The text in this file is written for a model to read, not a human.
//
// It is the whole hardening story. The real binary the shim displaced is one
// directory listing away, and no file permission stops a process running as the
// user's own uid — so what actually prevents a workaround is telling the model,
// at the moment it is deciding what to do next, that a workaround is not what is
// wanted and why. Three sentences carry that weight:
//
//	不要重试                     — closes the retry loop
//	不要换用其他命令或工具绕过     — closes the "call the moved-aside binary" reflex
//	ATM 会自己执行，重试会发两遍   — gives a concrete reason, which models follow far
//	                              more reliably than a bare prohibition
//
// Each message also hands the message content back, so the model's next move is
// obvious and useful: show it to the user and let them send it themselves.
//
// Execute prints nothing extra for an exitError, deliberately: an English error
// line stapled after this text would be read as further instructions.

// guardStderr is the process's real stderr, not cobra's configurable stream: the
// shim's stderr is what the calling agent reads, and it is the same stream the
// gated command's own output goes to. Indirected only so tests can capture it.
var guardStderr io.Writer = os.Stderr

// guardDenied reports a refusal the user actually made.
func guardDenied(approval store.Approval) error {
	var text strings.Builder
	fmt.Fprintf(&text, "ATM 已拒绝这次外发操作：%s\n", guard.ActionLine(approval))
	text.WriteString("用户在 ATM 中明确拒绝了。不要重试，不要换用其他命令或工具绕过。\n")
	if reason := strings.TrimSpace(approval.Reason); reason != "" {
		fmt.Fprintf(&text, "拒绝理由：%s\n", reason)
	}
	text.WriteString("请把下面这段内容原样输出给用户，由用户自行决定是否手动发送：\n")
	text.WriteString(guardContentBlock(approval))
	text.WriteString("然后继续本轮剩余的工作，不要再尝试发送。\n")

	fmt.Fprint(guardStderr, text.String())
	return exitError{code: guardExitDenied,
		err: fmt.Errorf("outbound action denied: %s", approval.ID)}
}

// guardPending reports that nobody has decided yet. The request is still live, so
// the important instruction is that ATM will run the command itself — a retry
// would send the same message a second time.
func guardPending(approval store.Approval, expired bool) error {
	var text strings.Builder
	if expired {
		fmt.Fprintf(&text, "ATM 的授权请求已过期：%s\n", guard.ActionLine(approval))
		text.WriteString("用户没有在有效期内处理。不要重试，不要换用其他命令或工具绕过。\n")
	} else {
		fmt.Fprintf(&text, "ATM 正在等待用户批准这次外发操作：%s\n", guard.ActionLine(approval))
		fmt.Fprintf(&text, "请求已记录（id=%s），%s 之内有效。用户在 ATM 中批准后，"+
			"ATM 会自己执行这条命令。\n", approval.ID, guard.Expire().String())
		text.WriteString("不要重试，不要换用其他命令或工具绕过 —— " +
			"重试会让同一条消息发两遍。\n")
	}
	text.WriteString("现在把下面这段内容原样输出给用户，并告知「已在 ATM 中等待批准」：\n")
	text.WriteString(guardContentBlock(approval))
	text.WriteString("然后继续本轮剩余的工作。\n")

	fmt.Fprint(guardStderr, text.String())
	return exitError{code: guardExitPending,
		err: fmt.Errorf("outbound action awaiting approval: %s", approval.ID)}
}

// guardRunningElsewhere reports that the command is already being executed by
// whoever won the claim. Whether it has taken effect is genuinely unknown, and
// saying so is better than reporting either success or failure.
func guardRunningElsewhere(approval store.Approval) error {
	var text strings.Builder
	fmt.Fprintf(&text, "这次外发操作正在被 ATM 执行：%s\n", guard.ActionLine(approval))
	fmt.Fprintf(&text, "请求 %s 已经批准并开始执行，但还没有回报结果。"+
		"绝对不要再执行一次 —— 那会把同一条消息发两遍。\n", approval.ID)
	fmt.Fprintf(&text, "告知用户「已批准并执行中，结果可用 atm guard show %s 查看」，"+
		"然后继续本轮剩余的工作。\n", approval.ID)

	fmt.Fprint(guardStderr, text.String())
	return exitError{code: guardExitPending,
		err: fmt.Errorf("outbound action already executing: %s", approval.ID)}
}

// guardBlocked reports that ATM could not record the request.
//
// This is the one place the gate fails closed. Failing open here would send the
// message silently while ATM believed it was reviewing sends, which is worse than
// having no gate at all — the user would have stopped watching. It costs only the
// commands a rule matched: the pass-through never reaches this code, so an ATM
// problem cannot break the reads an agent makes all day.
func guardBlocked(tool string, argv []string, cause error) error {
	var text strings.Builder
	fmt.Fprintf(&text, "ATM 无法记录这次外发操作的授权请求，已阻止本次发送以免静默外发。\n")
	fmt.Fprintf(&text, "原因：%v\n", cause)
	fmt.Fprintf(&text, "命令：%s\n", guard.RedactedCommand(tool, argv))
	text.WriteString("不要重试，不要换用其他命令或工具绕过。\n")
	text.WriteString("请把要发送的内容原样输出给用户，并告知用户运行 `atm doctor` 检查 ATM 自身状态。\n")

	fmt.Fprint(guardStderr, text.String())
	return exitError{code: guardExitBlocked,
		err: fmt.Errorf("guard could not record the request: %w", cause)}
}

// guardContentBlock renders the message so the model can hand it straight to the
// user. Fenced so a multi-line body cannot be mistaken for more instructions.
func guardContentBlock(approval store.Approval) string {
	var block strings.Builder
	block.WriteString("---\n")
	if title := strings.TrimSpace(approval.PreviewTitle); title != "" {
		fmt.Fprintf(&block, "标题：%s\n", title)
	}
	if target := strings.TrimSpace(approval.PreviewTarget); target != "" {
		fmt.Fprintf(&block, "接收方：%s\n", target)
	}
	if body := strings.TrimSpace(approval.PreviewBody); body != "" {
		fmt.Fprintf(&block, "正文：\n%s\n", body)
	}
	block.WriteString("---\n")
	return block.String()
}
