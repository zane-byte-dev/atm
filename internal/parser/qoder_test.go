package parser

import (
	"database/sql"
	"os"
	"path/filepath"
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
