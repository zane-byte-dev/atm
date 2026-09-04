package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"

	"github.com/zane-byte-dev/atm/internal/application"
)

// ReloadRevision applies exactly the snapshot whose revision is returned. A
// resident process must not pair stale effective settings with a newer file's
// revision, which would permit a stale editor to overwrite a CLI change.
// Reading a missing configuration remains side-effect free.
func (Service) ReloadRevision() (string, error) {
	data, err := os.ReadFile(ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		ResetBusinessDefaults()
		applyEnvOverrides()
		return configRevision(map[string]any{})
	}
	if err != nil {
		return "", unavailableConfigSaveError(err)
	}
	var raw map[string]any
	var cfg FileConfig
	if json.Unmarshal(data, &raw) != nil || raw == nil || json.Unmarshal(data, &cfg) != nil {
		return "", application.NewError(application.CodeConflict, "configuration file contains invalid JSON")
	}
	// Removing a setting from a CLI-edited file restores its default. Applying
	// only present fields would leave an old collector/model policy running.
	ResetBusinessDefaults()
	applyFileConfig(cfg)
	applyEnvOverrides()
	return configRevision(raw)
}

// ApplyRevision compares the expected file revision under the same advisory
// lock used by CLI writes, then atomically patches only the supplied settings.
func (service Service) ApplyRevision(expected string, patch SettingsPatch) error {
	if expected == "" {
		return application.NewError(application.CodeInvalidArgument, "settings revision is required")
	}
	values := patch.values()
	if len(values) == 0 {
		return application.NewError(application.CodeInvalidArgument, "no settings given")
	}
	normalized, err := normalizeSettings(values)
	if err != nil {
		return err
	}
	return mutateRawConfig(func(raw map[string]any) error {
		current, err := configRevision(raw)
		if err != nil {
			return err
		}
		if current != expected {
			return application.NewError(application.CodeConflict, "settings changed in another window or process; reload before saving")
		}
		for key, value := range normalized {
			raw[key] = value
		}
		return nil
	})
}

func configRevision(raw map[string]any) (string, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
