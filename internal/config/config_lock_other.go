//go:build !darwin && !linux

package config

import (
	"os"
	"sync"
)

// ATM's supported desktop/server platforms use the flock implementation. This
// fallback keeps other targets buildable and serializes writers in-process.
var fallbackConfigWriteMutex sync.Mutex

func lockConfigFile(_ *os.File) error {
	fallbackConfigWriteMutex.Lock()
	return nil
}

func unlockConfigFile(_ *os.File) {
	fallbackConfigWriteMutex.Unlock()
}
