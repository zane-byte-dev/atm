package background

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

// WithUsage gives one operation its own content-free model-usage journal. Each
// completed call is synced before it leaves the recorder; completion flushes it
// into usage tables. A crash leaves the journal for recovery, never a replay of
// the model call. The directory is durable user data, outside runtime/ caches.
// Call this while the host's configuration read gate is held.
func WithUsage(ctx context.Context, dataDir, id string, fn func(context.Context) error) error {
	operationErr, flushErr := runWithUsage(ctx, dataDir, id, fn)
	return errors.Join(operationErr, flushErr)
}

// runWithUsage keeps the operation result separate from the accounting result.
// The background manager can therefore retry only the durable accounting write
// after a transient SQLite failure, without inviting a replay of the external
// or model operation that already completed.
func runWithUsage(ctx context.Context, dataDir, id string, fn func(context.Context) error) (operationErr, flushErr error) {
	if ctx == nil || fn == nil {
		return invalid("context and model usage operation are required"), nil
	}
	if !safeUsageID(id) {
		return invalid("invalid model usage operation ID"), nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	recorder := &usageRecorder{dataDir: dataDir, id: id, stop: cancel}
	operationErr = fn(textmodel.WithSink(ctx, recorder.record))
	flushErr = recorder.flush()
	return operationErr, flushErr
}

type usageRecorder struct {
	mu          sync.Mutex
	dataDir, id string
	file        *os.File
	err         error
	stop        context.CancelFunc
}

func (r *usageRecorder) record(call textmodel.Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if r.err != nil && r.stop != nil {
			r.stop()
		}
	}()
	fields := map[string]any{"task": call.Task, "model": call.Model, "input": call.Usage.InputTokens, "output": call.Usage.OutputTokens, "cache_hit": call.Usage.CacheHitTokens, "duration_ms": call.DurationMS}
	if call.Err == "" {
		logging.Lifecycle("builtin_model_call", fields)
	} else {
		logging.Failure("builtin_model_call", "background", errors.New("built-in model request failed"), fields)
	}
	if !call.Usage.Reported() {
		return
	}
	if r.err != nil {
		return
	}
	if r.file == nil {
		dir := filepath.Join(r.dataDir, "model-usage-pending")
		if err := os.MkdirAll(dir, 0700); err != nil {
			r.err = err
			return
		}
		var err error
		r.file, err = os.OpenFile(filepath.Join(dir, r.id+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			r.err = err
			return
		}
		parent, err := os.Open(dir)
		if err != nil {
			r.err = err
			return
		}
		r.err = parent.Sync()
		parent.Close()
		if r.err != nil {
			return
		}
	}
	ts := call.StartedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	record := store.BuiltinModelCall{Task: call.Task, Model: call.Model, InputTokens: call.Usage.InputTokens, OutputTokens: call.Usage.OutputTokens, CacheHitTokens: call.Usage.CacheHitTokens, DurationMS: call.DurationMS, TS: ts.Unix(), OK: call.Err == ""}
	if err := json.NewEncoder(r.file).Encode(record); err != nil {
		r.err = err
		return
	}
	r.err = r.file.Sync()
}

func (r *usageRecorder) flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return r.err
	}
	closeErr := r.file.Close()
	if r.err != nil || closeErr != nil {
		return errors.Join(r.err, closeErr)
	}
	return flushUsageFile(context.Background(), r.dataDir, r.id)
}

// RecoverUsage finishes only accounting writes; it never reissues network
// requests. RecordBuiltinUsage is idempotent for this stable journal/session ID.
func RecoverUsage(ctx context.Context, dataDir string) error {
	dir := filepath.Join(dataDir, "model-usage-pending")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		if !safeUsageID(id) {
			continue
		}
		if err := flushUsageFile(ctx, dataDir, id); err != nil {
			return err
		}
	}
	return nil
}

func flushUsageFile(ctx context.Context, dataDir, id string) error {
	path := filepath.Join(dataDir, "model-usage-pending", id+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 32*1024*1024 {
		return fmt.Errorf("model usage journal is not a bounded regular file")
	}
	var last [1]byte
	endsWithNewline := info.Size() == 0
	if info.Size() > 0 {
		if _, err := f.ReadAt(last[:], info.Size()-1); err != nil {
			return err
		}
		endsWithNewline = last[0] == '\n'
	}
	scanner := bufio.NewScanner(io.LimitReader(f, 32*1024*1024+1))
	scanner.Buffer(make([]byte, 4096), 64*1024)
	var calls []store.BuiltinModelCall
	var readBytes int64
	incomplete := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var call store.BuiltinModelCall
		readBytes += int64(len(scanner.Bytes()) + 1)
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			if !endsWithNewline && readBytes >= info.Size() {
				incomplete = true
				break
			}
			return fmt.Errorf("model usage journal is incomplete: %w", err)
		}
		calls = append(calls, call)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(calls) > 0 {
		db, err := store.Open()
		if err != nil {
			return err
		}
		err = store.RecordBuiltinUsage(db, "atm-"+id, calls)
		db.Close()
		if err != nil {
			return err
		}
	}
	if incomplete {
		// A process can die between the JSON write and its newline/fsync. Recover
		// complete records only, and retain the damaged tail for inspection.
		logging.Failure("builtin_model_usage_incomplete", "background", errors.New("interrupted usage journal tail retained"), map[string]any{"recovered_calls": len(calls)})
		return os.Rename(path, path+".incomplete")
	}
	return os.Remove(path)
}

func safeUsageID(id string) bool {
	if len(id) < 1 || len(id) > 160 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
