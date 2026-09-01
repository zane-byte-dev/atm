//go:build !darwin && !linux

package work

import (
	"os"
	"sync"
)

var fallbackPlanDocumentMutex sync.Mutex

func lockPlanDocumentFile(_ *os.File) error {
	fallbackPlanDocumentMutex.Lock()
	return nil
}

func unlockPlanDocumentFile(_ *os.File) {
	fallbackPlanDocumentMutex.Unlock()
}
