package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

const (
	configMutationHelperEnv     = "ATM_TEST_CONFIG_MUTATION_HELPER"
	configMutationPathEnv       = "ATM_TEST_CONFIG_MUTATION_PATH"
	configMutationRoleEnv       = "ATM_TEST_CONFIG_MUTATION_ROLE"
	configMutationReadyEnv      = "ATM_TEST_CONFIG_MUTATION_READY"
	configMutationAttemptedEnv  = "ATM_TEST_CONFIG_MUTATION_ATTEMPTED"
	configMutationReleaseEnv    = "ATM_TEST_CONFIG_MUTATION_RELEASE"
	configMutationCompletedEnv  = "ATM_TEST_CONFIG_MUTATION_COMPLETED"
	configMutationHolderRole    = "holder"
	configMutationContenderRole = "contender"
)

// TestConfigMutationHelperProcess is invoked as a separate process by
// TestConcurrentProcessesPatchConfigWithoutLostUpdates. The holder deliberately
// pauses after reading config.json; without a lock spanning read + patch +
// rename, the contender would save first and the holder's stale snapshot would
// erase its field.
func TestConfigMutationHelperProcess(t *testing.T) {
	if os.Getenv(configMutationHelperEnv) != "1" {
		return
	}

	ConfigPath = os.Getenv(configMutationPathEnv)
	AtmDir = filepath.Dir(ConfigPath)
	switch os.Getenv(configMutationRoleEnv) {
	case configMutationHolderRole:
		err := mutateRawConfig(func(raw map[string]any) error {
			if err := os.WriteFile(os.Getenv(configMutationReadyEnv), []byte("ready"), 0o600); err != nil {
				return err
			}
			if err := waitForTestFile(os.Getenv(configMutationReleaseEnv), 10*time.Second); err != nil {
				return err
			}
			raw["owner_name"] = "holder"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	case configMutationContenderRole:
		if err := os.WriteFile(os.Getenv(configMutationAttemptedEnv), []byte("attempted"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeSettings(map[string]any{"text_model_name": "contender"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv(configMutationCompletedEnv), []byte("completed"), 0o600); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown helper role %q", os.Getenv(configMutationRoleEnv))
	}
}

func TestConcurrentProcessesPatchConfigWithoutLostUpdates(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("cross-process config locking is supported on macOS and Linux")
	}
	withTempConfigHome(t)
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"future_setting\": {\"kept\": true}\n}\n")
	if err := os.WriteFile(ConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	coordination := t.TempDir()
	ready := filepath.Join(coordination, "holder-ready")
	attempted := filepath.Join(coordination, "contender-attempted")
	release := filepath.Join(coordination, "release-holder")
	completed := filepath.Join(coordination, "contender-completed")

	holder, holderOutput := configMutationHelperCommand(ConfigPath, configMutationHolderRole, ready, attempted, release, completed)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(release, []byte("release"), 0o600)
		if holder.Process != nil {
			_ = holder.Process.Kill()
		}
	})
	holderDone := waitForCommand(holder)
	if err := waitForTestFile(ready, 5*time.Second); err != nil {
		t.Fatalf("holder did not acquire config lock: %v\n%s", err, holderOutput.String())
	}

	contender, contenderOutput := configMutationHelperCommand(ConfigPath, configMutationContenderRole, ready, attempted, release, completed)
	if err := contender.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if contender.Process != nil {
			_ = contender.Process.Kill()
		}
	})
	contenderDone := waitForCommand(contender)
	if err := waitForTestFile(attempted, 5*time.Second); err != nil {
		t.Fatalf("contender did not start its patch: %v\n%s", err, contenderOutput.String())
	}

	// The contender has reached writeSettings, but cannot complete while the
	// holder is paused inside its mutation callback with the flock held.
	select {
	case err := <-contenderDone:
		t.Fatalf("contender bypassed the config lock: %v\n%s", err, contenderOutput.String())
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(completed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("contender completed while holder owned lock: %v", err)
	}

	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := waitForCommandResult(holderDone, 5*time.Second); err != nil {
		t.Fatalf("holder failed: %v\n%s", err, holderOutput.String())
	}
	if err := waitForCommandResult(contenderDone, 5*time.Second); err != nil {
		t.Fatalf("contender failed: %v\n%s", err, contenderOutput.String())
	}

	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved config is invalid JSON: %v\n%s", err, data)
	}
	if raw["owner_name"] != "holder" || raw["text_model_name"] != "contender" {
		t.Fatalf("concurrent patches lost a field: %#v", raw)
	}
	if _, ok := raw["future_setting"]; !ok {
		t.Fatalf("unknown field was lost: %#v", raw)
	}
}

func TestAtomicConfigWriteFailurePreservesPreviousJSON(t *testing.T) {
	withTempConfigHome(t)
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"owner_name\": \"before\"\n}\n")
	if err := os.WriteFile(ConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	sensitiveCause := errors.New("secret-value-must-not-leak")
	err := writeRawConfigAtomically(
		ConfigPath,
		map[string]any{"owner_name": "after"},
		func(_, _ string) error { return sensitiveCause },
	)
	if !errors.Is(err, application.ErrUnavailable) || !errors.Is(err, sensitiveCause) {
		t.Fatalf("write error = %v, want typed unavailable wrapping cause", err)
	}
	if strings.Contains(err.Error(), "secret-value-must-not-leak") || strings.Contains(err.Error(), ConfigPath) {
		t.Fatalf("public error leaked persistence details: %v", err)
	}

	got, readErr := os.ReadFile(ConfigPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed write damaged previous config:\ngot  %s\nwant %s", got, original)
	}
	temporaryFiles, globErr := filepath.Glob(filepath.Join(filepath.Dir(ConfigPath), ".config.json.tmp-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("failed write left temporary files: %v", temporaryFiles)
	}
}

func TestMalformedRawConfigReturnsTypedConflict(t *testing.T) {
	withTempConfigHome(t)
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"api_key":"secret",`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadRawConfig()
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("load error = %v, want typed conflict", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), ConfigPath) {
		t.Fatalf("public error leaked config contents or path: %v", err)
	}
}

func configMutationHelperCommand(configPath, role, ready, attempted, release, completed string) (*exec.Cmd, *bytes.Buffer) {
	command := exec.Command(os.Args[0], "-test.run=^TestConfigMutationHelperProcess$")
	command.Env = append(os.Environ(),
		configMutationHelperEnv+"=1",
		configMutationPathEnv+"="+configPath,
		configMutationRoleEnv+"="+role,
		configMutationReadyEnv+"="+ready,
		configMutationAttemptedEnv+"="+attempted,
		configMutationReleaseEnv+"="+release,
		configMutationCompletedEnv+"="+completed,
	)
	output := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = output
	return command, output
}

func waitForCommand(command *exec.Cmd) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	return done
}

func waitForCommandResult(done <-chan error, timeout time.Duration) error {
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("process did not exit within %s", timeout)
	}
}

func waitForTestFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
