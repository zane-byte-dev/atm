//go:build !darwin && !linux

package web

import (
	"fmt"
	"os"
)

func lockInstance(file *os.File) error { return fmt.Errorf("atm serve requires macOS or Linux") }
func unlockInstance(file *os.File)     {}
