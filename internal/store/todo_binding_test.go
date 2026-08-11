package store

import (
	"testing"
)

func TestTodoSessionBindingLifecyclePreservesHistory(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "First"), openTodo("t2", "Second"))

	first, err := BindTodoSession(TodoSessionBinding{SessionID: "session-1", TodoID: "t1", Agent: "codex", Project: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	if first.TodoID != "t1" || first.UnboundAt != nil {
		t.Fatalf("first binding = %#v", first)
	}
	second, err := BindTodoSession(TodoSessionBinding{SessionID: "session-1", TodoID: "t2", Agent: "codex", Project: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	if second.TodoID != "t2" {
		t.Fatalf("second binding = %#v", second)
	}
	current, err := CurrentTodoBinding("session-1")
	if err != nil || current == nil || current.TodoID != "t2" {
		t.Fatalf("current = %#v, err=%v", current, err)
	}
	history, err := ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].UnboundAt == nil || history[0].Reason != "rebound" {
		t.Fatalf("binding history = %#v", history)
	}
	changed, err := UnbindTodoSession("session-1", "done")
	if err != nil || !changed {
		t.Fatalf("unbind changed=%v err=%v", changed, err)
	}
	current, err = CurrentTodoBinding("session-1")
	if err != nil || current != nil {
		t.Fatalf("current after unbind = %#v, err=%v", current, err)
	}
}

func TestUnbindTodoSessionsClosesAllActiveBindings(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Shared"))
	for _, sessionID := range []string{"s1", "s2"} {
		if _, err := BindTodoSession(TodoSessionBinding{SessionID: sessionID, TodoID: "t1"}); err != nil {
			t.Fatal(err)
		}
	}
	count, err := UnbindTodoSessions("t1", "waiting")
	if err != nil || count != 2 {
		t.Fatalf("unbind count=%d err=%v", count, err)
	}
}

func TestListActiveTodoSessionBindingsExcludesHistory(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "First"), openTodo("t2", "Second"))
	if _, err := BindTodoSession(TodoSessionBinding{SessionID: "s1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindTodoSession(TodoSessionBinding{SessionID: "s2", TodoID: "t2"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := UnbindTodoSession("s1", "done"); err != nil || !changed {
		t.Fatalf("unbind changed=%v err=%v", changed, err)
	}

	bindings, err := ListActiveTodoSessionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].SessionID != "s2" {
		t.Fatalf("active bindings = %#v", bindings)
	}
}

