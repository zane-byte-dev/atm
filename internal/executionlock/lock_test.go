//go:build darwin || linux

package executionlock

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("ATM_EXECUTION_LOCK_HELPER") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := Acquire(ctx, os.Getenv("ATM_EXECUTION_LOCK_DIR"), "shared")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("acquired")
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestLockExcludesOtherProcessesAndReleasesOnExit(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash=%t", crash), func(t *testing.T) {
			dir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestLockHelperProcess$")
			command.Env = append(os.Environ(), "ATM_EXECUTION_LOCK_HELPER=1", "ATM_EXECUTION_LOCK_DIR="+dir)
			command.Stderr = os.Stderr
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill() })
			scanner := bufio.NewScanner(stdout)
			if !scanner.Scan() || scanner.Text() != "acquired" {
				t.Fatalf("helper did not acquire lock: %s (%v)", scanner.Text(), scanner.Err())
			}
			waitCtx, stopWaiting := context.WithTimeout(ctx, 100*time.Millisecond)
			lock, err := Acquire(waitCtx, dir, "shared")
			stopWaiting()
			if lock != nil || !errors.Is(err, context.DeadlineExceeded) {
				if lock != nil {
					lock.Close()
				}
				t.Fatalf("other process must exclude waiter: lock=%v error=%v", lock, err)
			}
			other, err := Acquire(ctx, dir, "other-job")
			if err != nil {
				t.Fatalf("independent job blocked: %v", err)
			}
			other.Close()
			if crash {
				if err := command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := stdin.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err != nil && !crash {
				t.Fatal(err)
			}
			lock, err = Acquire(ctx, dir, "shared")
			if err != nil {
				t.Fatalf("process exit did not release lock: %v", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			if err := lock.Close(); err != nil {
				t.Fatalf("repeated close failed: %v", err)
			}
		})
	}
}

func TestLockWaiterAcquiresAfterRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dir := t.TempDir()
	first, err := Acquire(ctx, dir, "sync")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	result := make(chan error, 1)
	go func() {
		second, err := Acquire(ctx, dir, "sync")
		if err == nil {
			err = second.Close()
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("waiter finished while lock held: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestCancelledAndInvalidLockRequestsCreateNoRuntimeFiles(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, dir, "sync"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	for _, name := range []string{"", "../sync", "path/name", "has space", strings.Repeat("a", 65)} {
		if lock, err := Acquire(context.Background(), dir, name); err == nil {
			lock.Close()
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid requests created runtime directory: %v", err)
	}
}

func TestLockRejectsSymlinkAncestorsAndLinkedFiles(t *testing.T) {
	for _, kind := range []string{"runtime", "locks", "file-symlink", "file-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			target := filepath.Join(outside, "target")
			if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "runtime")
			switch kind {
			case "runtime":
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "locks":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(path, "locks")); err != nil {
					t.Fatal(err)
				}
			default:
				path = filepath.Join(path, "locks")
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				link := os.Symlink
				if kind == "file-hardlink" {
					link = os.Link
				}
				if err := link(target, filepath.Join(path, "sync.lock")); err != nil {
					t.Fatal(err)
				}
			}
			if lock, err := Acquire(context.Background(), dir, "sync"); err == nil {
				lock.Close()
				t.Fatal("linked lock path was accepted")
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("target mode changed: %v (%v)", info, err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "keep" {
				t.Fatalf("target content changed: %q (%v)", data, err)
			}
			if _, err := os.Stat(filepath.Join(outside, "sync.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created external lock: %v", err)
			}
		})
	}
}
