package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/zane-byte-dev/atm/internal/application"
)

type configRename func(oldPath, newPath string) error

// mutateRawConfig is the only read-modify-write path for config.json. The
// advisory lock is deliberately held while the file is re-read and patched:
// locking only the final rename would still allow CLI and server processes to
// overwrite each other's changes from stale snapshots.
func mutateRawConfig(apply func(raw map[string]any) error) error {
	return withConfigWriteLock(func(path string) error {
		raw, err := loadRawConfigAt(path)
		if err != nil {
			return err
		}
		if err := apply(raw); err != nil {
			return err
		}
		return writeRawConfigAtomically(path, raw, os.Rename)
	})
}

// loadRawConfig reads the file as an untyped map so a write preserves every
// other field, including ones this build does not know about.
func loadRawConfig() (map[string]any, error) {
	return loadRawConfigAt(ConfigPath)
}

func loadRawConfigAt(path string) (map[string]any, error) {
	raw := map[string]any{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return raw, nil
	}
	if err != nil {
		return nil, configPersistenceError(
			application.CodeUnavailable,
			"configuration could not be read",
			err,
			true,
		)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, configPersistenceError(
			application.CodeConflict,
			"configuration file contains invalid JSON",
			err,
			false,
		)
	}
	if raw == nil {
		return nil, configPersistenceError(
			application.CodeConflict,
			"configuration file must contain a JSON object",
			errors.New("top-level JSON value is null"),
			false,
		)
	}
	return raw, nil
}

// saveRawConfig replaces a complete raw snapshot under the same lock used by
// mutations. New read-modify-write callers should use mutateRawConfig so their
// snapshot is read only after acquiring the lock.
func saveRawConfig(raw map[string]any) error {
	return withConfigWriteLock(func(path string) error {
		return writeRawConfigAtomically(path, raw, os.Rename)
	})
}

func withConfigWriteLock(run func(path string) error) error {
	path := ConfigPath
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return unavailableConfigSaveError(err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return unavailableConfigSaveError(err)
	}

	lockPath := filepath.Join(directory, "."+filepath.Base(path)+".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return unavailableConfigSaveError(err)
	}
	defer lockFile.Close()
	if err := lockFile.Chmod(0o600); err != nil {
		return unavailableConfigSaveError(err)
	}
	if err := lockConfigFile(lockFile); err != nil {
		return unavailableConfigSaveError(err)
	}
	defer unlockConfigFile(lockFile)

	return run(path)
}

// writeRawConfigAtomically never changes the destination until the temporary
// file is fully written, flushed and closed. A failed encode/write/sync/close or
// rename therefore leaves the previous JSON intact.
func writeRawConfigAtomically(path string, raw map[string]any, rename configRename) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return configPersistenceError(
			application.CodeInternal,
			"configuration could not be encoded",
			err,
			false,
		)
	}
	data = append(data, '\n')

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return unavailableConfigSaveError(err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	closeTemporary := func() {
		_ = temporary.Close()
	}
	if err := temporary.Chmod(0o600); err != nil {
		closeTemporary()
		return unavailableConfigSaveError(err)
	}
	if _, err := temporary.Write(data); err != nil {
		closeTemporary()
		return unavailableConfigSaveError(err)
	}
	if err := temporary.Sync(); err != nil {
		closeTemporary()
		return unavailableConfigSaveError(err)
	}
	if err := temporary.Close(); err != nil {
		return unavailableConfigSaveError(err)
	}
	if err := rename(temporaryPath, path); err != nil {
		return unavailableConfigSaveError(err)
	}

	// Renaming is the atomic commit. Syncing the containing directory makes that
	// commit durable across a power loss where the filesystem supports it. Some
	// filesystems reject directory fsync; the already-committed file remains
	// valid, so this is intentionally best effort.
	if directoryFile, err := os.Open(directory); err == nil {
		_ = directoryFile.Sync()
		_ = directoryFile.Close()
	}
	return nil
}

func unavailableConfigSaveError(cause error) *application.Error {
	return configPersistenceError(
		application.CodeUnavailable,
		"configuration could not be saved",
		cause,
		true,
	)
}

func configPersistenceError(
	code application.ErrorCode,
	message string,
	cause error,
	retryable bool,
) *application.Error {
	err := application.WrapError(code, message, cause)
	err.Retryable = retryable
	return err
}
