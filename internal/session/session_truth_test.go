package session

import (
	"context"
	"testing"

	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestShowPairsSeveralAssistantMessagesWithTheirActualTurn(t *testing.T) {
	fixture := newServiceFixture(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM messages WHERE session_id = 'session-recent-full'`); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		seq           int
		role, content string
		scope, kind   string
	}{
		{0, "user", "first question", "local", "conversation"},
		{1, "assistant", "checking", "local", "progress"},
		{2, "assistant", "first final", "local", "final"},
		{3, "assistant", "intermediate duplicate", "local", "conversation"},
		{4, "user", "second question", "local", "conversation"},
		{5, "assistant", "second answer", "local", "conversation"},
		{6, "user", "Message Type: NEW_TASK", "control", "control"},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO messages
			(session_id,seq,role,content,ts,scope,kind) VALUES(?,?,?,?,?,?,?)`,
			"session-recent-full", row.seq, row.role, row.content,
			fixture.createdTS+int64(row.seq), row.scope, row.kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE sessions SET resume_id='child-thread', root_session_id='root-thread',
		parent_session_id='parent-thread', agent_path='/root/child', agent_nickname='Ada',
		subagent_depth=1, is_subagent=1, parser_version=?, content_state='available',
		result_status='completed', latest_progress='checking', final_result='first final'
		WHERE id='session-recent-full'`, parser.CurrentSessionParserVersion); err != nil {
		t.Fatal(err)
	}

	result, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalTurns != 2 || len(result.QA) != 2 {
		t.Fatalf("turn counts = total %d qa %#v", result.TotalTurns, result.QA)
	}
	if result.QA[0].Q != "first question" || result.QA[0].A != "first final" ||
		len(result.QA[0].Progress) != 1 || result.QA[1].Q != "second question" ||
		result.QA[1].A != "second answer" {
		t.Fatalf("qa = %#v", result.QA)
	}
	if result.RootSessionID != "root-thread" || result.ParentSessionID != "parent-thread" ||
		result.AgentPath != "/root/child" || result.AgentNickname != "Ada" ||
		result.ContentState == "" || result.ResultStatus != parser.SessionResultCompleted ||
		result.FinalResult != "first final" {
		t.Fatalf("metadata = %#v", result)
	}
}
