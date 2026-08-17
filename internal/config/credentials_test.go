package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempCredentialsDir(t *testing.T) string {
	t.Helper()
	oldAtmDir := AtmDir
	AtmDir = filepath.Join(t.TempDir(), ".atm")
	t.Cleanup(func() { AtmDir = oldAtmDir })
	return AtmDir
}

func TestTextModelCredentialRoundTripUsesPrivatePermissions(t *testing.T) {
	dir := withTempCredentialsDir(t)
	if err := SaveTextModelAPIKey("  secret-key\n"); err != nil {
		t.Fatal(err)
	}
	if shown := ShowConfig(); strings.Contains(shown, "secret-key") || strings.Contains(shown, "deepseek_api_key") {
		t.Fatalf("normal config output leaked credential metadata or value: %s", shown)
	}
	value, err := ReadTextModelAPIKey()
	if err != nil || value != "secret-key" {
		t.Fatalf("ReadTextModelAPIKey() = %q, %v", value, err)
	}
	configured, err := TextModelAPIKeyConfigured()
	if err != nil || !configured {
		t.Fatalf("TextModelAPIKeyConfigured() = %v, %v", configured, err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("data directory mode = %v, %v", dirInfo.Mode().Perm(), err)
	}
	fileInfo, err := os.Stat(CredentialsPath())
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %v, %v", fileInfo.Mode().Perm(), err)
	}
	if err := DeleteTextModelAPIKey(); err != nil {
		t.Fatal(err)
	}
	configured, err = TextModelAPIKeyConfigured()
	if err != nil || configured {
		t.Fatalf("configured after delete = %v, %v", configured, err)
	}
}

func TestTextModelCredentialRejectsBroadPermissions(t *testing.T) {
	withTempCredentialsDir(t)
	if err := os.MkdirAll(AtmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CredentialsPath(), []byte(`{"deepseek_api_key":"secret"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadTextModelAPIKey()
	if err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("permission error = %v", err)
	}
}

func TestTextModelCredentialRejectsMalformedFileWithoutLeakingContent(t *testing.T) {
	withTempCredentialsDir(t)
	if err := os.MkdirAll(AtmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CredentialsPath(), []byte(`not-json-secret`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadTextModelAPIKey()
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") || strings.Contains(err.Error(), "not-json-secret") {
		t.Fatalf("parse error = %v", err)
	}
}
