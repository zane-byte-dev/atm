package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrAlreadyRunning = errors.New("ATM workspace is already running for this data directory")

const instanceHeader = "X-ATM-Instance"

type Instance struct {
	SchemaVersion int    `json:"schema_version"`
	PID           int    `json:"pid"`
	InstanceID    string `json:"instance_id"`
	Origin        string `json:"origin"`
	Version       string `json:"version"`
	DataDir       string `json:"data_dir"`
	Mode          string `json:"mode"`
	StartedAt     string `json:"started_at"`
}

type Status struct {
	Running  bool      `json:"running"`
	Instance *Instance `json:"instance,omitempty"`
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func canonicalDataDir(path string, create bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("ATM data directory is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if create {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", err
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) && !create {
		return path, nil
	}
	return resolved, err
}

func openInstanceLock(dataDir string) (*os.File, error) {
	runtimeDir := filepath.Join(dataDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(runtimeDir, "server.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock.Chmod(0o600); err != nil {
		lock.Close()
		return nil, err
	}
	if err := lockInstance(lock); err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}

// WithStoppedInstance holds the same lock as Start while explicit maintenance
// runs. Checking server.json alone would allow another process to start between
// the check and the maintenance operation. This does not stop any process.
func WithStoppedInstance(dataDir string, action func() error) error {
	dataDir, err := canonicalDataDir(dataDir, false)
	if err != nil {
		return err
	}
	lock, err := openInstanceLock(dataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	defer unlockInstance(lock)
	return action()
}

func writePrivateJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(content, '\n'))
}

func writePrivateFile(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func readInstance(dataDir string) (Instance, string, error) {
	dataDir, err := canonicalDataDir(dataDir, false)
	if err != nil {
		return Instance{}, "", err
	}
	content, err := os.ReadFile(filepath.Join(dataDir, "runtime", "server.json"))
	if err != nil {
		return Instance{}, "", err
	}
	var info Instance
	if err := json.Unmarshal(content, &info); err != nil {
		return info, "", fmt.Errorf("invalid ATM instance record: %w", err)
	}
	origin, err := url.Parse(info.Origin)
	if err != nil || origin.Scheme != "http" || origin.Hostname() != "127.0.0.1" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return info, "", errors.New("ATM instance record is not a loopback HTTP origin")
	}
	port, err := strconv.Atoi(origin.Port())
	if err != nil || port < 1 || port > 65535 || info.InstanceID == "" || info.DataDir != dataDir || info.SchemaVersion != 1 {
		return info, "", errors.New("ATM instance record does not match this data directory")
	}
	token, err := os.ReadFile(filepath.Join(dataDir, "runtime", "control.token"))
	if err != nil {
		return info, "", err
	}
	if len(strings.TrimSpace(string(token))) < 32 {
		return info, "", errors.New("invalid ATM control credential")
	}
	return info, strings.TrimSpace(string(token)), nil
}

func controlCall(ctx context.Context, info Instance, token, operation string, target any) error {
	method := http.MethodPost
	if operation == "status" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, info.Origin+"/api/v1/control/"+operation, bytes.NewBufferString("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(instanceHeader, info.InstanceID)
	req.Header.Set("Content-Type", "application/json")
	// Never leak the local capability through an environment proxy or redirect.
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ATM instance verification failed (HTTP %d)", resp.StatusCode)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, target)
}

func ReadStatus(ctx context.Context, dataDir string) (Status, error) {
	info, token, err := readInstance(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var verified Instance
	if err := controlCall(ctx, info, token, "status", &verified); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return Status{}, nil
		}
		return Status{}, fmt.Errorf("cannot verify ATM workspace at %s: %w", info.Origin, err)
	}
	if verified.InstanceID != info.InstanceID || verified.DataDir != info.DataDir || verified.Version != info.Version {
		return Status{}, errors.New("ATM workspace identity changed; try again")
	}
	return Status{Running: true, Instance: &verified}, nil
}

func OpenExisting(ctx context.Context, dataDir string) (string, error) {
	info, token, err := readInstance(dataDir)
	if err != nil {
		return "", err
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := controlCall(ctx, info, token, "open", &result); err != nil {
		return "", err
	}
	if !strings.HasPrefix(result.URL, info.Origin+"/#ticket=") {
		return "", errors.New("ATM returned an invalid browser URL")
	}
	return result.URL, nil
}

func Stop(ctx context.Context, dataDir string) error {
	info, token, err := readInstance(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var result struct {
		Stopping bool `json:"stopping"`
	}
	return controlCall(ctx, info, token, "stop", &result)
}
