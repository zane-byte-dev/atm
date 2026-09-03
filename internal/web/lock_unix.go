//go:build darwin || linux

package web

import (
	"errors"
	"os"
	"syscall"
)

func lockInstance(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return ErrAlreadyRunning
	}
	return err
}

func unlockInstance(file *os.File) { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
