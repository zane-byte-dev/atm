package background

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

func TestConcurrentTaskAccountingBypassesGlobalSinkAndFlushes(t *testing.T) {
	dir := testData(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"ok\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":20}}`)
	}))
	defer server.Close()
	t.Setenv(textmodel.APIKeyEnv, "fixture-key")
	t.Setenv(textmodel.BaseURLEnv, server.URL)
	t.Setenv(textmodel.ModelEnv, "deepseek-test")
	oldSink := textmodel.Sink
	var global atomic.Int32
	textmodel.Sink = func(textmodel.Call) { global.Add(1) }
	t.Cleanup(func() { textmodel.Sink = oldSink })
	var wg sync.WaitGroup
	for _, id := range []string{"first-job", "second-job"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithUsage(context.Background(), dir, id, func(ctx context.Context) error {
				_, err := textmodel.Run(ctx, textmodel.TaskTodoRefine, time.Second, `{"type":"object"}`, "fixture")
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if global.Load() != 0 {
		t.Fatal("task calls leaked to CLI process buffer")
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sessions, events, input, cache int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT session_id),COUNT(*),SUM(input_tokens),SUM(cache_read_tokens) FROM usage_events`).Scan(&sessions, &events, &input, &cache); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 || events != 2 || input != 160 || cache != 40 {
		t.Fatalf("accounting=%d sessions %d events %d input %d cache", sessions, events, input, cache)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "model-usage-pending"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("journal not cleared: %v %v", entries, err)
	}
}

func TestAccountingRecoversCrashJournalWithoutDuplicateUsage(t *testing.T) {
	dir := testData(t)
	r := &usageRecorder{dataDir: dir, id: "crashed-job"}
	r.record(textmodel.Call{Task: "collection", Model: "fixture-model", Usage: textmodel.Usage{InputTokens: 20, OutputTokens: 5}, StartedAt: time.Now()})
	if r.err != nil {
		t.Fatal(r.err)
	}
	r.file.Close()
	path := filepath.Join(dir, "model-usage-pending", "crashed-job.jsonl")
	journal, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecoverUsage(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after SQLite committed, before unlinking the journal.
	if err := os.WriteFile(path, journal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverUsage(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, input int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(input_tokens) FROM usage_events WHERE session_id='atm-crashed-job'`).Scan(&count, &input); err != nil {
		t.Fatal(err)
	}
	if count != 1 || input != 20 {
		t.Fatalf("recovery double-counted count=%d input=%d", count, input)
	}
}

func TestOperationWithoutModelCallCreatesNoJournal(t *testing.T) {
	dir := testData(t)
	if err := WithUsage(context.Background(), dir, "no-model", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "model-usage-pending")); !os.IsNotExist(err) {
		t.Fatalf("unused recorder touched disk: %v", err)
	}
}

func TestAccountingRecoversCompleteRecordsBeforeInterruptedTail(t *testing.T) {
	dir := testData(t)
	r := &usageRecorder{dataDir: dir, id: "partial-job"}
	r.record(textmodel.Call{Task: "collection", Model: "fixture", Usage: textmodel.Usage{InputTokens: 25}, StartedAt: time.Now()})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, err := r.file.WriteString(`{"Task":"collection",`); err != nil {
		t.Fatal(err)
	}
	r.file.Close()
	if err := RecoverUsage(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "model-usage-pending", "partial-job.jsonl.incomplete")); err != nil {
		t.Fatal("interrupted tail was not retained", err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var input int
	if err := db.QueryRow(`SELECT input_tokens FROM usage WHERE session_id='atm-partial-job'`).Scan(&input); err != nil || input != 25 {
		t.Fatalf("recovered input=%d err=%v", input, err)
	}
}
