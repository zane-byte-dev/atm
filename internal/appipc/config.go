package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

func registerConfig(registry *ipc.Registry, dependencies Dependencies) {
	bindNoRequest(registry, "config.settings", func(
		context.Context,
		application.Call,
	) (config.Settings, error) {
		return dependencies.Config.Snapshot()
	})
	bind(registry, "config.save", func(
		_ context.Context,
		_ application.Call,
		patch config.SettingsPatch,
	) (config.Settings, error) {
		return dependencies.Config.Apply(patch)
	})
	bind(registry, "config.credential.save", func(
		_ context.Context,
		_ application.Call,
		input config.CredentialSaveInput,
	) (config.CredentialStatus, error) {
		return dependencies.Config.SaveCredential(input)
	})
	bindNoRequest(registry, "config.credential.delete", func(
		context.Context,
		application.Call,
	) (config.CredentialStatus, error) {
		return dependencies.Config.DeleteCredential()
	})
	bind(registry, "config.text_model.check", func(
		ctx context.Context,
		_ application.Call,
		input textmodel.ConnectionCheckInput,
	) (textmodel.CheckResult, error) {
		return dependencies.CheckTextModel(ctx, input)
	})
}
