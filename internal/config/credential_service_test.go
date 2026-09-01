package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestCredentialServiceNeverReturnsOrLeaksTheSecret(t *testing.T) {
	withTempCredentialsDir(t)
	const secret = "sk-app-secret-that-must-not-escape"

	status, err := Default.SaveCredential(CredentialSaveInput{APIKey: "  " + secret + "\n"})
	if err != nil || !status.Configured {
		t.Fatalf("SaveCredential() = %+v, %v", status, err)
	}
	payload, err := json.Marshal(status)
	if err != nil || string(payload) != `{"configured":true}` || strings.Contains(string(payload), secret) {
		t.Fatalf("credential result = %s, %v", payload, err)
	}
	value, err := ReadTextModelAPIKey()
	if err != nil || value != secret {
		t.Fatalf("saved value = %q, %v", value, err)
	}
	status, err = Default.CredentialStatus()
	if err != nil || !status.Configured {
		t.Fatalf("CredentialStatus() = %+v, %v", status, err)
	}
	status, err = Default.DeleteCredential()
	if err != nil || status.Configured {
		t.Fatalf("DeleteCredential() = %+v, %v", status, err)
	}
}

func TestCredentialServiceValidatesAtTheApplicationBoundary(t *testing.T) {
	withTempCredentialsDir(t)
	for _, testCase := range []struct {
		name  string
		value string
	}{
		{name: "empty", value: " \n\t "},
		{name: "too large", value: strings.Repeat("x", MaxCredentialBytes+1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Default.SaveCredential(CredentialSaveInput{APIKey: testCase.value})
			if !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("SaveCredential() error = %v, want invalid_argument", err)
			}
			if strings.Contains(err.Error(), testCase.value) {
				t.Fatal("validation error echoed the credential")
			}
		})
	}
}
