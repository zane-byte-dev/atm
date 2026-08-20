//go:build windows

package taskrun

import (
	"fmt"
	"os/exec"
	"strconv"
)

func (localProcess) Interrupt(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("task run controller has no process id")
	}
	if output, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w: %s", err, string(output))
	}
	return nil
}
