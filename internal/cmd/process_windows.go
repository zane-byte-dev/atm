//go:build windows

package cmd

import (
	"fmt"
	"os/exec"
	"strconv"

	"golang.org/x/sys/windows"
)

func configureBackgroundProcess(command *exec.Cmd) {}

func processIsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}

func terminateTaskRunProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("task run controller has no process id")
	}
	// /T includes the Agent child tree; /F is required because Windows has no
	// SIGTERM equivalent that a detached console process reliably receives.
	if output, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w: %s", err, string(output))
	}
	return nil
}
