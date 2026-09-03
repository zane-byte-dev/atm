package apphost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/session"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestActivityRejectsUnboundedReadsAndSideEffectFlags(t *testing.T) {
	h := testHost(t)
	for _, test := range []struct{ method, input string }{
		{"session.list", `{"sync_before_read":true}`},
		{"session.list", `{"limit":101}`},
		{"session.list", `{"offset":10001}`},
		{"session.list", `{"days":366}`},
		{"session.search", `{"keyword":"x","offset":1001}`},
		{"session.search", `{"keyword":""}`},
		{"session.show", `{"session_id":"s1","include_thinking":true}`},
		{"session.show", `{"session_id":"s1","limit":51}`},
		{"session.show", `{"session_id":"../../transcript"}`},
		{"session.show", `{"session_id":"s1","max_chars":0}`},
		{"session.status", `{"path":"/tmp"}`},
		{"usage.snapshot", `{"range":"all"}`},
		{"usage.snapshot", `{"sync":true}`},
		{"quota.cached", `{"live":true}`},
	} {
		t.Run(test.method+test.input, func(t *testing.T) {
			_, err := h.callActivity(context.Background(), webCall(), test.method, json.RawMessage(test.input))
			var appErr *application.Error
			if !errors.As(err, &appErr) || appErr.Code != application.CodeInvalidArgument {
				t.Fatalf("expected invalid argument; got %v", err)
			}
		})
	}
	if _, err := os.Stat(config.AtmDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validation created index: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.callActivity(ctx, webCall(), "session.status", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
}

func TestActivityMissingIndexDoesNotCreateIt(t *testing.T) {
	h := testHost(t)
	for _, method := range []string{"session.list", "session.status", "quota.cached"} {
		if _, err := h.callActivity(context.Background(), webCall(), method, nil); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	if _, err := h.callActivity(context.Background(), webCall(), "usage.snapshot", nil); !errors.Is(err, store.ErrDatabaseMissing) {
		t.Fatalf("missing usage index: %v", err)
	}
	entries, err := os.ReadDir(config.AtmDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read materialized files: %v, %v", entries, err)
	}
}

func TestActivityReadsSchema54WithoutChangingDatabaseOrFiles(t *testing.T) {
	h := testHost(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute).Unix()
	for _, item := range []struct {
		id, project string
		at          int64
	}{{"web-session-a", "atm", now}, {"web-session-b", "other", now - 3600}} {
		_, err = db.Exec(`INSERT INTO sessions(id,short_id,agent,project,file_path,created_at,created_ts,last_ts,summary) VALUES(?,?,?,?,?,?,?,?,?)`, item.id, item.id, "codex", item.project, filepath.Join(config.AtmDir, "absent-source.jsonl"), "2026-09-03", item.at, item.at, "Indexed session")
		if err != nil {
			t.Fatal(err)
		}
	}
	for seq, content := range []string{"needle first", strings.Repeat("界", 17000), "needle second", "second answer", "needle third", "third answer"} {
		role := "user"
		if seq%2 != 0 {
			role = "assistant"
		}
		if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts) VALUES(?,?,?,?,?)`, "web-session-a", seq, role, content, now+int64(seq)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO todos(id,position,title,status,priority,project,created) VALUES('t1',0,'Finished task','done','P2','atm','2026-09-03')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO todo_session_bindings(session_id,todo_id,agent,project,cwd,bound_at) VALUES('web-session-a','t1','codex','atm','/private/work',?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events(session_id,model,ts,input_tokens,output_tokens,cache_create_tokens,cache_read_tokens,cost_usd,fingerprint,request_count) VALUES(?,?,?,?,?,?,?,?,?,?)`, "web-session-a", "gpt-5", now, 100, 20, 5, 7, 0.01, "activity-usage", 1); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordQuotaSamples(db, []store.QuotaSample{
		{Agent: "codex", WindowMinutes: 300, UsedPercent: 20, ResetsAt: now + 3600, TS: now - 3600},
		{Agent: "codex", WindowMinutes: 300, UsedPercent: 45, ResetsAt: now + 3600, TS: now},
		{Agent: "codex", WindowMinutes: 10080, UsedPercent: 70, ResetsAt: now - 10, TS: now - 700},
	}, time.Unix(now, 0)); err != nil {
		t.Fatal(err)
	}
	// v55 only added this table. Exercise the exact legacy schema without ever
	// asking the host to migrate it.
	for _, statement := range []string{`DROP TABLE work_create_idempotency`, `UPDATE schema_version SET version=54`, `PRAGMA wal_checkpoint(TRUNCATE)`, `PRAGMA journal_mode=DELETE`} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	beforeFiles, _ := os.ReadDir(config.AtmDir)
	aliases := config.ProjectAliases
	invoke := func(method, input string) any {
		t.Helper()
		value, err := h.callActivity(context.Background(), webCall(), method, json.RawMessage(input))
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return value
	}
	listed := invoke("session.list", `{"limit":1,"days":7}`).(session.ListResult)
	if listed.Total != 2 || len(listed.Sessions) != 1 || listed.Sessions[0].ID != "web-session-a" {
		t.Fatalf("recent page=%+v", listed)
	}
	filtered := invoke("session.list", `{"project":"other","days":7}`).(session.ListResult)
	if filtered.Total != 1 || filtered.Sessions[0].Project != "other" {
		t.Fatalf("project filter=%+v", filtered)
	}
	searched := invoke("session.search", `{"keyword":"needle","limit":1,"offset":1}`).(SessionSearchPage)
	if searched.Total != 3 || searched.Returned != 1 || searched.Offset != 1 || utf8.RuneCountInString(searched.Matches[0].Content) > 600 {
		t.Fatalf("search page=%+v", searched)
	}
	shown := invoke("session.show", `{"session_id":"web-session-a","limit":2}`).(SessionPage)
	if len(shown.QA) != 2 || shown.TotalTurns != 3 || !shown.ContentLimited || shown.QA[1].A != "second answer" {
		t.Fatalf("transcript page=%+v", shown)
	}
	for _, qa := range shown.QA {
		count := utf8.RuneCountInString(qa.Q + qa.A + strings.Join(qa.Progress, ""))
		if count > 16000 {
			t.Fatalf("turn response exceeded bound: %d", count)
		}
	}
	next := invoke("session.show", `{"session_id":"web-session-a","offset":2,"limit":2}`).(SessionPage)
	if len(next.QA) != 1 || next.QA[0].A != "third answer" {
		t.Fatalf("next transcript page=%+v", next)
	}
	status := invoke("session.status", `{}`).(SessionStatus)
	if status.Health.SchemaVersion != 54 || status.Health.IndexedSessions != 2 || len(status.Agents) != 1 || len(status.Projects) != 2 {
		t.Fatalf("status=%+v", status)
	}
	if len(status.Bindings) != 1 || status.Bindings[0].State != "todo_not_in_progress" || status.Bindings[0].Binding.CWD != "" {
		t.Fatalf("binding status=%+v", status.Bindings)
	}
	usage := invoke("usage.snapshot", `{"range":"last_7_days"}`).(dashboard.Snapshot)
	if len(usage.Ranges["last_7_days"].ModelStats) != 1 || len(usage.Ranges["last_7_days"].Sessions) != 0 {
		t.Fatalf("usage=%+v", usage)
	}
	quota := invoke("quota.cached", `{}`).(CachedQuota)
	if len(quota.Windows) != 2 || quota.Windows[0].UsedPercent != 45 || quota.Windows[0].Stale || !quota.Windows[1].ResetElapsed || !quota.Windows[1].Stale {
		t.Fatalf("cached quota=%+v", quota)
	}
	after, err := os.ReadFile(config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("activity reads modified database bytes")
	}
	afterFiles, _ := os.ReadDir(config.AtmDir)
	if !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Fatalf("activity reads created/changed files: before=%v after=%v", beforeFiles, afterFiles)
	}
	if !reflect.DeepEqual(aliases, config.ProjectAliases) {
		t.Fatal("activity reads mutated project configuration")
	}
}
