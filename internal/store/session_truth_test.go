package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/parser"
)

type versionedTestAgent struct {
	path       string
	parseCalls int
}

func (agent *versionedTestAgent) Name() string                                 { return "versioned-test" }
func (agent *versionedTestAgent) Discover() []string                           { return []string{agent.path} }
func (agent *versionedTestAgent) LiveSessions(time.Duration) []parser.Session  { return nil }
func (agent *versionedTestAgent) ParseAppend(string, int64) *parser.ParsedFile { return nil }
func (agent *versionedTestAgent) ParseFile(string) *parser.ParsedFile {
	agent.parseCalls++
	return &parser.ParsedFile{
		SessionID: "versioned-session", ShortID: "versioned", Agent: agent.Name(),
		CreatedTS: 100, LastTS: 100,
		Inputs: []parser.Message{{Content: "question", TS: 100}},
	}
}

func TestSessionTruthPersistsLineageLocalTurnsAndStructuredResult(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID:       "rollout-child",
		ShortID:         "child",
		Agent:           "codex",
		Project:         "atm",
		CreatedTS:       100,
		LastTS:          110,
		ResumeID:        "child-thread",
		RootSessionID:   "root-thread",
		ParentSessionID: "parent-thread",
		AgentPath:       "/root/parser",
		AgentNickname:   "Ada",
		SubagentDepth:   1,
		IsSubagent:      true,
		ResultStatus:    parser.SessionResultCompleted,
		LatestProgress:  "ran tests",
		FinalResult:     "all fixed",
		Messages: []parser.TranscriptMessage{
			{Role: "user", Content: "Message Type: NEW_TASK", TS: 101,
				Scope: parser.MessageScopeControl, Kind: parser.MessageKindControl},
			{Role: "user", Content: "real local question", TS: 102,
				Scope: parser.MessageScopeLocal, Kind: parser.MessageKindConversation},
			{Role: "assistant", Content: "checking", TS: 103,
				Scope: parser.MessageScopeLocal, Kind: parser.MessageKindProgress, Phase: "commentary"},
			{Role: "assistant", Content: "ran tests", TS: 104,
				Scope: parser.MessageScopeLocal, Kind: parser.MessageKindProgress, Phase: "commentary"},
			{Role: "assistant", Content: "all fixed", TS: 105,
				Scope: parser.MessageScopeLocal, Kind: parser.MessageKindFinal, Phase: "final_answer"},
		},
		Tools: map[string]int{"exec_command": 1},
	}
	path := filepath.Join(t.TempDir(), "child.jsonl")
	if err := upsertSession(db, parsed, path, "codex", 1, 2); err != nil {
		t.Fatal(err)
	}

	listed, err := ListSessions(db, 0, 200, "codex", "")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %#v err=%v", listed, err)
	}
	row := listed[0]
	if row.QCount != 1 || row.FirstQ != "real local question" || row.RootSessionID != "root-thread" ||
		row.ParentSessionID != "parent-thread" || row.AgentPath != "/root/parser" ||
		row.AgentNickname != "Ada" || row.ContentState != parser.ContentStateAvailable ||
		row.ResultStatus != parser.SessionResultCompleted || row.ParserVersion != parser.CurrentSessionParserVersion {
		t.Fatalf("row = %#v", row)
	}
	if hits, err := SearchMessages(db, "NEW_TASK", "codex"); err != nil || len(hits) != 0 {
		t.Fatalf("control search hits = %#v err=%v", hits, err)
	}
	if hits, err := SearchMessages(db, "all fixed", "codex"); err != nil || len(hits) != 1 {
		t.Fatalf("final search hits = %#v err=%v", hits, err)
	}

	shown, err := GetSession(db, "child")
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Turns) != 1 || shown.Turns[0].Question != "real local question" ||
		shown.Turns[0].Final != "all fixed" || len(shown.Turns[0].Progress) != 2 {
		t.Fatalf("turns = %#v", shown.Turns)
	}
	if len(shown.Outputs) != 1 || shown.Outputs[0] != "all fixed" {
		t.Fatalf("outputs = %#v", shown.Outputs)
	}
}

