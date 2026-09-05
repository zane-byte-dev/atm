package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	browserKeyName         = "browser.key"
	browserSessionLifetime = 12 * time.Hour
	maxBrowserCookieBytes  = 2048
)

type browserSession struct {
	csrf      string
	issuedAt  time.Time
	expiresAt time.Time
}

type browserSessionClaims struct {
	Version   int    `json:"v"`
	CSRF      string `json:"csrf"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func browserCookieName(dataDir string) string {
	digest := sha256.Sum256([]byte(dataDir))
	return "atm_session_" + hex.EncodeToString(digest[:8])
}

func loadOrCreateBrowserKey(runtimeDir string) ([]byte, error) {
	keyPath := filepath.Join(runtimeDir, browserKeyName)
	info, err := os.Lstat(keyPath)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("browser credential is not a regular file")
		}
		if info.Mode().Perm() != 0o600 {
			if err := os.Chmod(keyPath, 0o600); err != nil {
				return nil, fmt.Errorf("protect browser credential: %w", err)
			}
		}
		content, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(content)))
		if err != nil || len(key) != 32 {
			return nil, errors.New("invalid persistent browser credential")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	key, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(key) != 32 {
		return nil, errors.New("cannot create persistent browser credential")
	}
	if err := writePrivateFile(keyPath, []byte(token+"\n")); err != nil {
		return nil, err
	}
	return key, nil
}

func newBrowserSession(now time.Time) (browserSession, error) {
	csrf, err := randomToken()
	if err != nil {
		return browserSession{}, err
	}
	now = now.UTC()
	return browserSession{csrf: csrf, issuedAt: now, expiresAt: now.Add(browserSessionLifetime)}, nil
}

func encodeBrowserSession(key []byte, session browserSession) (string, error) {
	claims := browserSessionClaims{
		Version:   1,
		CSRF:      session.csrf,
		IssuedAt:  session.issuedAt.Unix(),
		ExpiresAt: session.expiresAt.Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeBrowserSession(key []byte, value string, now time.Time) (browserSession, bool) {
	if len(value) == 0 || len(value) > maxBrowserCookieBytes {
		return browserSession{}, false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return browserSession{}, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return browserSession{}, false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return browserSession{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return browserSession{}, false
	}
	var claims browserSessionClaims
	if len(payload) > 1024 || json.Unmarshal(payload, &claims) != nil {
		return browserSession{}, false
	}
	csrf, err := base64.RawURLEncoding.DecodeString(claims.CSRF)
	if claims.Version != 1 || err != nil || len(csrf) != 32 || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt {
		return browserSession{}, false
	}
	issuedAt := time.Unix(claims.IssuedAt, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	if expiresAt.Sub(issuedAt) > browserSessionLifetime || now.Before(issuedAt.Add(-time.Minute)) || !now.Before(expiresAt) {
		return browserSession{}, false
	}
	return browserSession{csrf: claims.CSRF, issuedAt: issuedAt, expiresAt: expiresAt}, true
}
