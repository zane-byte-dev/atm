//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// replaceProcess hands this process over to the real binary and never returns on
// success.
//
// The fast path uses this rather than spawning a child for two reasons. It costs
// nothing measurable, which matters because every read an agent makes now goes
// through here. And it leaves no residue: a lingering Go parent for each of those
// reads would show up in `ps` and in ATM's own session scraping.
func replaceProcess(bin string, argv []string, env []string) error {
	return syscall.Exec(bin, append([]string{bin}, argv...), env)
}

// guardExecSupported reports whether interposition works on this platform.
func guardExecSupported() bool { return true }

func guardExecPID() int { return os.Getpid() }
