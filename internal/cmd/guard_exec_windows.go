//go:build windows

package cmd

import (
	"fmt"
	"os"
)

// Windows has no exec-replace, and the shim the installer writes is a POSIX shell
// script. Rather than half-supporting the gate — where a user could install it,
// see no error, and believe sends were being reviewed — every entry point refuses
// outright and says so.
func replaceProcess(string, []string, []string) error {
	return fmt.Errorf("the outbound action gate is not supported on Windows")
}

func guardExecSupported() bool { return false }

func guardExecPID() int { return os.Getpid() }
