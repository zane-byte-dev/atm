//go:build !darwin && !linux

package collector

import (
	"os"
	"sync"
)

// ATM's supported desktop/server platforms use flock. Keep other targets
// buildable and serialize collection marker writers within one process.
var fallbackTodoMarkerMutex sync.Mutex

func lockTodoMarkerFile(_ *os.File) error {
	fallbackTodoMarkerMutex.Lock()
	return nil
}

func unlockTodoMarkerFile(_ *os.File) {
	fallbackTodoMarkerMutex.Unlock()
}
