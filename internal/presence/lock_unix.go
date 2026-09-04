//go:build darwin || linux

package presence

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func acquireLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return f, nil
}

func releaseLock(f *os.File) {
	if f == nil {
		return
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}
