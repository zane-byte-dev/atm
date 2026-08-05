package parser

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestQoderCLIParseFile(t *testing.T) {
	oldProjects := config.QoderCLIProjects
	root := t.TempDir()
	config.QoderCLIProjects = root
	t.Cleanup(func() { config.QoderCLIProjects = oldProjects })

	path := filepath.Join(root, "-tmp-my-project", "transcript", "12345678-abcd.jsonl")
	writeJSONL(t, path,
		`{"type":"session_meta","timestamp":"2026-07-17T01:00:00Z","cwd":"/tmp/my-project","sessionId":"12345678-abcd"}`,
		`{"type":"user","timestamp":"2026-07-17T01:00:01Z","cwd":"/tmp/my-project","message":{"role":"user","content":[{"type":"text","text":"Implement the feature"}]}}`,
		`{"type":"assistant","timestamp":"2026-07-17T01:00:02Z","cwd":"/tmp/my-project","message":{"role":"assistant","model":"qoder-model","content":[{"type":"thinking","thinking":"hidden"},{"type":"tool_use","id":"tool-1","name":"Read","input":{}},{"type":"tool_use","id":"tool-2","name":"Skill","input":{"skill":"a1"}},{"type":"text","text":"Implemented."}],"usage":{"input_tokens":20,"output_tokens":5,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}`,
	)

	got := QoderCLIParseFile(path)
	if got == nil {
		t.Fatal("QoderCLIParseFile returned nil")
	}
	if got.SessionID != "qodercli:12345678-abcd" || got.Agent != "qodercli" || got.Project != "my-project" {
		t.Fatalf("metadata = %#v", got)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "Implement the feature" || got.Messages[1].Content != "Implemented." {
		t.Fatalf("messages = %#v", got.Messages)
	}
	if got.Tools["Read"] != 1 || got.Usage.Model != "qoder-model" || got.Usage.InputTokens != 20 || got.Usage.OutputTokens != 5 || got.Usage.RequestCount != 1 {
		t.Fatalf("tools=%#v usage=%#v", got.Tools, got.Usage)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "a1" {
		t.Fatalf("skills = %#v", got.Skills)
	}
}

// seedQoderDB builds the two tables QoderParseFile reads, in the shape the real
// Qoder client writes them.
func seedQoderDB(t *testing.T, messages ...string) {
	t.Helper()
	oldDB := config.QoderDB
	dbPath := filepath.Join(t.TempDir(), "local.db")
	config.QoderDB = dbPath
	t.Cleanup(func() { config.QoderDB = oldDB })

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	statements := append([]string{
		`CREATE TABLE chat_session (session_id TEXT PRIMARY KEY, session_title TEXT,
			project_name TEXT, mode TEXT, gmt_create INTEGER, gmt_modified INTEGER,
			last_user_query_at INTEGER)`,
		`CREATE TABLE chat_message (id TEXT PRIMARY KEY, session_id TEXT, role TEXT,
			content TEXT, token_info TEXT, model_info TEXT, gmt_create INTEGER)`,
		`INSERT INTO chat_session VALUES ('sess-1','Corpus session','atm','agent',100000,140000,140000)`,
	}, messages...)
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestQoderParseFileKeepsLongUserPrompts is the regression for the gap t188 was
// opened on: Qoder inlines IDE context into the prompt, so real user messages
// start around a kilobyte. The parser used to skip anything over 500 bytes, and
// because the storage layer pairs outputs to inputs by index, dropping the
// inputs silently discarded every assistant reply too — five sessions and 331
// upstream messages indexed as zero.
func TestQoderParseFileKeepsLongUserPrompts(t *testing.T) {
	longPrompt := strings.Repeat("上下文", 400) // ~1200 bytes, like a real Qoder prompt
	seedQoderDB(t,
		`INSERT INTO chat_message VALUES ('m1','sess-1','user','`+longPrompt+`','','',110000)`,
		`INSERT INTO chat_message VALUES ('m2','sess-1','assistant','Implemented it.',
			'{"prompt_tokens":1200,"completion_tokens":340,"cached_tokens":900}',
			'{"model_key":"qwen-max"}',120000)`,
		`INSERT INTO chat_message VALUES ('m3','sess-1','tool','tool output','','',130000)`,
	)

	got := QoderParseFile("qoder://sess-1")
	if got == nil {
		t.Fatal("QoderParseFile returned nil for a session that has messages")
	}
	if len(got.Inputs) != 1 {
		t.Fatalf("long user prompt was dropped instead of truncated: inputs = %#v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Content != "Implemented it." {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	// Truncated, not stored whole: the index is for search, not archival.
	if len([]rune(got.Inputs[0].Content)) > 2000 {
		t.Errorf("input was not truncated: %d runes", len([]rune(got.Inputs[0].Content)))
	}
	if !strings.HasPrefix(got.Inputs[0].Content, "上下文") {
		t.Error("truncation cut into the start of the prompt")
	}
	// Messages, not just Inputs/Outputs: an agent run answers one prompt with
	// several assistant messages, and the index-pairing fallback would keep only
	// as many replies as there were prompts.
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %#v", got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[1].Role != "assistant" {
		t.Errorf("roles out of order: %#v", got.Messages)
	}
	if got.Tools["tool_call"] != 1 {
		t.Errorf("tools = %#v", got.Tools)
	}
	if got.Usage.InputTokens != 300 || got.Usage.OutputTokens != 340 || got.Usage.CacheReadTokens != 900 {
		t.Errorf("usage = %#v", got.Usage)
	}
	if got.Usage.Model != "qoder-qwen-max" {
		t.Errorf("model = %q", got.Usage.Model)
	}
}

// Base64-looking payloads are still skipped: they are stored blobs, not prose,
// and indexing them would fill search with noise.
func TestQoderParseFileStillSkipsEncryptedContent(t *testing.T) {
	blob := strings.Repeat("aGVsbG8+d29ybGQ/", 80) // long, and full of + / =
	seedQoderDB(t,
		`INSERT INTO chat_message VALUES ('m1','sess-1','user','`+blob+`','','',110000)`,
		`INSERT INTO chat_message VALUES ('m2','sess-1','assistant','ok',
			'{"prompt_tokens":10,"completion_tokens":2}','{"model_key":"qwen-max"}',120000)`,
	)
	got := QoderParseFile("qoder://sess-1")
	if got == nil {
		t.Fatal("QoderParseFile returned nil")
	}
	if len(got.Inputs) != 0 {
		t.Errorf("an encrypted-looking blob was indexed: %#v", got.Inputs)
	}
}

// The counterpart to the blob case, and the reason the heuristic was rewritten:
// six occurrences of `+ / =` used to be enough to discard a message, and a single
// line mentioning two file paths and a couple of commands clears that on its own.
func TestQoderParseFileKeepsProseContainingSlashesAndPluses(t *testing.T) {
	// Deliberately over the old threshold of five marks, and unmistakably prose.
	reply := "改完了：internal/cmd/backup.go 和 internal/store/backup.go 都过了，" +
		"命令是 go test ./... + go vet ./... + make build，覆盖率 a/b/c 三块。"
	seedQoderDB(t,
		`INSERT INTO chat_message VALUES ('m1','sess-1','user','`+strings.Repeat("上下文", 400)+`','','',110000)`,
		`INSERT INTO chat_message VALUES ('m2','sess-1','assistant','`+reply+`',
			'{"prompt_tokens":10,"completion_tokens":2}','{"model_key":"qwen-max"}',120000)`,
	)
	got := QoderParseFile("qoder://sess-1")
	if got == nil {
		t.Fatal("QoderParseFile returned nil")
	}
	if len(got.Outputs) != 1 {
		t.Fatalf("prose with slashes and pluses was treated as an encrypted blob: %#v", got.Outputs)
	}
}

func TestLooksEncryptedDistinguishesBlobsFromProse(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"base64 blob", strings.Repeat("aGVsbG8+d29ybGQ/", 20), true},
		{"prose with marks", "run go test ./... + go vet ./... and check a/b/c = ok, then ship", false},
		{"chinese prose", "改完了：internal/cmd/backup.go + internal/store/backup.go 都过了 a/b/c", false},
		{"short string", "a+b/c=d", false},
		{"long identifier without padding", strings.Repeat("abcdef0123456789", 8), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := looksEncrypted(testCase.text); got != testCase.want {
				t.Errorf("looksEncrypted = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestQoderWorkParseFile(t *testing.T) {
	oldDB := config.QoderWorkDB
	dbPath := filepath.Join(t.TempDir(), "agents.db")
	config.QoderWorkDB = dbPath
	t.Cleanup(func() { config.QoderWorkDB = oldDB })

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT, path TEXT)`,
		`CREATE TABLE chats (id TEXT PRIMARY KEY, name TEXT, project_id TEXT, created_at INTEGER, updated_at INTEGER, deleted_at INTEGER)`,
		`CREATE TABLE sub_chats (id TEXT PRIMARY KEY, name TEXT, chat_id TEXT, mode TEXT, model_level TEXT, created_at INTEGER, updated_at INTEGER)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, chat_id TEXT, sub_chat_id TEXT, sequence INTEGER, role TEXT, parts TEXT, metadata TEXT, searchable_text TEXT, created_at INTEGER, updated_at INTEGER)`,
		`INSERT INTO projects VALUES ('p1','atm','/tmp/atm')`,
		`INSERT INTO chats VALUES ('c1','Main task','p1',100,140,NULL)`,
		`INSERT INTO sub_chats VALUES ('s1','Implementation','c1','agent','qwork-auto',101,140)`,
		`INSERT INTO messages VALUES ('m1','c1','s1',1,'user','[{"type":"text","text":"Build it"}]','{}','Build it',110,110)`,
		`INSERT INTO messages VALUES ('m2','c1','s1',2,'assistant','[{"type":"tool-Read","toolUseId":"t1"},{"type":"tool-Read","toolUseId":"t1"},{"type":"tool-Skill","toolUseId":"t2","input":{"skill":"atm"}},{"type":"text","text":"Done"}]','{"inputTokens":12,"outputTokens":4}','Done',120,120)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	discovered := DiscoverQoderWork()
	if len(discovered) != 1 || discovered[0] != "qoderwork://s1" {
		t.Fatalf("discovered = %#v", discovered)
	}
	got := QoderWorkParseFile(discovered[0])
	if got == nil {
		t.Fatal("QoderWorkParseFile returned nil")
	}
	if got.SessionID != "qoderwork:s1" || got.Agent != "qoderwork" || got.Project != "atm" || got.Summary != "Implementation" {
		t.Fatalf("metadata = %#v", got)
	}
	if len(got.Messages) != 2 || got.Tools["Read"] != 1 || got.Usage.InputTokens != 12 || got.Usage.OutputTokens != 4 {
		t.Fatalf("messages=%#v tools=%#v usage=%#v", got.Messages, got.Tools, got.Usage)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "atm" {
		t.Fatalf("skills = %#v", got.Skills)
	}
}

func TestDiscoverQoderCLICoversBothLayouts(t *testing.T) {
	oldProjects := config.QoderCLIProjects
	root := t.TempDir()
	config.QoderCLIProjects = root
	t.Cleanup(func() { config.QoderCLIProjects = oldProjects })
	paths := []string{filepath.Join(root, "project-a", "one.jsonl"), filepath.Join(root, "project-b", "transcript", "two.jsonl")}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got := DiscoverQoderCLI()
	if len(got) != 2 || got[0] != paths[0] || got[1] != paths[1] {
		t.Fatalf("discovered = %#v", got)
	}
}
