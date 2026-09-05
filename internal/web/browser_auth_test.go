package web

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistentBrowserKeyIsStableAndPrivate(t *testing.T) {
	runtimeDir := t.TempDir()
	first, err := loadOrCreateBrowserKey(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateBrowserKey(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || !bytes.Equal(first, second) {
		t.Fatal("browser key was not stable across loads")
	}
	info, err := os.Stat(filepath.Join(runtimeDir, browserKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("browser key permissions = %o", info.Mode().Perm())
	}
}

func TestBrowserSessionSignatureExpiryAndWorkspaceCookieName(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	now := time.Unix(1_800_000_000, 0).UTC()
	session, err := newBrowserSession(now)
	if err != nil {
		t.Fatal(err)
	}
	value, err := encodeBrowserSession(key, session)
	if err != nil {
		t.Fatal(err)
	}
	decoded, ok := decodeBrowserSession(key, value, now.Add(time.Hour))
	if !ok || decoded.csrf != session.csrf || !decoded.expiresAt.Equal(session.expiresAt) {
		t.Fatalf("valid session did not round trip: %+v, ok=%v", decoded, ok)
	}
	tampered := []byte(value)
	tampered[0] ^= 1
	if _, ok := decodeBrowserSession(key, string(tampered), now); ok {
		t.Fatal("tampered browser session was accepted")
	}
	if _, ok := decodeBrowserSession(key, value, session.expiresAt); ok {
		t.Fatal("expired browser session was accepted")
	}
	if browserCookieName("/tmp/one") == browserCookieName("/tmp/two") {
		t.Fatal("different data directories share a browser cookie name")
	}
}

func TestPersistentBrowserKeyRejectsSymlink(t *testing.T) {
	runtimeDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside-key")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(runtimeDir, browserKeyName)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateBrowserKey(runtimeDir); err == nil {
		t.Fatal("symlink browser key was accepted")
	}
}
