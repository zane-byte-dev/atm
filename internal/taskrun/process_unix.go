//go:build !windows

package taskrun

import (
	"fmt"
	"syscall"
	"time"
)

// Interrupt stops the detached controller and the Agent process group it owns.
func (localProcess) Interrupt(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("task run controller has no process id")
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
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
