package config

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// ExecutionPolicy is a private, in-memory generation of the settings used by
// outbound work. Callers capture it while the resident configuration is locked.
// Maps are copied and credentials never enter the JSON/job contracts.
type ExecutionPolicy struct {
	Settings
	DataDir       string
	Location      *time.Location
	Providers     map[string]QuotaProviderConfig
	Connectors    map[string]CollectionConnectorConfig
	credential    string
	credentialErr error
}

func CaptureExecutionPolicy() *ExecutionPolicy {
	p := &ExecutionPolicy{
		Settings: Settings{OwnerName: OwnerName, GrokLiveQuota: GrokLiveQuota,
			CollectionEnabled: CollectionEnabled, CollectionIntervalMinutes: CollectionIntervalMinutes,
			CollectionLookbackMinutes: CollectionLookbackMinutes, CollectionMessageRetentionDays: CollectionMessageRetentionDays,
			TextModelBaseURL: TextModelBaseURL, TextModelName: TextModelName, TextModelSource: TextModelSource,
			TodoRefinePrompt: TodoRefinePrompt, TodoRefineOnAdd: TodoRefineOnAdd},
		DataDir: AtmDir, Location: Loc, Providers: cloneExecutionMap(QuotaProviders), Connectors: cloneExecutionMap(CollectionConnectors),
	}
	for _, name := range []string{"ATM_TEXT_MODEL_API_KEY", "DEEPSEEK_API_KEY"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			p.credential = value
			break
		}
	}
	if p.credential == "" {
		p.credential, p.credentialErr = ReadTextModelAPIKey()
	}
	if value := strings.TrimSpace(os.Getenv("ATM_TEXT_MODEL_BASE_URL")); value != "" {
		p.TextModelBaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ATM_TEXT_MODEL_MODEL")); value != "" {
		p.TextModelName = value
	}
	return p
}

func cloneExecutionMap[T any](value map[string]T) map[string]T {
	data, _ := json.Marshal(value)
	var cloned map[string]T
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func (policy *ExecutionPolicy) ModelCredential() (string, error) {
	return policy.credential, policy.credentialErr
}

type executionContextKey struct{}
type externalContextKey struct{}
type executionContext struct {
	policy   *ExecutionPolicy
	external func(context.Context, func(context.Context) error) error
	gate     chan struct{}
}

func WithExecutionPolicy(ctx context.Context, policy *ExecutionPolicy, external func(context.Context, func(context.Context) error) error) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, executionContextKey{}, executionContext{policy: policy, external: external, gate: make(chan struct{}, 1)})
}

func PolicyForExecution(ctx context.Context) *ExecutionPolicy {
	if ctx == nil {
		ctx = context.Background()
	}
	if value, ok := ctx.Value(executionContextKey{}).(executionContext); ok {
		return value.policy
	}
	return CaptureExecutionPolicy()
}

// External releases the resident configuration gate only around synchronous
// outbound work. The callback must use policy/captured arguments, never mutable
// package settings. Parallel providers are joined inside one callback; nested
// outbound calls receive the marked context and cannot release the gate twice.
// Sibling external calls are serialized. Callers must not run concurrent local
// work on this execution context while an external callback is in flight.
// Local persistence resumes under the gate and rechecks current business state.
func External(ctx context.Context, run func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(externalContextKey{}) != nil {
		return run(ctx)
	}
	value, ok := ctx.Value(executionContextKey{}).(executionContext)
	if !ok || value.external == nil {
		return run(ctx)
	}
	select {
	case value.gate <- struct{}{}:
		defer func() { <-value.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return value.external(context.WithValue(ctx, externalContextKey{}, true), run)
}
