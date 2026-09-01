package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/store"
)

// guardPollInterval is how often a waiting gate re-reads its request. Same
// cadence as `atm todo tail -f`, and the same reasoning: fast enough that a click
// feels immediate, slow enough to be invisible.
const guardPollInterval = 500 * time.Millisecond

// guardFollowGrace bounds how long a second invocation waits on the outcome of a
// command another invocation is already executing.
const guardFollowGrace = 20 * time.Second

// guardOutputCap limits how much of the command's output is held for the record.
const guardOutputCap = 4096

// execReplace is indirected only so tests can observe the pass-through. The real
// implementation replaces this process, which would take the test binary with it.
var execReplace = replaceProcess

var guardExecCmd = &cobra.Command{
	Use:   "exec --tool <name> -- <real-binary> [args...]",
	Short: "Run a command through the gate (invoked by an installed shim)",
	Long: `Runs a command, asking you first if it reaches somebody else.

Not meant to be typed. ` + "`atm guard install`" + ` puts a shim at the tool's own path
that calls this, so every agent on the machine goes through it without any of
them being configured.

A command no rule matches is handed straight to the real binary and this process
disappears. A command that matches becomes a request you decide on.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runGuardExec,
}

func runGuardExec(cmd *cobra.Command, args []string) error {
	tool := strings.TrimSpace(guardExecTool)
	realBin := args[0]
	argv := args[1:]

	if !guardExecSupported() {
		return execReplace(realBin, argv, os.Environ())
	}

	match, err := guard.Find(argv, guard.Rules(tool))
	if err != nil {
		// A rule for this tool could not be evaluated, so whether this command is a
		// send is unknown. Blocking is the only honest answer: passing it through
		// would send on the strength of ATM's own misconfiguration.
		return guardBlocked(tool, argv, err)
	}
	if match == nil {
		return execReplace(realBin, argv, os.Environ())
	}
	return runGatedCommand(tool, realBin, argv, match)
}

// runGatedCommand is the only path that touches the database, the socket, or the
// clock. Everything before it is the pass-through, which is why an ATM failure
// here can be loud without breaking the reads an agent makes all day.
func runGatedCommand(tool, realBin string, argv []string, match *guard.Match) error {
	wait := guardExecWait
	if wait <= 0 {
		wait = guard.Wait()
	}
	expire := guardExecExpire
	if expire <= 0 {
		expire = guard.Expire()
	}

	now := time.Now().In(config.Loc)
	pid := guardExecPID()
	cwd, _ := os.Getwd()
	// The deadline is written before the wait begins, so a gate killed by its
	// agent's own timeout still has a truthful record of when it stopped owning
	// execution.
	gateDeadline := now.Add(wait).Unix()

	// Writable on purpose: this is the path that must run the v45 migration, and a
	// read-only handle could not have created the request anyway.
	db, err := store.Open()
	if err != nil {
		return guardBlocked(tool, argv, err)
	}
	defer db.Close()

	dedup := store.ApprovalDedupKey(tool, argv, cwd)
	cooldown := now.Add(-guard.DenyCooldown()).Unix()
	if denied, err := store.RecentDeniedApproval(db, dedup, cooldown); err != nil {
		return guardBlocked(tool, argv, err)
	} else if denied != nil {
		// The user already answered this exact command. Answering again from the
		// record is what stops a retrying agent from re-raising the same banner.
		return guardDenied(*denied)
	}

	request := store.Approval{
		DedupKey:      dedup,
		Tool:          tool,
		RuleID:        match.Rule.ID,
		RealBin:       realBin,
		Argv:          argv,
		CWD:           cwd,
		EnvAgent:      guardCallingAgent(),
		Label:         guardLabel(match, tool, argv),
		PreviewTarget: match.Target,
		PreviewTitle:  match.Title,
		PreviewBody:   guardBody(match, tool, argv),
		StdinPiped:    guardStdinCarriesContent(),
		GatePID:       pid,
		GateDeadline:  gateDeadline,
		RequestedAt:   now.Unix(),
		ExpiresAt:     now.Add(expire).Unix(),
	}

	approval, err := store.CreateApproval(db, request)
	switch {
	case errors.Is(err, store.ErrApprovalPending):
		existing, lookupErr := store.PendingApprovalByDedup(db, dedup, now.Unix())
		if lookupErr != nil {
			return guardBlocked(tool, argv, lookupErr)
		}
		if existing == nil {
			return guardBlocked(tool, argv,
				errors.New("an identical request was pending and then vanished"))
		}
		if err := store.AttachApproval(db, existing.ID, pid, gateDeadline); err != nil {
			return guardBlocked(tool, argv, err)
		}
		// Deliberately no notification: this is the same command asking again, and
		// a second banner for one decision is worse than none.
		approval = *existing
		approval.GatePID = pid
		approval.GateDeadline = gateDeadline
	case err != nil:
		return guardBlocked(tool, argv, err)
	default:
		notifyGuardRequest(approval)
	}

	return waitForGuardDecision(db, approval, realBin, argv, wait, pid)
}

// waitForGuardDecision polls until somebody decides or the wait budget runs out.
//
// The handle is held across ticks rather than reopened: in WAL mode each query
// takes its own read snapshot, so a decision written by the app in another
// process is visible on the very next tick.
func waitForGuardDecision(db *sql.DB, approval store.Approval, realBin string, argv []string,
	wait time.Duration, pid int) error {
	deadline := time.Now().Add(wait)
	for {
		now := time.Now().In(config.Loc).Unix()
		current, err := store.GetApproval(db, approval.ID, now)
		if err != nil {
			return guardBlocked(approval.Tool, argv, err)
		}
		if current == nil {
			return guardBlocked(approval.Tool, argv,
				fmt.Errorf("request %s disappeared while waiting", approval.ID))
		}

		switch current.EffectiveStatus(now) {
		case store.ApprovalApproved:
			return guardRunApproved(db, *current, realBin, argv, pid)
		case store.ApprovalDenied:
			return guardDenied(*current)
		case store.ApprovalRunning, store.ApprovalDone:
			// Another invocation of the identical command got there first. Reporting
			// its outcome as this one's is what stops two agents narrating one send
			// in opposite directions.
			return guardFollowOutcome(db, *current)
		case store.ApprovalExpired:
			return guardPending(*current, true)
		}

		if !time.Now().Before(deadline) {
			// Hand ownership back immediately so a later approval executes at once
			// instead of waiting out a deadline nobody is honouring any more.
			if err := store.ReleaseApprovalGate(db, current.ID); err != nil {
				return guardBlocked(approval.Tool, argv, err)
			}
			return guardPending(*current, false)
		}
		time.Sleep(guardPollInterval)
	}
}

// guardRunApproved claims execution and runs the command. Losing the claim is not
// a failure — it means somebody else is running it.
func guardRunApproved(db *sql.DB, approval store.Approval, realBin string, argv []string, pid int) error {
	if err := store.ClaimApprovalRun(db, approval.ID, "gate", pid); err != nil {
		return guardFollowOutcome(db, approval)
	}
	code, output := runGatedChild(realBin, argv)
	if err := store.FinishApproval(db, approval.ID, code, output); err != nil {
		// The command already ran; failing to record that must not be reported as
		// the command failing.
		fmt.Fprintf(os.Stderr, "atm guard: 命令已执行，但结果没记上：%v\n", err)
	}
	if code != 0 {
		return exitError{code: code, err: fmt.Errorf("%s exited %d", approval.Tool, code)}
	}
	return nil
}

// runGatedChild runs the real binary with this process's stdio, and keeps a copy
// of its output for the record.
//
// The child is deliberately left in this process group — no
// configureBackgroundProcess — so a Ctrl-C in the agent's terminal still reaches
// it. And argv passes through verbatim, including the tool's own `-y`: stripping
// it would make the tool prompt on a stdin that may be closed, and a gate that
// edits the command it is gating is a far larger thing to trust.
func runGatedChild(realBin string, argv []string) (int, string) {
	tail := &guardOutputTail{}
	command := exec.Command(realBin, argv...)
	command.Stdin = os.Stdin
	command.Stdout = io.MultiWriter(os.Stdout, tail)
	command.Stderr = io.MultiWriter(os.Stderr, tail)

	err := command.Run()
	if err == nil {
		return 0, tail.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), tail.String()
	}
	// Could not be started at all — a missing or unexecutable real binary, which
	// usually means a clobbered shim.
	fmt.Fprintf(os.Stderr, "atm guard: 无法执行 %s：%v\n", realBin, err)
	return 126, strings.TrimSpace(tail.String() + "\n" + err.Error())
}

// guardFollowOutcome waits for the invocation that won the claim and then reports
// its result verbatim.
func guardFollowOutcome(db *sql.DB, approval store.Approval) error {
	deadline := time.Now().Add(guardFollowGrace)
	for {
		now := time.Now().In(config.Loc).Unix()
		current, err := store.GetApproval(db, approval.ID, now)
		if err != nil || current == nil {
			return guardRunningElsewhere(approval)
		}
		if current.Status == store.ApprovalDone {
			code := 0
			if current.ExitCode != nil {
				code = *current.ExitCode
			}
			fmt.Fprintf(os.Stderr,
				"atm guard: 这条命令已经由 ATM 执行过一次（%s），退出码 %d。不要再执行。\n",
				current.ID, code)
			if current.Output != "" {
				fmt.Fprintln(os.Stderr, current.Output)
			}
			if code != 0 {
				return exitError{code: code, err: fmt.Errorf("%s exited %d", current.Tool, code)}
			}
			return nil
		}
		if !time.Now().Before(deadline) {
			return guardRunningElsewhere(*current)
		}
		time.Sleep(guardPollInterval)
	}
}

// notifyGuardRequest pushes the request to the app, and raises a local banner
// itself only if the app is not there to raise one. Two banners for one decision
// is the failure mode being avoided.
func notifyGuardRequest(approval store.Approval) {
	envelope := agentevent.GuardEnvelope{
		ID:        approval.ID,
		Tool:      approval.Tool,
		Label:     approval.Label,
		Target:    approval.PreviewTarget,
		Title:     approval.PreviewTitle,
		Body:      approval.PreviewBody,
		CWD:       approval.CWD,
		Agent:     approval.EnvAgent,
		ExpiresAt: approval.ExpiresAt,
	}
	if err := agentevent.DeliverGuard(envelope); err == nil {
		return
	}
	subtitle := approval.Label
	if approval.PreviewTarget != "" {
		subtitle += " → " + approval.PreviewTarget
	}
	body := approval.PreviewBody
	if body == "" {
		body = "在 ATM 中批准或拒绝：" + approval.ID
	}
	postLocalBanner("ATM 待授权", subtitle, body, "guard show "+approval.ID)
}

func guardLabel(match *guard.Match, tool string, argv []string) string {
	if label := strings.TrimSpace(match.Rule.Label); label != "" {
		return label
	}
	return guard.RedactedCommand(tool, argv)
}

// guardBody falls back to the command itself when no extractor produced one. The
// user is being asked to approve something and must see what; an empty card is
// not an acceptable answer, and skipping the gate certainly is not.
func guardBody(match *guard.Match, tool string, argv []string) string {
	if body := strings.TrimSpace(match.Body); body != "" {
		return body
	}
	return guard.RedactedCommand(tool, argv)
}

// guardCallingAgent is best effort and often empty: of the agents that actually
// call these CLIs, only some announce themselves in the environment at all. The
// working directory is the reliable answer to "who asked", and is always recorded.
func guardCallingAgent() string {
	return cliAgentFromEnvironment()
}

// guardStdinCarriesContent reports whether the command was handed input it could
// read, which makes deferred execution unsafe: stdin cannot be replayed, so
// approving later would run the command against different or absent content.
//
// The test is deliberately narrow — a pipe or a file with bytes actually in it —
// rather than the obvious "stdin is not a terminal". Agents do not run commands
// on a terminal: measured, Claude Code hands the child a *socket*, and /dev/null
// is a character device, so "not a terminal" is true for essentially every real
// invocation and would have permanently disabled deferred execution, which is the
// path by which anything ever completes.
//
// Under-detection costs nothing here: none of the gated commands read stdin at
// all, since every one of them carries its content in flags.
func guardStdinCarriesContent() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	mode := info.Mode()
	if mode&os.ModeNamedPipe != 0 || mode.IsRegular() {
		return info.Size() > 0
	}
	return false
}

// guardOutputTail keeps the last of a command's output. The tail rather than the
// head: when a send fails, the reason is at the end.
type guardOutputTail struct {
	buffer []byte
}

func (t *guardOutputTail) Write(chunk []byte) (int, error) {
	t.buffer = append(t.buffer, chunk...)
	if len(t.buffer) > guardOutputCap {
		t.buffer = t.buffer[len(t.buffer)-guardOutputCap:]
	}
	return len(chunk), nil
}

func (t *guardOutputTail) String() string {
	return strings.TrimSpace(string(t.buffer))
}
