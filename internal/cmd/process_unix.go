//go:build !windows

package cmd

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func configureBackgroundProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processIsRunning(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// terminateTaskRunProcess stops the detached controller and every process it
// launched. startTaskRunController makes the controller a process-group leader,
// and the Agent inherits that group, so targeting -pid avoids leaving Codex (or
// one of its tool processes) running after ATM reports the run interrupted.
func terminateTaskRunProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("task run controller has no process id")
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			// A test/manual controller may not be its own group. Fall back to the
			// exact PID; an already-exited process is an idempotent success.
			if directErr := syscall.Kill(pid, syscall.SIGTERM); directErr == syscall.ESRCH {
				return nil
			} else if directErr != nil {
				return directErr
			}
		} else {
			return err
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pid, 0) == syscall.ESRCH {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}
