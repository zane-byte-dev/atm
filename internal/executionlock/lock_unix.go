//go:build darwin || linux

package executionlock

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Anchor traversal to open directory descriptors. Reject links within runtime,
// including a swapped lock path, before chmod/open can affect another file.
func openLockFile(dataDir, name string) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	root, err := unix.Open(dataDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open data directory: %w", err)
	}
	defer unix.Close(root)
	runtime, err := openLockDirectory(root, "runtime")
	if err != nil {
		return nil, fmt.Errorf("open runtime directory: %w", err)
	}
	defer unix.Close(runtime)
	locks, err := openLockDirectory(runtime, "locks")
	if err != nil {
		return nil, fmt.Errorf("open locks directory: %w", err)
	}
	defer unix.Close(locks)
	fd, err := openLockAt(locks, name+".lock")
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Nlink != 1 {
		unix.Close(fd)
		return nil, errors.New("execution lock must be a regular file without hard links")
	}
	return os.NewFile(uintptr(fd), name+".lock"), nil
}

func openLockAt(directory int, name string) (int, error) {
	flags := unix.O_RDWR | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
	for range 4 {
		fd, err := unix.Openat(directory, name, flags, 0)
		if !errors.Is(err, unix.ENOENT) {
			return fd, err
		}
		// Only one first user creates the inode. A racing creator reopens the
		// winner instead of relying on simultaneous non-exclusive creation.
		fd, err = unix.Openat(directory, name, flags|unix.O_CREAT|unix.O_EXCL, 0o600)
		if !errors.Is(err, unix.EEXIST) {
			return fd, err
		}
	}
	return -1, errors.New("execution lock path changed repeatedly while opening")
}

func openLockDirectory(parent int, name string) (int, error) {
	if err := unix.Mkdirat(parent, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, err
	}
	return unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
}

func tryLockFile(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
