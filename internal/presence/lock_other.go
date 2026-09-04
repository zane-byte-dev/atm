//go:build !darwin && !linux

package presence

import (
	"errors"
	"os"
)

func acquireLock(string) (*os.File, error) {
	return nil, errors.New("Agent hooks require macOS or Linux")
}
func releaseLock(*os.File) {}
