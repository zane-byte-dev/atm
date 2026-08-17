package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const CredentialsFileName = "credentials.json"

type credentialsFile struct {
	DeepSeekAPIKey string `json:"deepseek_api_key,omitempty"`
}

// CredentialsPath is a function because tests and alternate installations can
// redirect AtmDir after package initialization.
func CredentialsPath() string {
	return filepath.Join(AtmDir, CredentialsFileName)
}

// ReadTextModelAPIKey reads ATM's local DeepSeek credential. The file is kept
// separate from config.json so config display, backups and diagnostic exports
// cannot accidentally acquire the secret.
func ReadTextModelAPIKey() (string, error) {
	credentials, err := readCredentials()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(credentials.DeepSeekAPIKey), nil
}

func TextModelAPIKeyConfigured() (bool, error) {
	value, err := ReadTextModelAPIKey()
	return value != "", err
}

func SaveTextModelAPIKey(rawValue string) error {
	value := strings.TrimSpace(rawValue)
	if value == "" {
		return fmt.Errorf("DeepSeek API Key must not be empty")
	}
	return writeCredentials(credentialsFile{DeepSeekAPIKey: value})
}

func DeleteTextModelAPIKey() error {
	err := os.Remove(CredentialsPath())
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("delete credentials file: %w", err)
}

func readCredentials() (credentialsFile, error) {
	path := CredentialsPath()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return credentialsFile{}, nil
	}
	if err != nil {
		return credentialsFile{}, fmt.Errorf("inspect credentials file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return credentialsFile{}, fmt.Errorf("credentials path is not a regular file: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return credentialsFile{}, fmt.Errorf("credentials file permissions are too broad (%#o): run chmod 600 %s", info.Mode().Perm(), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return credentialsFile{}, fmt.Errorf("read credentials file: %w", err)
	}
	var credentials credentialsFile
	if err := json.Unmarshal(data, &credentials); err != nil {
		return credentialsFile{}, fmt.Errorf("credentials file is not valid JSON: %w", err)
	}
	return credentials, nil
}

func writeCredentials(credentials credentialsFile) error {
	if err := os.MkdirAll(AtmDir, 0o700); err != nil {
		return fmt.Errorf("create ATM data directory: %w", err)
	}
	if err := os.Chmod(AtmDir, 0o700); err != nil {
		return fmt.Errorf("secure ATM data directory: %w", err)
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials file: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(AtmDir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary credentials file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary credentials file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary credentials file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary credentials file: %w", err)
	}
	if err := os.Rename(temporaryPath, CredentialsPath()); err != nil {
		return fmt.Errorf("replace credentials file: %w", err)
	}
	if err := os.Chmod(CredentialsPath(), 0o600); err != nil {
		return fmt.Errorf("secure credentials file: %w", err)
	}
	return nil
}
