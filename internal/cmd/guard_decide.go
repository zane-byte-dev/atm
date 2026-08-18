package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

// guardDeferredTimeout bounds a command ATM runs on the user's behalf. Generous,
// because the alternative — killing a send halfway and reporting failure for a
// message that went out — is the worse error.
const guardDeferredTimeout = 60 * time.Second

var guardApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a gated action and run it",
	Long: `Approves the request and, unless a waiting agent still owns it, runs the command.

ATM runs it itself because by the time you decide, the agent that asked has
usually been told not to retry and has moved on. Pass --run=false to record the
approval without running anything.

Only what came through an installed shim can be run this way, and a command whose
input arrived on a pipe is refused rather than replayed against different content.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDB(false, func(db *sql.DB) error {
			return runGuardDecision(db, args[0], true)
		})
	},
}

var guardDenyCmd = &cobra.Command{
	Use:   "deny <id>",
	Short: "Refuse a gated action",
	Long: `Records a refusal. The waiting agent is told to hand you the content instead.

The refusal also answers for a short while: an agent re-running the identical
command is refused from the record rather than raising the same request again.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDB(false, func(db *sql.DB) error {
			return runGuardDecision(db, args[0], false)
		})
	},
}

func runGuardDecision(db *sql.DB, id string, approve bool) error {
	now := time.Now().In(config.Loc).Unix()
	approval, err := store.GetApproval(db, id, now)
	if err != nil {
		return err
	}
	if approval == nil {
		return fmt.Errorf("approval not found: %s", id)
	}
	if approval.Status == store.ApprovalRunning {
		// Nothing may act on a running request. Whether the command took effect is
		// not recorded anywhere, and re-running it would send twice.
		return fmt.Errorf("approval %s is already executing; check the target yourself with `atm guard show %s`",
			id, id)
	}

	if !approve {
		if err := store.DenyApproval(db, id, now, guardDecideBy, guardDecideReason); err != nil {
			return err
		}
		return guardReportDecision(db, id, "denied", now)
	}

	if err := store.ApproveApproval(db, id, now, guardDecideBy, guardDecideReason); err != nil {
		return err
	}

	// The waiting gate owns execution until the deadline it wrote down. Handing it
	// the work rather than racing it is what keeps one approved command from being
	// run twice, and it does not depend on guessing whether that process is alive.
	if !guardApproveRun {
		return guardReportDecision(db, id, "approved", now)
	}
	if approval.GateOwnsExecution(now) {
		return guardReportDecision(db, id, "approved-gate-runs", now)
	}
	return guardRunDeferred(db, id, now)
}

// guardRunDeferred is ATM executing a command on the user's behalf, which makes
// the approvals table an execution surface. Two constraints keep that narrow:
// only a binary displaced by an installed shim may be run, and the agent's
// environment is never replayed — the command runs in ATM's own environment, in
// the recorded working directory. Replaying the environment would mean storing
// whatever secrets it held.
func guardRunDeferred(db *sql.DB, id string, now int64) error {
	approval, err := store.GetApproval(db, id, now)
	if err != nil {
		return err
	}
	if approval == nil {
		return fmt.Errorf("approval not found: %s", id)
	}
	if approval.StdinPiped {
		return fmt.Errorf(
			"approval %s was given input on a pipe, which cannot be reproduced; "+
				"it is approved but must be re-run by whoever composed it", id)
	}
	if err := guardExecutableIsRegistered(approval.Tool, approval.RealBin); err != nil {
		return err
	}

	if err := store.ClaimApprovalRun(db, id, "app", os.Getpid()); err != nil {
		// Lost the claim to a gate that woke up first. Its result is the real one.
		return guardReportDecision(db, id, "approved-gate-runs", now)
	}

	command := exec.Command(approval.RealBin, approval.Argv...)
	command.Dir = approval.CWD
	combined, runErr := guardRunWithTimeout(command)
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 126
			combined = strings.TrimSpace(combined + "\n" + runErr.Error())
		}
	}
	if err := store.FinishApproval(db, id, code, combined); err != nil {
		return err
	}
	if code != 0 {
		// Approved and attempted is not the same as sent. Say which happened.
		return fmt.Errorf("approved, but %s exited %d: %s", approval.Tool, code, guardOneLine(combined))
	}
	return guardReportDecision(db, id, "approved-and-ran", now)
}

func guardRunWithTimeout(command *exec.Cmd) (string, error) {
	tail := &guardOutputTail{}
	command.Stdout = tail
	command.Stderr = tail
	if err := command.Start(); err != nil {
		return tail.String(), err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return tail.String(), err
	case <-time.After(guardDeferredTimeout):
		if command.Process != nil {
			command.Process.Kill()
		}
		<-done
		return tail.String(), fmt.Errorf("timed out after %s; the action may or may not have taken effect",
			guardDeferredTimeout)
	}
}

// guardExecutableIsRegistered narrows what ATM will run to the binaries the user
// installed a gate in front of. Without it, anything able to write a row in the
// approvals table would get arbitrary code executed by ATM.
func guardExecutableIsRegistered(tool, realBin string) error {
	if !guard.IsRealBinPath(realBin) {
		return fmt.Errorf("refusing to run %s: not a binary displaced by an ATM shim", realBin)
	}
	// The tool comes from the row, not from a scan of the config: a row that named
	// a tool nothing gates must be refused rather than matched against whatever
	// else happens to be installed.
	binPath := guard.BinPathFromReal(realBin)
	state, err := guard.Status(tool, binPath)
	if err != nil {
		return fmt.Errorf("refusing to run %s: %w", realBin, err)
	}
	if !state.Installed || state.RealPath != realBin {
		return fmt.Errorf("refusing to run %s: %s is not currently gated at %s", realBin, tool, binPath)
	}
	if _, err := os.Stat(realBin); err != nil {
		return fmt.Errorf("refusing to run %s: %w", realBin, err)
	}
	return nil
}

func guardReportDecision(db *sql.DB, id, outcome string, now int64) error {
	approval, err := store.GetApproval(db, id, now)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(approval)
		return nil
	}
	switch outcome {
	case "denied":
		fmt.Printf("已拒绝 %s：%s\n", id, guardActionLine(*approval))
	case "approved":
		fmt.Printf("已批准 %s（未执行）：%s\n", id, guardActionLine(*approval))
	case "approved-gate-runs":
		fmt.Printf("已批准 %s，由正在等待的调用方执行：%s\n", id, guardActionLine(*approval))
	case "approved-and-ran":
		fmt.Printf("已批准并执行 %s：%s\n", id, guardActionLine(*approval))
	}
	return nil
}