func TestStoreClassifiesHarnessEnvelopesBeforeIndexingAnyAgent(t *testing.T) {
	db := openTempDB(t)
	control := &parser.ParsedFile{
		SessionID: "control-session", ShortID: "control", Agent: "claude", CreatedTS: 100, LastTS: 101,
		Messages: []parser.TranscriptMessage{
			{Role: "user", Content: "<app-context>internal only</app-context>", TS: 100},
			{Role: "assistant", Content: "control acknowledgement", TS: 101},
		},
	}
	if err := upsertSession(db, control, filepath.Join(t.TempDir(), "control.jsonl"), "claude", 1, 2); err != nil {
		t.Fatal(err)
	}
	wrapped := &parser.ParsedFile{
		SessionID: "wrapped-session", ShortID: "wrapped", Agent: "claude", CreatedTS: 110, LastTS: 111,
		Messages: []parser.TranscriptMessage{
			{Role: "user", Content: "<app-context>internal</app-context>\n## My request:\nreal request", TS: 110},
			{Role: "assistant", Content: "real answer", TS: 111},
		},
	}
	if err := upsertSession(db, wrapped, filepath.Join(t.TempDir(), "wrapped.jsonl"), "claude", 1, 2); err != nil {
		t.Fatal(err)
	}

	listed, err := ListSessions(db, 0, 200, "claude", "")
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %#v err=%v", listed, err)
	}
	byID := map[string]ListResult{}
	for _, row := range listed {
		byID[row.FullID] = row
	}
	if row := byID["control-session"]; row.QCount != 0 || row.ContentState != parser.ContentStateControlOnly {
		t.Fatalf("control row = %#v", row)
	}
	if row := byID["wrapped-session"]; row.QCount != 1 || row.FirstQ != "real request" {
		t.Fatalf("wrapped row = %#v", row)
	}
	if hits, err := SearchMessages(db, "control acknowledgement", "claude"); err != nil || len(hits) != 0 {
		t.Fatalf("paired control answer leaked: hits=%#v err=%v", hits, err)
	}
}

func TestFreshSchemaContainsSessionTruthColumns(t *testing.T) {
	db := openTempDB(t)
	want := map[string]bool{
		"resume_id": false, "root_session_id": false, "parent_session_id": false,
		"agent_path": false, "agent_nickname": false, "subagent_depth": false,
		"is_subagent": false, "is_internal": false, "parser_version": false, "content_state": false,
		"result_status": false, "latest_progress": false, "final_result": false,
	}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('sessions')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for column, found := range want {
		if !found {
			t.Errorf("sessions.%s missing", column)
		}
	}
	messageColumns := map[string]bool{"scope": false, "kind": false, "phase": false}
	messageRows, err := db.Query(`SELECT name FROM pragma_table_info('messages')`)
	if err != nil {
		t.Fatal(err)
	}
	defer messageRows.Close()
	for messageRows.Next() {
		var name string
		if err := messageRows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if _, ok := messageColumns[name]; ok {
			messageColumns[name] = true
		}
	}
	for column, found := range messageColumns {
		if !found {
			t.Errorf("messages.%s missing", column)
		}
	}
}

func TestSyncReparsesUnchangedSourceWhenParserVersionIsStale(t *testing.T) {
	db := openTempDB(t)
	path := filepath.Join(t.TempDir(), "versioned.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &versionedTestAgent{path: path}
	if _, err := syncAgent(db, agent); err != nil {
		t.Fatal(err)
	}
	if agent.parseCalls != 1 {
		t.Fatalf("first sync parse calls = %d", agent.parseCalls)
	}
	if _, err := syncAgent(db, agent); err != nil {
		t.Fatal(err)
	}
	if agent.parseCalls != 1 {
		t.Fatalf("unchanged current source reparsed: calls=%d", agent.parseCalls)
	}
	if _, err := db.Exec(`UPDATE sessions SET parser_version=0 WHERE id='versioned-session'`); err != nil {
		t.Fatal(err)
	}
	if _, err := syncAgent(db, agent); err != nil {
		t.Fatal(err)
	}
	if agent.parseCalls != 2 {
		t.Fatalf("stale parser source was not rebuilt: calls=%d", agent.parseCalls)
	}
}
