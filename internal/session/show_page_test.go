package session

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type pageMessage struct {
	role    string
	content string
	scope   string
	kind    string
}

func seedPageMessages(t *testing.T, fixture serviceFixture, messages []pageMessage) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM messages WHERE session_id = 'session-recent-full'`); err != nil {
		t.Fatal(err)
	}
	for index, message := range messages {
		if message.scope == "" {
			message.scope = "local"
		}
		if message.kind == "" {
			message.kind = "conversation"
		}
		// Gaps keep page boundaries independent of any assumption that seq is
		// also a row count or a turn number.
		seq := 7 + index*3
		if _, err := db.Exec(`INSERT INTO messages
			(session_id,seq,role,content,ts,scope,kind) VALUES(?,?,?,?,?,?,?)`,
			"session-recent-full", seq, message.role, message.content,
			fixture.createdTS+int64(seq), message.scope, message.kind); err != nil {
			t.Fatal(err)
		}
	}
}

func assertShowPageMatchesFull(t *testing.T, service Service, full ShowResult, offset, limit int) {
	t.Helper()
	got, err := service.ShowPage(context.Background(), PageInput{
		SessionID: "recent", Offset: offset, Limit: limit,
	})
	if err != nil {
		t.Fatalf("show page offset=%d limit=%d: %v", offset, limit, err)
	}
	start := min(offset, len(full.QA))
	end := min(start+limit, len(full.QA))
	want := full
	want.QA = append([]QA{}, full.QA[start:end]...)
	want.ReturnedTurns = len(want.QA)
	want.Truncated = len(want.QA) < full.TotalTurns
	// This private CLI field supports opt-in thinking reads; indexed pages do
	// not expose a source path or use it as a fallback.
	want.TranscriptPath = ""
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("page offset=%d limit=%d differs from complete Show:\ngot  %#v\nwant %#v", offset, limit, got, want)
	}
}

func TestShowPagePreservesVisibleTurnsAndRawNumbering(t *testing.T) {
	fixture := newServiceFixture(t)
	seedPageMessages(t, fixture, []pageMessage{
		{role: "system", content: "ignored before the first assistant"},
		{role: "user", content: "\t\n\u00a0\u3000"},
		{role: "assistant", kind: "progress", content: "leading progress"},
		{role: "assistant", content: "leading answer"},
		{role: "user", content: "# AGENTS.md instructions\nhidden question"},
		{role: "user", content: "plain question"},
		{role: "assistant", content: "ordinary answer replaced by final"},
		{role: "assistant", kind: "final", content: "first final"},
		{role: "assistant", kind: "progress", content: "progress for plain question"},
		{role: "assistant", kind: "final", content: "second final"},
		{role: "user", content: "<system-reminder>hidden question</system-reminder>"},
		{role: "assistant", content: "must not revive a hidden final"},
		{role: "assistant", kind: "final", content: "# AGENTS.md instructions\nhidden first final"},
		{role: "assistant", kind: "final", content: "visible alone, hidden when joined after the prefix"},
		{role: "user", content: "<environment_context>hidden question"},
		{role: "assistant", kind: "final", content: "<app-context>hidden first final"},
		{role: "assistant", kind: "final", content: "## My request for Codex:\nrestored final"},
		{role: "user", content: "<ide_selection>hidden question</ide_selection>"},
		{role: "assistant", kind: "progress", content: "仍在核对"},
		{role: "assistant", kind: "final", content: "<recommended_plugins>hidden final"},
		{role: "user", content: "## My request:\nolder request\n## My request for Codex:\nlatest request"},
		{role: "user", kind: "control", content: "must not create another turn"},
		{role: "user", kind: "progress", content: "non-conversation user is ignored"},
		{role: "assistant", scope: "inherited", kind: "final", content: "inherited final is ignored"},
		{role: "assistant", kind: "control", content: "control assistant is ignored"},
		{role: "assistant", content: "last answer"},
	})
	if err := os.Remove(fixture.transcriptPath); err != nil {
		t.Fatal(err)
	}
	full, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	wantQA := []QA{
		{Turn: 1, A: "leading answer", Progress: []string{"leading progress"}},
		{Turn: 3, Q: "plain question", A: "first final\n\nsecond final", Progress: []string{"progress for plain question"}},
		{Turn: 5, A: "restored final"},
		{Turn: 6, Progress: []string{"仍在核对"}},
		{Turn: 7, Q: "latest request", A: "last answer"},
	}
	if !reflect.DeepEqual(full.QA, wantQA) || full.TotalTurns != len(wantQA) {
		t.Fatalf("unexpected complete Show baseline: %#v", full)
	}
	for _, limit := range []int{1, 2, 3, 50} {
		for offset := 0; offset < len(full.QA); offset += limit {
			assertShowPageMatchesFull(t, fixture.service, full, offset, limit)
		}
		assertShowPageMatchesFull(t, fixture.service, full, len(full.QA), limit)
		assertShowPageMatchesFull(t, fixture.service, full, len(full.QA)+1, limit)
	}
}

func TestShowPageVisibilityMatchesRequestMarkersTagsAndUnicodeSpace(t *testing.T) {
	fixture := newServiceFixture(t)
	contents := []string{
		"<ide_selection>hidden</ide_selection>",
		"\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000",
		"\u3000# AGENTS.md instructions\ncontrol\u00a0",
		"<permissions instructions>control",
		"<app-context>control",
		"<environment_context>control",
		"<skills_instructions>control",
		"<image name=\"attachment\">control",
		"</image>control",
		"Message Type: NEW_TASK\ncontrol",
		"Message Type: MESSAGE\ncontrol",
		"Message Type: FINAL_ANSWER\ncontrol",
		"Within the root conversation\ncontrol",
		"You are an agent in a team of agents collaborating\ncontrol",
		"Some conversation entries were omitted.\ncontrol",
		"<recommended_plugins>control",
		"<recommended_plugins>wrapped\n## My request for Codex:\nactual request",
		"## My request:\nfirst\n# My request:\nsecond\n## My request:\nlast",
		"<ide_opened_file>file</ide_opened_file><ide_selection>selection</ide_selection>",
		"<system-reminder>first</system-reminder><system-reminder>second</system-reminder>",
		"before<system_context>hidden</system_context>after",
		"<system_context>an unmatched opening tag hides the remainder",
		"kept<ide_opened_file>an unmatched opening tag hides the remainder",
		"<ide_selection>outer<ide_selection>inner</ide_selection>suffix</ide_selection>",
		// Prefix removal happens before tag removal, and is not repeated.
		"<system_context>hidden</system_context># AGENTS.md instructions",
		// Request extraction happens before tag removal, including a marker
		// inside a wrapper whose opening tag extraction removes.
		"<system_context>## My request:\nvisible</system_context>",
		"\u3000 visible 中文🙂 \u00a0",
		"final ordinary question",
	}
	messages := make([]pageMessage, len(contents))
	for index, content := range contents {
		messages[index] = pageMessage{role: "user", content: content}
	}
	seedPageMessages(t, fixture, messages)
	full, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	if full.TotalTurns < 5 || full.TotalTurns >= len(contents) {
		t.Fatalf("fixture needs both visible turns and gaps: %#v", full)
	}
	for offset := 0; offset <= len(full.QA); offset++ {
		assertShowPageMatchesFull(t, fixture.service, full, offset, 1)
	}
}

func TestShowPageReturnsCompleteLongUnicodeSelectedTurn(t *testing.T) {
	fixture := newServiceFixture(t)
	question := strings.Repeat("问题甲乙🙂\n", 8000) + "问题末尾"
	progress := strings.Repeat("检查丙丁🙂\n", 5000) + "进度末尾"
	firstFinal := strings.Repeat("答案戊己🙂\n", 12000) + "第一段末尾"
	secondFinal := "第二段最终结果🙂"
	seedPageMessages(t, fixture, []pageMessage{
		{role: "user", content: "before the selected page"},
		{role: "assistant", content: "first answer"},
		{role: "user", content: question},
		{role: "assistant", kind: "progress", content: progress},
		{role: "assistant", content: "ordinary response replaced by the complete final"},
		{role: "assistant", kind: "final", content: firstFinal},
		{role: "assistant", kind: "final", content: secondFinal},
		{role: "user", content: "after the selected page"},
		{role: "assistant", content: "last answer"},
	})
	got, err := fixture.service.ShowPage(context.Background(), PageInput{SessionID: "recent", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	wantQA := []QA{{Turn: 2, Q: question, A: firstFinal + "\n\n" + secondFinal, Progress: []string{progress}}}
	if got.TotalTurns != 3 || got.ReturnedTurns != 1 || !got.Truncated || got.ContentTruncated || !reflect.DeepEqual(got.QA, wantQA) {
		t.Fatalf("selected Unicode turn is incomplete: total=%d returned=%d truncated=%v content_truncated=%v qa_count=%d",
			got.TotalTurns, got.ReturnedTurns, got.Truncated, got.ContentTruncated, len(got.QA))
	}
}

func TestShowPageEmptyAndEntirelyHiddenSessions(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		messages []pageMessage
	}{
		{name: "empty"},
		{name: "hidden", messages: []pageMessage{
			{role: "user", content: "# AGENTS.md instructions"},
			{role: "assistant", kind: "progress", content: "<system-reminder>hidden</system-reminder>"},
			{role: "assistant", kind: "final", content: "<recommended_plugins>hidden"},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newServiceFixture(t)
			seedPageMessages(t, fixture, testCase.messages)
			full, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "recent"})
			if err != nil {
				t.Fatal(err)
			}
			if full.TotalTurns != 0 {
				t.Fatalf("hidden baseline = %#v", full)
			}
			assertShowPageMatchesFull(t, fixture.service, full, 0, 1)
			assertShowPageMatchesFull(t, fixture.service, full, 1, 1)
		})
	}
}

func TestShowPageValidatesBoundsAndMissingSession(t *testing.T) {
	fixture := newServiceFixture(t)
	for _, input := range []PageInput{
		{Limit: 1},
		{SessionID: "recent", Offset: -1, Limit: 1},
		{SessionID: "recent", Offset: 100001, Limit: 1},
		{SessionID: "recent", Limit: -1},
		{SessionID: "recent", Limit: 0},
		{SessionID: "recent", Limit: 51},
	} {
		if _, err := fixture.service.ShowPage(context.Background(), input); !errors.Is(err, application.ErrInvalidArgument) {
			t.Errorf("input %#v: got %v, want invalid argument", input, err)
		}
	}
	if _, err := fixture.service.ShowPage(context.Background(), PageInput{SessionID: "missing", Limit: 1}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing session error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.service.ShowPage(ctx, PageInput{SessionID: "recent", Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled page error = %v", err)
	}
}
