package guard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultDeferredTimeout = 60 * time.Second
	deferredOutputCap      = 4096
)

// LocalDeferredExecutor is the production process adapter. It never replays
// the requesting agent's environment: the child inherits ATM's own environment
// and runs only in the recorded working directory.
type LocalDeferredExecutor struct {
	Timeout time.Duration
}

func (executor LocalDeferredExecutor) Validate(ctx context.Context, approval Approval) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !IsRealBinPath(approval.RealBin) {
		return fmt.Errorf("refusing to run %s: not a binary displaced by an ATM shim", approval.RealBin)
	}
	binPath := BinPathFromReal(approval.RealBin)
	state, err := Status(approval.Tool, binPath)
	if err != nil {
		return fmt.Errorf("refusing to run %s: %w", approval.RealBin, err)
	}
	if !state.Installed || state.RealPath != approval.RealBin {
		return fmt.Errorf("refusing to run %s: %s is not currently gated at %s",
			approval.RealBin, approval.Tool, binPath)
	}
	if _, err := os.Stat(approval.RealBin); err != nil {
		return fmt.Errorf("refusing to run %s: %w", approval.RealBin, err)
	}
	return nil
}

func (executor LocalDeferredExecutor) Execute(ctx context.Context, approval Approval) ExecutionResult {
	timeout := executor.Timeout
	if timeout <= 0 {
		timeout = defaultDeferredTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tail := &deferredOutputTail{}
	command := exec.CommandContext(runCtx, approval.RealBin, approval.Argv...)
	command.Dir = approval.CWD
	command.Stdout = tail
	command.Stderr = tail
	err := command.Run()
	if err == nil {
		return ExecutionResult{Output: tail.String()}
	}

	code := 126
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	detail := err.Error()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		detail = fmt.Sprintf("timed out after %s; the action may or may not have taken effect", timeout)
	} else if runCtx.Err() != nil {
		detail = runCtx.Err().Error()
	}
	return ExecutionResult{
		ExitCode: code,
		Output:   strings.TrimSpace(tail.String() + "\n" + detail),
	}
}

type deferredOutputTail struct {
	buffer []byte
}

func (tail *deferredOutputTail) Write(chunk []byte) (int, error) {
	tail.buffer = append(tail.buffer, chunk...)
	if len(tail.buffer) > deferredOutputCap {
		tail.buffer = tail.buffer[len(tail.buffer)-deferredOutputCap:]
	}
	return len(chunk), nil
}

func (tail *deferredOutputTail) String() string {
	return strings.TrimSpace(string(tail.buffer))
}
