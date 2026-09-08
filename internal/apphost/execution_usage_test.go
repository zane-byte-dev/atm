package apphost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

func TestBackgroundModelLogsStayInCapturedDirectoryDuringSettingsReload(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%t", failed), func(t *testing.T) {
			h := testHost(t)
			other := t.TempDir()
			entered, release, recorded := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if failed {
					http.Error(w, "private provider response", http.StatusServiceUnavailable)
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
			}))
			t.Cleanup(func() { unblock(); server.Close() })
			t.Setenv(textmodel.APIKeyEnv, "fixture-key")
			t.Setenv(textmodel.BaseURLEnv, server.URL)
			t.Setenv(textmodel.ModelEnv, "fixture-model")
			completed := make(chan background.Job, 2)
			manager, err := background.New(background.Options{DataDir: h.dataDir, WithConfig: h.WithConfig,
				Execute: func(ctx context.Context, _ application.Call, _ background.Request, _ func(string)) (any, error) {
					// The built-in refinement/collection adapters have this outer
					// boundary. Their model's usage sink runs before it reacquires
					// the host gate, even though the HTTP client has its own boundary.
					err := config.External(ctx, func(ctx context.Context) error {
						_, err := textmodel.Run(ctx, textmodel.TaskCheck, 3*time.Second, `{"type":"object"}`, "fixture")
						close(recorded)
						return err
					})
					return nil, err
				},
				OnChange: func(job background.Job) {
					if job.Terminal() {
						completed <- job
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				unblock()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := manager.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			if err := manager.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Run(t.Context(), webCall(), background.Request{Kind: background.QuotaRefresh}, "logging-policy"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("background model did not start")
			}

			data, err := json.Marshal(map[string]string{"data_dir": other})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(h.configPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			func() {
				h.gate.Lock()
				defer h.gate.Unlock()
				defer h.restoreDataPaths()
				// Hold the real settings reload between ReloadRevision and the
				// resident path restore. This makes routing to mutable AtmDir fail
				// deterministically; repeated reloads also exercise concurrent
				// writes while the actual model usage sink finishes under -race.
				if _, err := config.Default.ReloadRevision(); err != nil {
					t.Fatal(err)
				}
				unblock()
				deadline := time.After(3 * time.Second)
				for {
					select {
					case <-recorded:
						return
					case <-deadline:
						t.Fatal("model usage sink waited for the configuration gate")
					default:
						if _, err := config.Default.ReloadRevision(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}()

			select {
			case job := <-completed:
				want := "succeeded"
				if failed {
					want = "failed"
				}
				if job.Status != want {
					t.Fatalf("background result = %+v", job)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("background model did not finish")
			}
			lines, err := logging.Tail(filepath.Join(h.dataDir, "logs", "cli.log"), 0)
			if err != nil || len(lines) != 1 || !strings.Contains(lines[0], "builtin_model_call") {
				t.Fatalf("captured model log = %v, %v", lines, err)
			}
			if strings.Contains(lines[0], "private provider response") || strings.Contains(lines[0], "fixture-key") {
				t.Fatalf("model log included private response or credential: %s", lines[0])
			}
			if _, err := os.Stat(filepath.Join(other, "logs")); !os.IsNotExist(err) {
				t.Fatalf("model log followed reloaded data_dir: %v", err)
			}
		})
	}
}
