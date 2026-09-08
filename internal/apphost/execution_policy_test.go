package apphost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/quota"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
)

func writeExecutionSettings(t *testing.T, h *Host, values map[string]any) WorkspaceSettings {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := h.WorkspaceSettings(context.Background(), webCall())
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func awaitExecution(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("operation remained blocked by external work")
	}
}

func TestSettingsSaveDuringModelUsesOneExecutionPolicy(t *testing.T) {
	h := testHost(t)
	for _, key := range []string{"ATM_TEXT_MODEL_API_KEY", "DEEPSEEK_API_KEY", "ATM_TEXT_MODEL_BASE_URL", "ATM_TEXT_MODEL_MODEL"} {
		t.Setenv(key, "")
	}
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	type request struct{ Auth, Body, Endpoint string }
	requests := make(chan request, 3)
	answer := `{"choices":[{"message":{"content":"{\"title\":\"Refined task\",\"complexity\":\"simple\",\"children\":[]}"}}]}`
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{r.Header.Get("Authorization"), string(body), "old"}
		entered <- struct{}{}
		<-release
		fmt.Fprint(w, answer)
	}))
	defer func() { unblock(); oldServer.Close() }()
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{r.Header.Get("Authorization"), string(body), "new"}
		fmt.Fprint(w, answer)
	}))
	defer newServer.Close()
	settings := writeExecutionSettings(t, h, map[string]any{"text_model_base_url": oldServer.URL, "text_model_name": "old-model", "text_model_source": "old-source", "todo_refine_prompt": "old-instruction"})
	if _, err := h.saveWorkspaceCredential(context.Background(), webCall(), &config.CredentialSaveInput{APIKey: "old-key"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	run := func(ctx context.Context, count int, source string) error {
		for range count {
			_, got, err := refine.Propose(ctx, store.Todo{ID: "t1", Title: "task"}, "task text", refine.Options{})
			if err != nil {
				return err
			}
			if got != source {
				return fmt.Errorf("mixed source: %q", got)
			}
			if config.PolicyForExecution(ctx).DataDir != h.dataDir || config.AtmDir != h.dataDir {
				return fmt.Errorf("resident directory changed")
			}
		}
		return nil
	}
	go func() {
		done <- h.WithConfig(context.Background(), func(ctx context.Context) error { return run(ctx, 2, "old-source") })
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("model did not start")
	}
	updated := make(chan error, 1)
	go func() {
		name, source, prompt := "new-model", "new-source", "new-instruction"
		_, err := h.SaveWorkspaceBusiness(context.Background(), webCall(), WorkspaceBusinessInput{Revision: settings.Revision, SettingsPatch: config.SettingsPatch{TextModelBaseURL: &newServer.URL, TextModelName: &name, TextModelSource: &source, TodoRefinePrompt: &prompt}})
		if err == nil {
			_, err = h.saveWorkspaceCredential(context.Background(), webCall(), &config.CredentialSaveInput{APIKey: "new-key"})
		}
		if err == nil {
			_, err = h.SaveWorkspacePreferences(context.Background(), webCall(), WorkspacePreferencesInput{OwnerName: "During model"})
		}
		updated <- err
	}()
	awaitExecution(t, updated)
	unblock()
	awaitExecution(t, done)
	if err := h.WithConfig(context.Background(), func(ctx context.Context) error { return run(ctx, 1, "new-source") }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got := <-requests
		generation := "old"
		if i == 2 {
			generation = "new"
		}
		if got.Auth != "Bearer "+generation+"-key" || got.Endpoint != generation || !strings.Contains(got.Body, generation+"-model") || !strings.Contains(got.Body, generation+"-instruction") {
			t.Fatalf("request %d mixed execution policy: endpoint=%s", i, got.Endpoint)
		}
	}
}

// The real provider subprocess waits at a loopback test endpoint. It carries no
// credentials, and never consults a developer's installed provider or agent logs.
func TestSettingsProviderHelper(t *testing.T) {
	for i, value := range os.Args {
		if value != "--host-provider" || i+1 >= len(os.Args) {
			continue
		}
		response, err := http.Get(os.Args[i+1])
		if err != nil {
			os.Exit(2)
		}
		response.Body.Close()
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[{"id":"daily","agent":"claude","title":"fixture","observed_at":"2026-09-08T00:00:00Z","metrics":[{"id":"count","label":"Count","used":1,"limit":10}]}]}`)
		os.Exit(0)
	}
}

func TestSettingsSaveDuringProviderKeepsCapturedRegistry(t *testing.T) {
	h := testHost(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { entered <- struct{}{}; <-release; w.WriteHeader(200) }))
	defer func() { unblock(); server.Close() }()
	writeExecutionSettings(t, h, map[string]any{"quota_providers": map[string]config.QuotaProviderConfig{"fixture": {Command: os.Args[0], Args: []string{"-test.run=TestSettingsProviderHelper", "--", "--host-provider", server.URL}}}})
	done := make(chan error, 1)
	go func() {
		done <- h.WithConfig(context.Background(), func(ctx context.Context) error {
			for range 2 {
				got, err := quota.Default.Snapshot(ctx, webCall(), quota.Input{Agent: "claude"})
				if err != nil {
					return err
				}
				if len(got.Warnings) != 0 || got.Agents["claude"] == nil || len(got.Agents["claude"].ProviderCards) != 1 {
					return fmt.Errorf("provider registry changed during execution: %+v", got.Warnings)
				}
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	updated := make(chan error, 1)
	go func() {
		// External CLI edits can remove a provider while its current call runs.
		if err := os.WriteFile(h.configPath, []byte(`{"quota_providers":{}}`), 0600); err != nil {
			updated <- err
			return
		}
		_, err := h.SaveWorkspacePreferences(context.Background(), webCall(), WorkspacePreferencesInput{OwnerName: "During provider"})
		updated <- err
	}()
	awaitExecution(t, updated)
	unblock()
	awaitExecution(t, done)
	if err := h.WithConfig(context.Background(), func(ctx context.Context) error {
		if len(config.PolicyForExecution(ctx).Providers) != 0 {
			return fmt.Errorf("new job retained removed provider")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type policyConnector struct{ entered, release chan struct{} }

func (policyConnector) ID() string { return "fixture" }
func (c policyConnector) Fetch(ctx context.Context, _ store.CollectionSource, _ int64) ([]collector.Message, int64, error) {
	close(c.entered)
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
	if config.PolicyForExecution(ctx).OwnerName != "Before collector" {
		return nil, 0, fmt.Errorf("collector policy changed")
	}
	return nil, 0, nil
}

func TestSettingsSaveDuringCollectionFetch(t *testing.T) {
	h := testHost(t)
	writeExecutionSettings(t, h, map[string]any{"owner_name": "Before collector"})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertCollectionSource(db, store.CollectionSource{Connector: "fixture", Kind: "chat", ExternalID: "fixture", Name: "fixture", Enabled: true})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	c := policyConnector{make(chan struct{}), make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(c.release) }) }
	defer unblock()
	registry, err := collector.NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- h.WithConfig(context.Background(), func(ctx context.Context) error {
			service := collector.ServiceForExecution(ctx)
			service.Connectors = registry
			_, err := service.Run(ctx, "")
			return err
		})
	}()
	select {
	case <-c.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not start")
	}
	updated := make(chan error, 1)
	go func() {
		_, err := h.SaveWorkspacePreferences(context.Background(), webCall(), WorkspacePreferencesInput{OwnerName: "During collector"})
		updated <- err
	}()
	awaitExecution(t, updated)
	unblock()
	awaitExecution(t, done)
}

func TestExternalFailureAndPanicRestoreConfigurationGate(t *testing.T) {
	h := testHost(t)
	for _, panicInside := range []bool{false, true} {
		func() {
			defer func() {
				if recovered := recover(); (recovered != nil) != panicInside {
					t.Fatalf("unexpected panic: %v", recovered)
				}
			}()
			_ = h.WithConfig(context.Background(), func(ctx context.Context) error {
				return config.External(ctx, func(context.Context) error {
					if !h.gate.TryLock() {
						t.Fatal("external callback retained configuration gate")
					}
					h.gate.Unlock()
					if panicInside {
						panic("fixture")
					}
					return context.Canceled
				})
			})
		}()
		if !h.gate.TryLock() {
			t.Fatal("configuration gate not balanced after external work")
		}
		h.gate.Unlock()
	}
}
