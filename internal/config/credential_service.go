package config

import (
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
)

// MaxCredentialBytes is the largest text-model credential accepted by any
// adapter. Keeping the limit at the application boundary means the CLI and Web
// cannot quietly disagree about what is valid.
const MaxCredentialBytes = 64 << 10

// CredentialSaveInput is intentionally write-only. No service result contains
// APIKey, so neither JSON output nor an adapter log can accidentally echo it.
type CredentialSaveInput struct {
	APIKey string `json:"api_key"`
}

// CredentialStatus exposes only presence, never the saved credential.
type CredentialStatus struct {
	Configured bool `json:"configured"`
}

// CredentialStatus reports whether the built-in text model has a key saved on
// disk. Environment overrides are deliberately not included: this service owns
// the local credential configured by the browser workspace and CLI.
func (Service) CredentialStatus() (CredentialStatus, error) {
	configured, err := TextModelAPIKeyConfigured()
	if err != nil {
		return CredentialStatus{}, application.WrapError(
			application.CodeInternal,
			"read text-model credential status",
			err,
		)
	}
	return CredentialStatus{Configured: configured}, nil
}

// SaveCredential validates and atomically persists one local text-model key.
// The secret is never interpolated into an error message or returned value.
func (Service) SaveCredential(input CredentialSaveInput) (CredentialStatus, error) {
	if len(input.APIKey) > MaxCredentialBytes {
		err := application.NewError(
			application.CodeInvalidArgument,
			"DeepSeek API Key exceeds the maximum size",
		)
		err.Details = map[string]any{"field": "api_key", "max_bytes": MaxCredentialBytes}
		return CredentialStatus{}, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		err := application.NewError(application.CodeInvalidArgument, "DeepSeek API Key must not be empty")
		err.Details = map[string]any{"field": "api_key"}
		return CredentialStatus{}, err
	}
	if err := SaveTextModelAPIKey(input.APIKey); err != nil {
		return CredentialStatus{}, application.WrapError(
			application.CodeInternal,
			"save text-model credential",
			err,
		)
	}
	return CredentialStatus{Configured: true}, nil
}

// DeleteCredential removes the saved local key. Deleting an absent key remains
// idempotent and returns the same unconfigured status.
func (Service) DeleteCredential() (CredentialStatus, error) {
	if err := DeleteTextModelAPIKey(); err != nil {
		return CredentialStatus{}, application.WrapError(
			application.CodeInternal,
			"delete text-model credential",
			err,
		)
	}
	return CredentialStatus{Configured: false}, nil
}
