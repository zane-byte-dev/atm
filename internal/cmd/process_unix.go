//go:build !windows

package cmd

import (
	"os/exec"
	"syscall"
)

func configureBackgroundProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processIsRunning(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}