func TestFindSessionsForTodoUsesBindingsAndKeepsUnindexedSessions(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Exact session history"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO sessions
		(id,short_id,agent,project,file_path,created_at,created_ts,summary,last_ts)
		VALUES('rollout-2026-08-03-indexed-session','indexed1','codex','atm','/tmp/indexed.jsonl','',90,'Indexed work',400)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts) VALUES
		('rollout-2026-08-03-indexed-session',0,'user','first',110),
		('rollout-2026-08-03-indexed-session',1,'assistant','answer',120),
		('rollout-2026-08-03-indexed-session',2,'user','second',310)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tools(session_id,name,count) VALUES('rollout-2026-08-03-indexed-session','exec',3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage
		(session_id,input_tokens,output_tokens,cost_usd) VALUES('rollout-2026-08-03-indexed-session',100,20,0.25)`); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]any{
		{"indexed-session", "t1", "codex", "atm", "/tmp/old", int64(100), int64(200), "manual"},
		{"missing-session-123", "t1", "pi", "atm", "/tmp/missing", int64(250), int64(260), "done"},
		{"indexed-session", "t1", "codex", "atm", "/tmp/new", int64(300), nil, ""},
	} {
		if _, err := db.Exec(`INSERT INTO todo_session_bindings
			(session_id,todo_id,agent,project,cwd,bound_at,unbound_at,reason)
			VALUES(?,?,?,?,?,?,?,?)`, args...); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := FindSessionsForTodo(db, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %#v", sessions)
	}
	indexed := sessions[0]
	if indexed.SessionID != "indexed-session" || !indexed.Indexed || indexed.BindingCount != 2 ||
		indexed.FirstBoundAt != 100 || indexed.BoundAt != 300 || indexed.UnboundAt != nil ||
		indexed.CWD != "/tmp/new" || indexed.Queries != 2 || indexed.ToolCalls != 3 || indexed.CostUSD != 0.25 {
		t.Fatalf("indexed session = %#v", indexed)
	}
	// The ledger id is not the index id for codex, so callers that need to read
	// the transcript get the resolved one alongside the session's own span.
	if indexed.IndexedID != "rollout-2026-08-03-indexed-session" ||
		indexed.StartedAt != 90 || indexed.LastAt != 400 {
		t.Fatalf("indexed session identity = %#v", indexed)
	}
	// The last assistant message travels with the binding: it is what a Todo
	// shows as the run's outcome once the session leaves the live-status window,
	// and live status is the only other place that text exists.
	if indexed.LatestResult != "answer" {
		t.Fatalf("indexed latest result = %q", indexed.LatestResult)
	}
	if missing := sessions[1]; missing.LatestResult != "" {
		t.Fatalf("an unindexed session cannot have a result: %q", missing.LatestResult)
	}
	missing := sessions[1]
	if missing.SessionID != "missing-session-123" || missing.Indexed || missing.ShortID != "missing-" ||
		missing.Agent != "pi" || missing.UnboundAt == nil || missing.Reason != "done" {
		t.Fatalf("missing session = %#v", missing)
	}
	if missing.IndexedID != "" || missing.StartedAt != 0 || missing.LastAt != 0 {
		t.Fatalf("unindexed session identity = %#v", missing)
	}
}

func TestEarliestUserMessagesReturnsOldestPromptsPerSession(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO sessions(id,short_id,agent,file_path) VALUES
		('s1','s1','codex','/tmp/s1.jsonl'),
		('s2','s2','codex','/tmp/s2.jsonl')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts) VALUES
		('s1',0,'user','preamble',10),
		('s1',1,'assistant','answer',20),
		('s1',2,'user','real ask',30),
		('s1',3,'user','later ask',40),
		('s2',0,'user','other session',50)`); err != nil {
		t.Fatal(err)
	}

	messages, err := EarliestUserMessages(db, []string{"s1", "s2", "", "s3"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := messages["s1"]; len(got) != 2 || got[0] != "preamble" || got[1] != "real ask" {
		t.Fatalf("s1 messages = %#v", got)
	}
	if got := messages["s2"]; len(got) != 1 || got[0] != "other session" {
		t.Fatalf("s2 messages = %#v", got)
	}
	if _, ok := messages["s3"]; ok {
		t.Fatalf("unknown session should be absent: %#v", messages)
	}
}

func TestMatchTodosPrioritizesProjectThenQuery(t *testing.T) {
	tf := &TodoFile{Items: []Todo{
		{ID: "t1", Title: "Wanda video integration", Project: "wanda", Priority: "P0", Status: TodoStatusInProgress},
		{ID: "t2", Title: "Agent 会话绑定 ATM TODO", Description: "session todo binding", Project: "atm", Priority: "P1", Status: TodoStatusOpen},
		{ID: "t3", Title: "ATM knowledge cleanup", Project: "atm", Priority: "P2", Status: TodoStatusOpen},
	}}
	results := MatchTodos(tf, "atm", "实现会话绑定", 3)
	if len(results) != 2 || results[0].ID != "t2" || results[1].ID != "t3" {
		t.Fatalf("project matches = %#v", results)
	}
	results = MatchTodos(tf, "unknown", "Wanda video", 3)
	if len(results) != 1 || results[0].ID != "t1" {
		t.Fatalf("semantic fallback = %#v", results)
	}
	if results := MatchTodos(tf, "unknown", "", 3); len(results) != 0 {
		t.Fatalf("empty fallback should not return global noise: %#v", results)
	}
}

// Startup injection and duplicate checking want opposite things from the same
// ranking: injection would rather offer an extra candidate than miss one, while
// deciding whether to create a todo needs "nothing matches" to be expressible.
// Being in the current project is worth +100, so without a floor on the query's
// own contribution every active todo in the repo comes back for any query at all.
func TestMatchTodosWithOptionsSeparatesDedupFromStartupInjection(t *testing.T) {
	tf := &TodoFile{Items: []Todo{
		{ID: "t1", Title: "配额只有当下百分比，没有变化速度", Project: "atm", Priority: "P1", Status: TodoStatusOpen},
		{ID: "t2", Title: "ATM knowledge cleanup", Project: "atm", Priority: "P2", Status: TodoStatusOpen},
		{ID: "t3", Title: "Wanda video integration", Project: "wanda", Priority: "P0", Status: TodoStatusInProgress},
	}}

	// Startup injection: unchanged, still offers the project's todos.
	if got := MatchTodos(tf, "atm", "完全无关的目标", 3); len(got) == 0 {
		t.Fatal("startup injection stopped offering candidates; missing one costs more than an extra")
	}

	dedup := func(query string) []TodoMatch {
		return MatchTodosWithOptions(tf, TodoMatchOptions{
			Project: "atm", Query: query, Limit: 3,
			MinQueryScore: TodoDedupMinQueryScore, AllProjects: true,
		})
	}
	if got := dedup("完全无关的目标"); len(got) != 0 {
		t.Fatalf("dedup returned candidates for an unrelated goal: %#v", got)
	}
	hits := dedup("配额只有当下百分比，没有变化速度")
	if len(hits) != 1 || hits[0].ID != "t1" {
		t.Fatalf("dedup on an existing title = %#v", hits)
	}
	// QueryScore is reported separately, so a caller can set its own floor rather
	// than reverse-engineering one out of the project and priority bonuses.
	if hits[0].QueryScore >= hits[0].Score {
		t.Fatalf("query score %d should be a part of total %d", hits[0].QueryScore, hits[0].Score)
	}
	// A duplicate filed under another project is still a duplicate.
	if got := dedup("Wanda video integration"); len(got) != 1 || got[0].ID != "t3" {
		t.Fatalf("dedup missed a cross-project duplicate: %#v", got)
	}
}
