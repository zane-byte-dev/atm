//go:build darwin || linux

package work

import (
	"os"
	"syscall"
)

func lockPlanDocumentFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockPlanDocumentFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
