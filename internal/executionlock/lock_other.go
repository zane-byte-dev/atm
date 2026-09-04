//go:build !darwin && !linux

package executionlock

import (
	"errors"
	"os"
)

func openLockFile(_, _ string) (*os.File, error) {
	return nil, errors.New("cross-process execution locks require macOS or Linux")
}

// Do not silently replace cross-process exclusion with a process-local mutex.
// ATM's supported macOS and Linux runtimes use the flock implementation.
func tryLockFile(_ *os.File) (bool, error) {
	return false, errors.New("cross-process execution locks require macOS or Linux")
}

func unlockFile(_ *os.File) error { return nil }
