//go:build darwin || linux

package collector

import (
	"os"
	"syscall"
)

func lockTodoMarkerFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockTodoMarkerFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
