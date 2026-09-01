package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := ""
	for _, line := range lines {
		data += line + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
}

func TestCodexSessionTitlesUseLatestThreadIndexName(t *testing.T) {
	index := filepath.Join(t.TempDir(), "session_index.jsonl")
	writeJSONL(t, index,
		`{"id":"thread-1","thread_name":"Initial title","updated_at":"2026-08-01T10:00:00Z"}`,
		`{"id":"thread-2","thread_name":"Another conversation","updated_at":"2026-08-01T10:01:00Z"}`,
		`{"id":"thread-1","thread_name":"优化 ATM Agents 标签","updated_at":"2026-08-02T07:08:58Z"}`,
	)

	titles := codexSessionTitlesFromFile(index)
	if titles["thread-1"] != "优化 ATM Agents 标签" {
		t.Fatalf("thread title = %q", titles["thread-1"])
	}
	if titles["thread-2"] != "Another conversation" {
		t.Fatalf("second thread title = %q", titles["thread-2"])
	}
}

// Codex writes no title into its rollout, so an indexed codex session used to
// reach every ATM surface nameless. Its own thread index carries the title.
func TestCodexParseFileNamesSessionFromThreadIndex(t *testing.T) {
	root := t.TempDir()
	writeJSONL(t, filepath.Join(root, "session_index.jsonl"),
		`{"id":"019fc6cb-c6ce-71e2-9b65-8f0af41c825b","thread_name":"优化 UI 设计","updated_at":"2026-08-03T08:44:41Z"}`,
	)
	useCodexSessions(t, filepath.Join(root, "sessions"))

	fp := filepath.Join(config.CodexSessions, "2026", "08", "03",
		"rollout-2026-08-03T16-44-31-019fc6cb-c6ce-71e2-9b65-8f0af41c825b.jsonl")
	writeJSONL(t, fp,
		`{"timestamp":"2026-08-03T16:44:31Z","type":"session_meta","payload":{"id":"019fc6cb-c6ce-71e2-9b65-8f0af41c825b","cwd":"/tmp/my-project"}}`,
		`{"timestamp":"2026-08-03T16:45:00Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"把绑定会话行重排一下"}]}}`,
	)

	got := CodexParseFile(fp)
	if got == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if got.Summary != "优化 UI 设计" {
		t.Fatalf("summary = %q", got.Summary)
	}
}

// Threads the index has not covered still have to be nameable, and the first
// stored prompt is usually a harness preamble rather than anything the human
// typed.
func TestCodexParseFileFallsBackToFirstHumanPrompt(t *testing.T) {
	root := t.TempDir()
	useCodexSessions(t, filepath.Join(root, "sessions"))

	fp := filepath.Join(config.CodexSessions, "2026", "08", "03", "rollout-2026-08-03T16-44-31-untitled.jsonl")
	writeJSONL(t, fp,
		`{"timestamp":"2026-08-03T16:44:31Z","type":"session_meta","payload":{"id":"missing-from-index","cwd":"/tmp/my-project"}}`,
		`{"timestamp":"2026-08-03T16:44:40Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"<recommended_plugins>\nAtlassian Rovo"}]}}`,
		`{"timestamp":"2026-08-03T16:44:45Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\n<INSTRUCTIONS>bind first</INSTRUCTIONS>"}]}}`,
		`{"timestamp":"2026-08-03T16:45:00Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"任务详情看不到 session 信息\n第二行不该出现在标题里"}]}}`,
	)

	got := CodexParseFile(fp)
	if got == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if got.Summary != "任务详情看不到 session 信息" {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func useCodexSessions(t *testing.T, dir string) {
	t.Helper()
	original := config.CodexSessions
	config.CodexSessions = dir
	t.Cleanup(func() { config.CodexSessions = original })
}

func TestCodexLastUserEventSurvivesLargeTurnPayloads(t *testing.T) {
	rollout := filepath.Join(t.TempDir(), "rollout-thread.jsonl")
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"Older input"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"# Files mentioned by the user:\n\n## My request for Codex:\n展示最近一次用户输入"}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_image","image_url":"` + strings.Repeat("A", reverseSearchChunkSize*3) + `"}]}}`,
	}
	for index := 0; index < 80; index++ {
		lines = append(lines, `{"type":"response_item","payload":{"type":"function_call","name":"tool"}}`)
	}
	writeJSONL(t, rollout, lines...)

	message := codexLastEventMessage(rollout, "user_message", codexLiveSessionMetadata{})
	if visible := codexVisibleUserMessage(message); visible != "展示最近一次用户输入" {
		t.Fatalf("visible user input = %q", visible)
	}
}

func TestCodexRecentAgentEventsKeepChronologicalPhases(t *testing.T) {
	rollout := filepath.Join(t.TempDir(), "rollout-thread.jsonl")
	writeJSONL(t, rollout,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"First update","phase":"commentary"}}`,
		`{"type":"response_item","payload":{"content":"`+strings.Repeat("A", reverseSearchChunkSize*2)+`"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"Completed result","phase":"final_answer"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"New turn update","phase":"commentary"}}`,
	)

	events := codexRecentAgentEvents(rollout, 3, codexLiveSessionMetadata{})
	if len(events) != 3 {
		t.Fatalf("agent events = %#v", events)
	}
	if events[0].Message != "First update" || events[1].Phase != "final_answer" || events[2].Message != "New turn update" {
		t.Fatalf("agent events order = %#v", events)
	}
}

func TestCodexRecentAgentEventsSupportCurrentResponseItems(t *testing.T) {
	rollout := filepath.Join(t.TempDir(), "rollout-thread.jsonl")
	writeJSONL(t, rollout,
		`{"ordinal":1,"type":"response_item","payload":{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Current update"}]}}`,
		`{"ordinal":2,"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"commentary","content":[{"type":"Text","text":"Current update"}]}}}`,
		`{"ordinal":3,"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Current result"}]}}`,
	)

	events := codexRecentAgentEvents(rollout, 3, codexLiveSessionMetadata{})
	if len(events) != 2 {
		t.Fatalf("agent events = %#v", events)
	}
	if events[0] != (codexAgentEvent{Message: "Current update", Phase: "commentary"}) ||
		events[1] != (codexAgentEvent{Message: "Current result", Phase: "final_answer"}) {
		t.Fatalf("current agent events = %#v", events)
	}
}

func TestClaudeParseFileFiltersNoiseAndCollectsUsage(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "sample-project", "0123456789abcdef.jsonl")
	writeJSONL(t, fp,
		`{"type":"ai-title","timestamp":"2026-06-27T10:00:00Z","aiTitle":"Build parser tests"}`,
		`{"type":"user","timestamp":"2026-06-27T10:00:01Z","message":{"content":[{"type":"text","text":"<system-reminder>ignore this</system-reminder>"}]}}`,
		`{"type":"user","timestamp":"2026-06-27T10:00:02Z","message":{"content":[{"type":"text","text":"Please build this feature"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:03Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":2,"cache_read_input_tokens":3},"content":[{"type":"tool_use","name":"Edit"},{"type":"tool_use","name":"Skill","input":{"skill":"atm"}},{"type":"text","text":"Done."}]}}`,
	)

	got := ClaudeParseFile(fp)
	if got == nil {
		t.Fatal("ClaudeParseFile returned nil")
	}
	if got.SessionID != "0123456789abcdef" || got.ShortID != "01234567" {
		t.Fatalf("unexpected ids: %#v", got)
	}
	if got.Project != "sample-project" {
		t.Fatalf("project = %q", got.Project)
	}
	if got.Summary != "Build parser tests" {
		t.Fatalf("summary = %q", got.Summary)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Content != "Please build this feature" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Content != "Done." {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	if got.Tools["Edit"] != 1 {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "atm" {
		t.Fatalf("skills = %#v", got.Skills)
	}
	if got.Usage.Model != "claude-sonnet-4-6" ||
		got.Usage.InputTokens != 10 ||
		got.Usage.OutputTokens != 5 ||
		got.Usage.CacheCreateTokens != 2 ||
		got.Usage.CacheReadTokens != 3 {
		t.Fatalf("usage = %#v", got.Usage)
	}
	if len(got.UsageEvents) != 1 || got.UsageEvents[0].Model != "claude-sonnet-4-6" || got.UsageEvents[0].InputTokens != 10 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	wantTS := time.Date(2026, 6, 27, 10, 0, 3, 0, time.UTC).Unix()
	if got.LastTS != wantTS {
		t.Fatalf("last ts = %d, want %d", got.LastTS, wantTS)
	}
}

func TestClaudeParseFileKeepsPromptAfterNoiseBlock(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "sample-project", "0123456789abcdef.jsonl")
	writeJSONL(t, fp,
		`{"type":"user","timestamp":"2026-07-29T06:00:00Z","message":{"content":[{"type":"text","text":"<ide_opened_file>The user opened config.go</ide_opened_file>"},{"type":"text","text":"能增加一个模型执行速度的监测吗？"}]}}`,
		`{"type":"assistant","timestamp":"2026-07-29T06:00:01Z","message":{"id":"msg_opus","model":"claude-opus-5","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":80},"content":[{"type":"text","text":"可以。"}]}}`,
	)

	got := ClaudeParseFile(fp)
	if got == nil {
		t.Fatal("ClaudeParseFile returned nil for a prompt after an IDE noise block")
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Content != "能增加一个模型执行速度的监测吗？" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
	if got.Usage.Model != "claude-opus-5" || got.Usage.InputTokens != 100 ||
		got.Usage.OutputTokens != 20 || got.Usage.CacheReadTokens != 80 {
		t.Fatalf("usage = %#v", got.Usage)
	}
}

// No agent logs how long a request took, so each parser derives it from the
// records around the response. The window has to start at the last thing the
// model read and end at the last thing it wrote, or it measures the tools instead
// of the model.
func TestClaudeTimesARequestAcrossItsSplitRecords(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "sample-project", "0123456789abcdef.jsonl")
	writeJSONL(t, fp,
		`{"type":"user","timestamp":"2026-06-27T10:00:02.000Z","message":{"content":[{"type":"text","text":"Please build this feature"}]}}`,
		// One response, three records: the window runs to the last of them.
		`{"type":"assistant","timestamp":"2026-06-27T10:00:07.500Z","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":300},"content":[{"type":"text","text":"Working on it."}]}}`,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:08.000Z","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":300},"content":[{"type":"tool_use","name":"Edit"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:09.250Z","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":300},"content":[{"type":"tool_use","name":"Write"}]}}`,
		// The tool ran for 30s. The next request is timed from its result, so that
		// time belongs to neither request.
		`{"type":"user","timestamp":"2026-06-27T10:00:39.000Z","message":{"content":[{"type":"tool_result","text":"ok"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:41.000Z","message":{"id":"msg_2","model":"claude-opus-5","usage":{"input_tokens":12,"output_tokens":90},"content":[{"type":"text","text":"Done."}]}}`,
	)

	got := ClaudeParseFile(fp)
	if got == nil {
		t.Fatal("ClaudeParseFile returned nil")
	}
	if len(got.UsageEvents) != 2 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	if got.UsageEvents[0].DurationMS != 7250 {
		t.Fatalf("first duration = %d ms, want 7250 (10:00:02.000 → 10:00:09.250)", got.UsageEvents[0].DurationMS)
	}
	if got.UsageEvents[1].DurationMS != 2000 {
		t.Fatalf("second duration = %d ms, want 2000 (tool time excluded)", got.UsageEvents[1].DurationMS)
	}
}

// Codex writes token_count at the same instant as the tool output that the next
// request starts from, so the response has to be timed by its own output records.
// Requests whose responses were written to another file — sub-agent rollups —
// have no window and must report 0 rather than inheriting the last one.
func TestCodexTimesRequestsByModelOutputNotByTheUsageRecord(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "2026", "06", "27", "rollout-abcdef123456.jsonl")
	writeJSONL(t, fp,
		`{"timestamp":"2026-06-27T10:00:00.000Z","type":"session_meta","payload":{"cwd":"/tmp/my-project"}}`,
		`{"timestamp":"2026-06-27T10:00:01.000Z","type":"turn_context","payload":{"model":"gpt-5.1-codex"}}`,
		`{"timestamp":"2026-06-27T10:00:10.000Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Implement stats command"}]}}`,
		`{"timestamp":"2026-06-27T10:00:14.000Z","type":"response_item","payload":{"type":"reasoning"}}`,
		`{"timestamp":"2026-06-27T10:00:16.500Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{}"}}`,
		// Tool ran 8s; its output opens the next request and carries the usage of
		// the one that just finished.
		`{"timestamp":"2026-06-27T10:00:24.500Z","type":"response_item","payload":{"type":"function_call_output"}}`,
		`{"timestamp":"2026-06-27T10:00:24.500Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":200,"cached_input_tokens":0}}}}`,
		// A second usage record with no output of its own in between: unmeasurable.
		`{"timestamp":"2026-06-27T10:00:26.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":180,"output_tokens":260,"cached_input_tokens":0}}}}`,
	)

	got := CodexParseFile(fp)
	if got == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if len(got.UsageEvents) != 2 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	if got.UsageEvents[0].DurationMS != 6500 {
		t.Fatalf("first duration = %d ms, want 6500 (10:00:10.000 → 10:00:16.500)", got.UsageEvents[0].DurationMS)
	}
	if got.UsageEvents[1].DurationMS != 0 {
		t.Fatalf("second duration = %d ms, want 0: no output record of its own", got.UsageEvents[1].DurationMS)
	}
}

func TestPiTimesRequestsFromThePrecedingRecord(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "--tmp-pi-project--", "2026-07-10_uuid123456.jsonl")
	writeJSONL(t, fp,
		`{"type":"session","timestamp":"2026-07-10T10:00:00.000Z","cwd":"/tmp/pi-project"}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:05.000Z","message":{"role":"user","content":[{"type":"text","text":"Explain the parser"}]}}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:11.400Z","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"Here goes."}],"usage":{"input":50,"output":320}}}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:40.000Z","message":{"role":"toolResult","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:43.000Z","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"And done."}],"usage":{"input":60,"output":100}}}`,
	)

	got := PiParseFile(fp)
	if got == nil {
		t.Fatal("PiParseFile returned nil")
	}
	if len(got.UsageEvents) != 2 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	if got.UsageEvents[0].DurationMS != 6400 || got.UsageEvents[1].DurationMS != 3000 {
		t.Fatalf("durations = %d, %d ms; want 6400, 3000",
			got.UsageEvents[0].DurationMS, got.UsageEvents[1].DurationMS)
	}
}

// An append parse starts mid-file, so the first response in the appended region
// has nothing to be timed from. Reporting 0 keeps it out of the speed stats
// instead of inventing a window from the region's own first record.
func TestClaudeAppendLeavesTheFirstResponseUntimed(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "sample-project", "0123456789abcdef.jsonl")
	head := `{"type":"user","timestamp":"2026-06-27T10:00:02.000Z","message":{"content":[{"type":"text","text":"Please build this feature"}]}}`
	writeJSONL(t, fp, head)
	offset := fileSize(fp)
	writeJSONL(t, fp, head,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:07.500Z","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10,"output_tokens":300},"content":[{"type":"text","text":"Working on it."}]}}`,
	)

	got := ClaudeParseAppend(fp, offset)
	if got == nil {
		t.Fatal("ClaudeParseAppend returned nil")
	}
	if len(got.UsageEvents) != 1 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	if got.UsageEvents[0].DurationMS != 0 {
		t.Fatalf("duration = %d ms, want 0: the request's start is before the offset", got.UsageEvents[0].DurationMS)
	}
}

func TestPiParseFilePreservesOrderAndUsageByModel(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "--tmp-pi-project--", "2026-07-10_uuid123456.jsonl")
	writeJSONL(t, fp,
		`{"type":"session","timestamp":"2026-07-10T10:00:00Z","cwd":"/tmp/pi-project"}`,
		`{"type":"session_info","timestamp":"2026-07-10T10:00:01Z","name":"Pi parser"}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:02Z","message":{"role":"user","content":"first question"}}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:03Z","message":{"role":"user","content":"follow up"}}`,
		`{"type":"model_change","timestamp":"2026-07-10T10:00:04Z","modelId":"model-a"}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:05Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"secret"},{"type":"toolCall","name":"read","arguments":{"path":"/Users/test/.agents/skills/example-chat/SKILL.md"}},{"type":"text","text":"answer a"}],"usage":{"input":10,"output":4,"cacheRead":3,"cacheWrite":2}}}`,
		`{"type":"model_change","timestamp":"2026-07-10T10:00:06Z","modelId":"model-b"}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:07Z","message":{"role":"assistant","model":"model-b","content":[{"type":"text","text":"answer b"}],"usage":{"input":20,"output":8,"cacheRead":6,"cacheWrite":1}}}`,
	)

	got := PiParseFile(fp)
	if got == nil {
		t.Fatal("PiParseFile returned nil")
	}
	if got.Project != "pi-project" || got.Summary != "Pi parser" {
		t.Fatalf("metadata = %#v", got)
	}
	if len(got.Messages) != 4 || got.Messages[0].Role != "user" || got.Messages[1].Content != "follow up" || got.Messages[2].Content != "answer a" {
		t.Fatalf("messages = %#v", got.Messages)
	}
	if got.Tools["read"] != 1 {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "example-chat" {
		t.Fatalf("skills = %#v", got.Skills)
	}
	if got.Usage.Model != "model-b" || got.Usage.InputTokens != 30 || got.Usage.OutputTokens != 12 || got.Usage.RequestCount != 2 {
		t.Fatalf("usage = %#v", got.Usage)
	}
	if len(got.UsageEvents) != 2 || got.UsageEvents[0].Model != "model-a" || got.UsageEvents[1].Model != "model-b" {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	blocks := PiExtractThinking(fp)
	if len(blocks) != 1 || blocks[0].Thinking != "secret" || blocks[0].Response != "answer a" {
		t.Fatalf("thinking = %#v", blocks)
	}
}

// Pi's live rows name the session by an eight-character prefix for display,
// while the notch extension reports the whole id. ResumeID is what carries the
// untruncated one, and without it a Pi hook event can never be joined to the row
// it describes — leaving the snapshot heuristic to prompt on its own.
func TestPiLiveSessionsExposeTheFullSessionID(t *testing.T) {
	const sessionID = "019fd0f0-e373-7f32-9c1a-656466790cbc"
	root := t.TempDir()
	oldSessions := config.PiSessions
	config.PiSessions = root
	t.Cleanup(func() { config.PiSessions = oldSessions })

	fp := filepath.Join(root, "--tmp-pi-project--", "2026-08-05T08-01-15-635Z_"+sessionID+".jsonl")
	writeJSONL(t, fp,
		`{"type":"session","timestamp":"2026-08-05T08:01:15Z","cwd":"/tmp/pi-project"}`,
		`{"type":"message","timestamp":"2026-08-05T08:01:16Z","message":{"role":"user","content":"实现 t202"}}`,
	)

	sessions := PiLiveSessions(24 * time.Hour)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].SessionID != sessionID[:8] {
		t.Errorf("session id = %q, want the truncated one for display", sessions[0].SessionID)
	}
	if sessions[0].ResumeID != sessionID {
		t.Errorf("resume id = %q, want %q", sessions[0].ResumeID, sessionID)
	}
}

func TestCodexLiveSessionsKeepSubagentIdentityAndOwnActivity(t *testing.T) {
	const (
		childID  = "019fd0f0-e373-7f32-9c1a-656466790cbc"
		parentID = "019fd0f0-e373-7f32-9c1a-656466790cba"
	)
	root := t.TempDir()
	useCodexSessions(t, root)
	now := time.Now().In(config.Loc)
	fp := filepath.Join(root, now.Format("2006"), now.Format("01"), now.Format("02"),
		"rollout-"+now.Format("2006-01-02T15-04-05")+"-"+childID+".jsonl")
	writeJSONL(t, fp,
		`{"ordinal":0,"timestamp":"2026-08-30T01:00:00Z","type":"session_meta","payload":{"session_id":"root-thread","id":"`+childID+`","parent_thread_id":"`+parentID+`","agent_path":"/root/recent_todos/status_model","agent_nickname":"Goodall","subagent_history_start_ordinal":6,"cwd":"/tmp/child-project","originator":"Codex Desktop","source":{"subagent":{"thread_spawn":{"parent_thread_id":"`+parentID+`","depth":2,"agent_path":"/root/recent_todos/status_model","agent_nickname":"Goodall"}}}}}`,
		`{"ordinal":1,"timestamp":"2026-08-30T01:00:01Z","type":"session_meta","payload":{"id":"`+parentID+`","cwd":"/tmp/parent-project","originator":"Parent Client"}}`,
		`{"ordinal":2,"timestamp":"2026-08-30T01:00:02Z","type":"turn_context","payload":{"model":"parent-model"}}`,
		`{"ordinal":3,"timestamp":"2026-08-30T01:00:03Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"copied parent prompt"}]}}`,
		`{"ordinal":4,"timestamp":"2026-08-30T01:00:04Z","type":"event_msg","payload":{"type":"user_message","message":"copied parent event"}}`,
		`{"ordinal":5,"timestamp":"2026-08-30T01:00:05Z","type":"event_msg","payload":{"type":"agent_message","message":"copied parent update","phase":"commentary"}}`,
		`{"ordinal":6,"timestamp":"2026-08-30T01:00:06Z","type":"turn_context","payload":{"model":"child-model"}}`,
		`{"ordinal":7,"timestamp":"2026-08-30T01:00:07Z","type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"child answer"}]}}`,
		`{"ordinal":8,"timestamp":"2026-08-30T01:00:08Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec"}}`,
		`{"ordinal":9,"timestamp":"2026-08-30T01:00:09Z","type":"event_msg","payload":{"type":"agent_message","message":"child update","phase":"commentary"}}`,
		`{"ordinal":10,"timestamp":"2026-08-30T01:00:10Z","type":"event_msg","payload":{"type":"agent_message","message":"child result","phase":"final_answer"}}`,
	)

	sessions := CodexLiveSessions(24 * time.Hour)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}
	got := sessions[0]
	if got.ResumeID != childID || got.RootSessionID != "root-thread" || got.ParentSessionID != parentID {
		t.Errorf("session identity = resume %q root %q parent %q", got.ResumeID, got.RootSessionID, got.ParentSessionID)
	}
	if got.AgentPath != "/root/recent_todos/status_model" || got.AgentNickname != "Goodall" || got.SubagentDepth != 2 {
		t.Errorf("subagent metadata = path %q nickname %q depth %d", got.AgentPath, got.AgentNickname, got.SubagentDepth)
	}
	if got.Client != "Codex Desktop" || got.CWD != "/tmp/child-project" || got.Model != "child-model" {
		t.Errorf("child metadata overwritten by parent = client %q cwd %q model %q", got.Client, got.CWD, got.Model)
	}
	if got.FirstQ != "" || got.LastUserMsg != "" {
		t.Errorf("copied parent prompt leaked into child row: first=%q last=%q", got.FirstQ, got.LastUserMsg)
	}
	if got.LatestResult != "child result" || len(got.RecentUpdates) != 1 || got.RecentUpdates[0] != "child update" {
		t.Errorf("child activity = result %q updates %#v", got.LatestResult, got.RecentUpdates)
	}
	if len(got.RecentTools) != 1 || got.RecentTools[0] != "exec" {
		t.Errorf("child tools = %#v", got.RecentTools)
	}
}

func TestCodexLiveMetadataFindsOwnModelPastHeadWindow(t *testing.T) {
	const boundary = 45
	fp := filepath.Join(t.TempDir(), "rollout-deep-child.jsonl")
	lines := []string{
		`{"ordinal":0,"timestamp":"2026-08-30T01:00:00Z","type":"session_meta","payload":{"session_id":"root-thread","id":"deep-child","parent_thread_id":"direct-parent","thread_source":"subagent","subagent_history_start_ordinal":45,"agent_path":"/root/one/two/three","source":{"subagent":{"thread_spawn":{"depth":3,"parent_thread_id":"direct-parent"}}}}}`,
	}
	for ordinal := 1; ordinal < boundary; ordinal++ {
		lines = append(lines, fmt.Sprintf(`{"ordinal":%d,"timestamp":"2026-08-30T01:00:01Z","type":"event_msg","payload":{"type":"copied_parent_record"}}`, ordinal))
	}
	lines = append(lines,
		`{"ordinal":45,"timestamp":"2026-08-30T01:00:45Z","type":"turn_context","payload":{"model":"deep-child-model"}}`,
		`{"ordinal":46,"timestamp":"2026-08-30T01:00:46Z","type":"turn_context","payload":{"model":"later-model"}}`,
	)
	writeJSONL(t, fp, lines...)

	metadata := codexLiveMetadata(fp)
	if metadata.Model != "deep-child-model" {
		t.Fatalf("model = %q, want first own turn_context past HeadJSONL window", metadata.Model)
	}
	if metadata.RootSessionID != "root-thread" || metadata.ParentSessionID != "direct-parent" {
		t.Errorf("session tree = root %q parent %q", metadata.RootSessionID, metadata.ParentSessionID)
	}
}

func TestCodexLiveMetadataKeepsRegularForkOutOfSubagentTree(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "rollout-fork.jsonl")
	writeJSONL(t, fp,
		`{"ordinal":0,"timestamp":"2026-08-30T01:00:00Z","type":"session_meta","payload":{"id":"fork-thread","session_id":"original-root","forked_from_id":"parent-thread","parent_thread_id":"parent-thread","cwd":"/tmp/project","originator":"Codex Desktop"}}`,
		`{"ordinal":1,"timestamp":"2026-08-30T01:00:01Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"continue this user fork"}]}}`,
	)

	metadata := codexLiveMetadata(fp)
	if metadata.IsSubagent || metadata.ParentSessionID != "" || metadata.RootSessionID != "" {
		t.Fatalf("regular fork classified as subagent: %#v", metadata)
	}
	if metadata.ResumeID != "fork-thread" || metadata.FirstQ != "continue this user fork" {
		t.Errorf("regular fork identity = resume %q first %q", metadata.ResumeID, metadata.FirstQ)
	}
}

func TestCodexLiveMetadataRecognizesLegacySubagentSourceShapes(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "string", source: `{"subagent":"review"}`},
		{name: "other", source: `{"subagent":{"other":"guardian"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fp := filepath.Join(t.TempDir(), "rollout-legacy-child.jsonl")
			writeJSONL(t, fp,
				`{"timestamp":"2026-08-30T01:00:00Z","type":"session_meta","payload":{"id":"child-thread","session_id":"root-thread","forked_from_id":"parent-thread","source":`+test.source+`}}`,
				`{"timestamp":"2026-08-30T01:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"copied parent prompt"}]}}`,
			)
			metadata := codexLiveMetadata(fp)
			if !metadata.IsSubagent || metadata.ParentSessionID != "parent-thread" {
				t.Fatalf("legacy metadata = %#v", metadata)
			}
			if metadata.FirstQ != "" {
				t.Fatalf("copied parent prompt leaked: %q", metadata.FirstQ)
			}
		})
	}
}

func TestCodexLiveMetadataRecoversSubagentIDFromRolloutFilename(t *testing.T) {
	const (
		childID = "019fd0f0-e373-7f32-9c1a-656466790cbc"
		rootID  = "019fd0f0-e373-7f32-9c1a-656466790cba"
	)
	fp := filepath.Join(t.TempDir(), "rollout-2026-08-30T01-00-00-"+childID+".jsonl")
	writeJSONL(t, fp,
		`{"ordinal":0,"timestamp":"2026-08-30T01:00:00Z","type":"session_meta","payload":{"session_id":"`+rootID+`","parent_thread_id":"parent-thread","agent_path":"/root/child","subagent_history_start_ordinal":1,"source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent-thread","depth":1,"agent_path":"/root/child"}}}}}`,
		`{"ordinal":1,"timestamp":"2026-08-30T01:00:01Z","type":"turn_context","payload":{"model":"child-model"}}`,
	)

	metadata := codexLiveMetadata(fp)
	if metadata.ResumeID != childID || metadata.RootSessionID != rootID {
		t.Fatalf("identity = resume %q root %q", metadata.ResumeID, metadata.RootSessionID)
	}
}

func TestCodexLiveSessionsHideGuardianBeforeModelAppears(t *testing.T) {
	root := t.TempDir()
	useCodexSessions(t, root)
	now := time.Now().In(config.Loc)
	fp := filepath.Join(root, now.Format("2006"), now.Format("01"), now.Format("02"),
		"rollout-"+now.Format("2006-01-02T15-04-05")+"-01a050a6-676b-78c3-8bb2-401cec33452b.jsonl")
	writeJSONL(t, fp,
		`{"timestamp":"2026-08-30T03:11:18.124Z","type":"session_meta","payload":{"id":"01a050a6-676b-78c3-8bb2-401cec33452b","session_id":"root-thread","parent_thread_id":"parent-thread","thread_source":"guardian_review","source":{"subagent":{"other":"guardian"}},"originator":"Codex Desktop","cwd":"/tmp/project"}}`,
		`{"timestamp":"2026-08-30T03:11:18.124Z","type":"response_item","payload":{"type":"compaction"}}`,
		`{"timestamp":"2026-08-30T03:11:18.124Z","type":"event_msg","payload":{"type":"thread_settings_applied"}}`,
	)

	metadata := codexLiveMetadata(fp)
	if !metadata.IsSubagent || !metadata.IsInternalSubagent || metadata.Model != "" {
		t.Fatalf("guardian metadata = %#v", metadata)
	}
	if sessions := CodexLiveSessions(24 * time.Hour); len(sessions) != 0 {
		t.Fatalf("guardian leaked into live sessions: %#v", sessions)
	}
}

func TestPiParseAppendReturnsOnlyNewRecords(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "--tmp-p--", "date_abcdefgh.jsonl")
	writeJSONL(t, fp,
		`{"type":"session","timestamp":"2026-07-10T10:00:00Z","cwd":"/tmp/p"}`,
		`{"type":"message","timestamp":"2026-07-10T10:00:01Z","message":{"role":"user","content":"initial question"}}`,
	)
	before, err := os.Stat(fp)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(fp, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(`{"type":"message","timestamp":"2026-07-10T10:00:02Z","message":{"role":"assistant","model":"model-a","content":[{"type":"text","text":"new answer"}],"usage":{"input":5,"output":2}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got := PiParseAppend(fp, before.Size())
	if got == nil || !got.Append || len(got.Messages) != 1 || got.Messages[0].Content != "new answer" || got.Usage.InputTokens != 5 {
		t.Fatalf("append = %#v", got)
	}
}

func TestCodexParseFileExtractsProjectMessagesAndTools(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "2026", "06", "27", "rollout-abcdef123456.jsonl")
	writeJSONL(t, fp,
		`{"timestamp":"2026-06-27T10:00:00Z","type":"session_meta","payload":{"cwd":"/tmp/my-project"}}`,
		`{"timestamp":"2026-06-27T10:00:30Z","type":"turn_context","payload":{"model":"gpt-5.1-codex"}}`,
		`{"timestamp":"2026-06-27T10:01:00Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Implement stats command"}]}}`,
		`{"timestamp":"2026-06-27T10:01:30Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":40}}}}`,
		`{"timestamp":"2026-06-27T10:02:00Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"sed -n '1,200p' /Users/test/.codex/skills/atm/SKILL.md\"}"}}`,
		`{"timestamp":"2026-06-27T10:03:00Z","type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"Implemented."},{"type":"output_text","text":"Added tests."}]}}`,
		`{"timestamp":"2026-06-27T10:04:00Z","type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"Ready to ship."}]}}`,
		`{"timestamp":"2026-06-27T10:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":35,"cached_input_tokens":70}}}}`,
	)

	got := CodexParseFile(fp)
	if got == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if got.ShortID != "abcdef12" {
		t.Fatalf("short id = %q", got.ShortID)
	}
	if got.Project != "my-project" {
		t.Fatalf("project = %q", got.Project)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Content != "Implement stats command" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
	wantInputTS := time.Date(2026, 6, 27, 10, 1, 0, 0, time.UTC).Unix()
	if got.Inputs[0].TS != wantInputTS {
		t.Fatalf("input ts = %d, want %d", got.Inputs[0].TS, wantInputTS)
	}
	wantOutput := "Implemented.\n\nAdded tests.\n\nReady to ship."
	if len(got.Outputs) != 1 || got.Outputs[0].Content != wantOutput {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	wantOutputTS := time.Date(2026, 6, 27, 10, 4, 0, 0, time.UTC).Unix()
	if got.Outputs[0].TS != wantOutputTS {
		t.Fatalf("output ts = %d, want %d", got.Outputs[0].TS, wantOutputTS)
	}
	if got.Tools["exec_command"] != 1 {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "atm" {
		t.Fatalf("skills = %#v", got.Skills)
	}
	wantCreated := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC).Unix()
	if got.CreatedTS != wantCreated {
		t.Fatalf("created ts = %d, want %d", got.CreatedTS, wantCreated)
	}
	wantLastTS := time.Date(2026, 6, 27, 10, 5, 0, 0, time.UTC).Unix()
	if got.LastTS != wantLastTS {
		t.Fatalf("last ts = %d, want %d", got.LastTS, wantLastTS)
	}
	if got.Usage.Model != "gpt-5.1-codex" || got.Usage.InputTokens != 90 || got.Usage.OutputTokens != 35 || got.Usage.CacheReadTokens != 70 {
		t.Fatalf("usage = %#v", got.Usage)
	}
	if len(got.UsageEvents) != 2 || got.UsageEvents[0].InputTokens != 60 || got.UsageEvents[1].InputTokens != 30 || got.UsageEvents[1].CacheReadTokens != 30 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
}

func TestCodexParseFileKeepsOnlySubagentOwnRecords(t *testing.T) {
	root := t.TempDir()
	useCodexSessions(t, filepath.Join(root, "sessions"))
	fp := filepath.Join(config.CodexSessions, "2026", "08", "30",
		"rollout-2026-08-30T01-00-00-019fd0f0-e373-7f32-9c1a-656466790cbc.jsonl")
	writeJSONL(t, fp,
		`{"ordinal":0,"timestamp":"2026-08-30T01:00:00Z","type":"session_meta","payload":{"id":"child-thread","session_id":"root-thread","parent_thread_id":"parent-thread","cwd":"/tmp/child-project","agent_path":"/root/child","subagent_history_start_ordinal":7,"source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent-thread","depth":1,"agent_path":"/root/child"}}}}}`,
		`{"ordinal":1,"timestamp":"2026-08-30T01:00:01Z","type":"session_meta","payload":{"id":"parent-thread","cwd":"/tmp/parent-project"}}`,
		`{"ordinal":2,"timestamp":"2026-08-30T01:00:02Z","type":"turn_context","payload":{"model":"parent-model"}}`,
		`{"ordinal":3,"timestamp":"2026-08-30T01:00:03Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"copied parent prompt"}]}}`,
		`{"ordinal":4,"timestamp":"2026-08-30T01:00:04Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"copied parent answer"}]}}`,
		`{"ordinal":5,"timestamp":"2026-08-30T01:00:05Z","type":"response_item","payload":{"type":"function_call","name":"Skill","input":{"skill":"parent-skill"}}}`,
		`{"ordinal":6,"timestamp":"2026-08-30T01:00:06Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":40},"total_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":40}}}}`,
		`{"ordinal":7,"timestamp":"2026-08-30T01:00:07Z","type":"turn_context","payload":{"model":"child-model"}}`,
		`{"ordinal":8,"timestamp":"2026-08-30T01:00:08Z","type":"response_item","payload":{"type":"agent_message","author":"/root","recipient":"/root/child","content":[{"type":"input_text","text":"delegated child task"}]}}`,
		`{"ordinal":9,"timestamp":"2026-08-30T01:00:09Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"child answer"}]}}`,
		`{"ordinal":10,"timestamp":"2026-08-30T01:00:10Z","type":"response_item","payload":{"type":"function_call","name":"Skill","input":{"skill":"atm"}}}`,
		`{"ordinal":11,"timestamp":"2026-08-30T01:00:11Z","type":"response_item","payload":{"type":"custom_tool_call","name":"child_tool"}}`,
		`{"ordinal":12,"timestamp":"2026-08-30T01:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":20,"output_tokens":5,"cached_input_tokens":10},"total_token_usage":{"input_tokens":120,"output_tokens":25,"cached_input_tokens":50}}}}`,
	)

	got := CodexParseFile(fp)
	if got == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if got.Project != "child-project" || got.Summary != "delegated child task" {
		t.Errorf("identity = project %q summary %q", got.Project, got.Summary)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Content != "delegated child task" {
		t.Errorf("inputs = %#v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Content != "child answer" {
		t.Errorf("outputs = %#v", got.Outputs)
	}
	if got.Tools["Skill"] != 1 || got.Tools["child_tool"] != 1 || got.Tools["parent_tool"] != 0 {
		t.Errorf("tools = %#v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "atm" {
		t.Errorf("skills = %#v", got.Skills)
	}
	if got.Usage.Model != "child-model" || got.Usage.RequestCount != 1 || got.Usage.InputTokens != 10 ||
		got.Usage.OutputTokens != 5 || got.Usage.CacheReadTokens != 10 || len(got.UsageEvents) != 1 {
		t.Errorf("usage = %#v events %#v", got.Usage, got.UsageEvents)
	}
	if got.UsageEvents[0].DurationMS != 3_000 {
		t.Errorf("child duration = %d ms, want 3000 from incoming agent task to last model output", got.UsageEvents[0].DurationMS)
	}
}

// A forked Codex thread opens with a restamped copy of its parent's history, so
// the two files report the same requests. The fingerprints have to match across
// the pair for the store to recognise the copies, and stay distinct between the
// fork's own requests and everything before them.
func TestCodexUsageFingerprintsMatchAcrossAForkedThread(t *testing.T) {
	dir := t.TempDir()
	parentRecords := []string{
		`{"timestamp":"2026-06-27T10:00:00Z","type":"session_meta","payload":{"id":"parent-id","cwd":"/tmp/my-project"}}`,
		`{"timestamp":"2026-06-27T10:00:30Z","type":"turn_context","payload":{"model":"gpt-5.1-codex"}}`,
		`{"timestamp":"2026-06-27T10:00:45Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Implement stats command"}]}}`,
		`{"timestamp":"2026-06-27T10:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":40},"total_token_usage":{"input_tokens":100,"output_tokens":20,"cached_input_tokens":40}}}}`,
		`{"timestamp":"2026-06-27T10:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":150,"output_tokens":30,"cached_input_tokens":90},"total_token_usage":{"input_tokens":250,"output_tokens":50,"cached_input_tokens":130}}}}`,
	}
	parentPath := filepath.Join(dir, "2026", "06", "27", "rollout-parentaa1234.jsonl")
	writeJSONL(t, parentPath, parentRecords...)

	// The fork replays the parent's two requests at its own timestamp, then makes
	// one of its own continuing the parent's running totals.
	forkRecords := append([]string{
		`{"timestamp":"2026-06-27T11:00:00Z","type":"session_meta","payload":{"id":"fork-id","forked_from_id":"parent-id","cwd":"/tmp/my-project"}}`,
	}, parentRecords[1:]...)
	forkRecords = append(forkRecords,
		`{"timestamp":"2026-06-27T11:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":200,"output_tokens":40,"cached_input_tokens":150},"total_token_usage":{"input_tokens":450,"output_tokens":90,"cached_input_tokens":280}}}}`)
	forkPath := filepath.Join(dir, "2026", "06", "27", "rollout-forkaaa12345.jsonl")
	writeJSONL(t, forkPath, forkRecords...)

	parent, fork := CodexParseFile(parentPath), CodexParseFile(forkPath)
	if parent == nil || fork == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if len(parent.UsageEvents) != 2 || len(fork.UsageEvents) != 3 {
		t.Fatalf("event counts = %d, %d", len(parent.UsageEvents), len(fork.UsageEvents))
	}
	for index, event := range parent.UsageEvents {
		if event.Fingerprint == "" {
			t.Fatalf("parent event %d has no fingerprint", index)
		}
		if fork.UsageEvents[index].Fingerprint != event.Fingerprint {
			t.Fatalf("replayed event %d fingerprint = %q, want %q",
				index, fork.UsageEvents[index].Fingerprint, event.Fingerprint)
		}
	}
	own := fork.UsageEvents[2].Fingerprint
	if own == "" || own == parent.UsageEvents[0].Fingerprint || own == parent.UsageEvents[1].Fingerprint {
		t.Fatalf("fork's own event fingerprint = %q", own)
	}
}

// One assistant response is written as several records, each repeating the same
// usage. message.id is what separates that from a second request.
func TestClaudeCountsARepeatedMessageIDOnce(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "sample-project", "0123456789abcdef.jsonl")
	writeJSONL(t, fp,
		`{"type":"user","timestamp":"2026-06-27T10:00:00Z","message":{"content":[{"type":"text","text":"Please build this feature"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:01Z","message":{"id":"msg_one","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"tool_use","name":"Edit"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-27T10:00:02Z","message":{"id":"msg_one","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"Done."}]}}`,
		// Same tuple as msg_one but a different response, and not adjacent to it:
		// the tuple alone cannot tell this apart from a repeat.
		`{"type":"assistant","timestamp":"2026-06-27T10:00:03Z","message":{"id":"msg_two","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"text","text":"More."}]}}`,
	)

	got := ClaudeParseFile(fp)
	if got == nil {
		t.Fatal("ClaudeParseFile returned nil")
	}
	if len(got.UsageEvents) != 2 || got.Usage.RequestCount != 2 || got.Usage.InputTokens != 20 {
		t.Fatalf("usage = %#v, events = %#v", got.Usage, got.UsageEvents)
	}
	if got.UsageEvents[0].Fingerprint != "claude:msg_one" || got.UsageEvents[1].Fingerprint != "claude:msg_two" {
		t.Fatalf("fingerprints = %#v", got.UsageEvents)
	}
}

func TestSkillNamesRejectShellPlaceholders(t *testing.T) {
	for _, input := range []string{
		`sed -n '1,20p' /tmp/skills/*/SKILL.md`,
		`sed -n '1,20p' /tmp/skills/$s/SKILL.md`,
	} {
		if got := skillFromText(input); got != "" {
			t.Fatalf("skillFromText(%q) = %q, want empty", input, got)
		}
	}
	if got := skillFromText(`/tmp/skills/atm/SKILL.md`); got != "atm" {
		t.Fatalf("valid skill = %q, want atm", got)
	}
}

func TestCodexParseFileContinuesAfterLargeRecord(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "2026", "07", "13", "rollout-large-record.jsonl")
	largeReasoning := `{"timestamp":"2026-07-13T07:01:00Z","type":"response_item","payload":{"type":"reasoning","encrypted_content":"` + strings.Repeat("x", 2*1024*1024) + `"}}`
	writeJSONL(t, fp,
		`{"timestamp":"2026-07-13T07:00:00Z","type":"session_meta","payload":{"cwd":"/tmp/atm"}}`,
		`{"timestamp":"2026-07-13T07:00:10Z","type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Keep parsing after the large record"}]}}`,
		`{"timestamp":"2026-07-13T07:00:20Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":20,"reasoning_output_tokens":5,"cached_input_tokens":40},"total_token_usage":{"input_tokens":100,"output_tokens":20,"reasoning_output_tokens":5,"cached_input_tokens":40}}}}`,
		largeReasoning,
		`{"timestamp":"2026-07-13T07:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":60,"output_tokens":15,"reasoning_output_tokens":5,"cached_input_tokens":30},"total_token_usage":{"input_tokens":160,"output_tokens":35,"reasoning_output_tokens":10,"cached_input_tokens":70}}}}`,
	)

	got := CodexParseFile(fp)
	if got == nil {
		t.Fatal("CodexParseFile returned nil")
	}
	if len(got.UsageEvents) != 2 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	if got.Usage.InputTokens != 90 || got.Usage.CacheReadTokens != 70 || got.Usage.OutputTokens != 35 || got.UsageEvents[1].OutputTokens != 15 {
		t.Fatalf("last-token usage = %#v, events = %#v", got.Usage, got.UsageEvents)
	}
	wantLastTS := time.Date(2026, 7, 13, 7, 2, 0, 0, time.UTC).Unix()
	if got.LastTS != wantLastTS {
		t.Fatalf("last ts = %d, want %d", got.LastTS, wantLastTS)
	}
}

func TestCodexSessionDayDirsCoversMidnightWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 6, 28, 0, 5, 0, 0, loc)
	got := codexSessionDayDirs("/sessions", now, 10*time.Minute)
	want := []string{
		filepath.Join("/sessions", "2026", "06", "27"),
		filepath.Join("/sessions", "2026", "06", "28"),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("dirs = %#v, want %#v", got, want)
	}
}

func TestCopilotParseFileExtractsTranscript(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "copilot-session.jsonl")
	writeJSONL(t, fp,
		`{"type":"session.start","data":{"startTime":"2026-06-27T10:00:00Z"}}`,
		`{"type":"user.message","data":{"createdAt":"2026-06-27T10:01:00Z","content":"How do I wire this?"}}`,
		`{"type":"assistant.message","data":{"createdAt":"2026-06-27T10:02:00Z","content":"Use the helper."}}`,
		`{"type":"assistant.message","data":{"createdAt":"2026-06-27T10:03:00Z","content":"Then run tests."}}`,
		`{"type":"tool.execution_start","data":{"toolName":"read_file","arguments":"{\"filePath\":\"/Users/test/.agents/skills/atm/SKILL.md\"}"}}`,
	)

	got := CopilotParseFile(fp, "atm")
	if got == nil {
		t.Fatal("CopilotParseFile returned nil")
	}
	if got.SessionID != "copilot-session" || got.ShortID != "copilot-" {
		t.Fatalf("unexpected ids: %#v", got)
	}
	if got.Project != "atm" {
		t.Fatalf("project = %q", got.Project)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Content != "How do I wire this?" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
	wantInputTS := time.Date(2026, 6, 27, 10, 1, 0, 0, time.UTC).Unix()
	if got.Inputs[0].TS != wantInputTS {
		t.Fatalf("input ts = %d, want %d", got.Inputs[0].TS, wantInputTS)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Content != "Use the helper.\n\nThen run tests." {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	wantOutputTS := time.Date(2026, 6, 27, 10, 3, 0, 0, time.UTC).Unix()
	if got.Outputs[0].TS != wantOutputTS {
		t.Fatalf("output ts = %d, want %d", got.Outputs[0].TS, wantOutputTS)
	}
	if got.Tools["read_file"] != 1 {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "atm" {
		t.Fatalf("skills = %#v", got.Skills)
	}
	wantCreated := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC).Unix()
	if got.CreatedTS != wantCreated {
		t.Fatalf("created ts = %d, want %d", got.CreatedTS, wantCreated)
	}
	if got.LastTS != wantOutputTS {
		t.Fatalf("last ts = %d, want %d", got.LastTS, wantOutputTS)
	}
}

func TestCopilotParseFileUsesWorkspaceScopedSessionID(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "workspace-a", "GitHub.copilot-chat", "transcripts", "same-session.jsonl")
	writeJSONL(t, fp,
		`{"type":"session.start","data":{"startTime":"2026-06-27T10:00:00Z"}}`,
		`{"type":"user.message","data":{"content":"Explain this file"}}`,
	)

	got := CopilotParseFile(fp, "atm")
	if got == nil {
		t.Fatal("CopilotParseFile returned nil")
	}
	if got.SessionID != "copilot:workspace-a:same-session" {
		t.Fatalf("session id = %q", got.SessionID)
	}
	if got.ShortID != "same-ses" {
		t.Fatalf("short id = %q", got.ShortID)
	}
}

func TestClaudeParseAppendReturnsNewRecordsOnCleanAppend(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "proj", "0123456789abcdef.jsonl")
	writeJSONL(t, fp,
		`{"type":"user","timestamp":"2026-07-10T10:00:00Z","message":{"content":[{"type":"text","text":"first question here"}]}}`,
	)
	before := fileSize(fp)
	appendLine(t, fp, `{"type":"assistant","timestamp":"2026-07-10T10:00:01Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":7,"output_tokens":3},"content":[{"type":"text","text":"an answer"}]}}`)

	got := ClaudeParseAppend(fp, before)
	if got == nil || !got.Append {
		t.Fatalf("append = %#v", got)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Content != "an answer" || got.Usage.InputTokens != 7 {
		t.Fatalf("append content = %#v", got)
	}
}

func TestClaudeParseAppendForcesFullReparseOnContinuationReplay(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "proj", "0123456789abcdef.jsonl")
	writeJSONL(t, fp,
		`{"type":"user","timestamp":"2026-07-10T10:00:00Z","message":{"content":[{"type":"text","text":"first question here"}]}}`,
	)
	before := fileSize(fp)
	// A compaction replay: the continuation summary marker followed by a replay
	// of the earlier message. Incremental append would double-store it, so the
	// parser must return nil and let sync fall back to a full (deduped) re-parse.
	appendLine(t, fp, `{"type":"user","timestamp":"2026-07-10T10:05:00Z","message":{"content":[{"type":"text","text":"This session is being continued from a previous conversation. Summary: ..."}]}}`)
	appendLine(t, fp, `{"type":"user","timestamp":"2026-07-10T10:05:01Z","message":{"content":[{"type":"text","text":"first question here"}]}}`)

	if got := ClaudeParseAppend(fp, before); got != nil {
		t.Fatalf("expected nil (force full re-parse) on replay, got %#v", got)
	}
}

func TestQoderCLIParseAppendReturnsOnlyNewRecords(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "proj", "session-abc.jsonl")
	writeJSONL(t, fp,
		`{"type":"user","timestamp":"2026-07-10T10:00:00Z","cwd":"/tmp/proj","message":{"role":"user","content":[{"type":"text","text":"initial question"}]}}`,
	)
	before := fileSize(fp)
	appendLine(t, fp, `{"type":"assistant","timestamp":"2026-07-10T10:00:02Z","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"new reply"}],"usage":{"input_tokens":9,"output_tokens":4}}}`)

	got := QoderCLIParseAppend(fp, before)
	if got == nil || !got.Append {
		t.Fatalf("append = %#v", got)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Content != "new reply" || got.Usage.InputTokens != 9 {
		t.Fatalf("append content = %#v", got)
	}
}

func TestOffsetOnRecordBoundary(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "s.jsonl")
	writeJSONL(t, fp, `{"a":1}`, `{"b":2}`)
	size := fileSize(fp)
	if !OffsetOnRecordBoundary(fp, size) {
		t.Fatal("append-only file size should land on a record boundary")
	}
	if OffsetOnRecordBoundary(fp, size-1) {
		t.Fatal("mid-record offset must not be reported as a boundary")
	}
	if OffsetOnRecordBoundary(fp, 0) {
		t.Fatal("zero offset is never a boundary")
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestGrokParseFileExtractsMessagesToolsSkillsAndUsage(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "%2Ftmp%2Fgrok-project", "019fa7cc-a6a2-7bb0-99c4-5a235935acaa")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	summary := `{
  "info": {"id": "019fa7cc-a6a2-7bb0-99c4-5a235935acaa", "cwd": "/tmp/grok-project"},
  "session_summary": "Wire Grok stats",
  "created_at": "2026-07-28T08:00:00Z",
  "updated_at": "2026-07-28T08:10:00Z",
  "current_model_id": "grok-4.5",
  "generated_title": "Wire Grok stats"
}`
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(summary), 0644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	chat := filepath.Join(sessionDir, "chat_history.jsonl")
	writeJSONL(t, chat,
		`{"type":"system","content":"You are Grok 4.5 released by xAI."}`,
		`{"type":"user","content":[{"type":"text","text":"<user_info>\nOS Version: macos\n</user_info>"}]}`,
		`{"type":"user","synthetic_reason":"system_reminder","content":[{"type":"text","text":"<system-reminder>\nskills list\n</system-reminder>"}]}`,
		`{"type":"user","prompt_index":0,"content":[{"type":"text","text":"<user_query>\nImplement grokbuild stats\n</user_query>"}]}`,
		`{"type":"assistant","model_id":"grok-4.5-build","content":"Looking up the skill.","tool_calls":[{"id":"call-1","name":"read_file","arguments":"{\"target_file\":\"/Users/test/.agents/skills/atm/SKILL.md\"}"}]}`,
		`{"type":"tool_result","tool_call_id":"call-1","content":"skill body"}`,
		`{"type":"assistant","model_id":"grok-4.5-build","content":"Done wiring the parser."}`,
	)
	updates := filepath.Join(sessionDir, "updates.jsonl")
	writeJSONL(t, updates,
		`{"timestamp":1785225600,"method":"_x.ai/session/update","params":{"sessionId":"019fa7cc-a6a2-7bb0-99c4-5a235935acaa","update":{"sessionUpdate":"turn_completed","prompt_id":"prompt-1","stop_reason":"end_turn","usage":{"inputTokens":1000,"outputTokens":50,"totalTokens":1050,"cachedReadTokens":200,"modelCalls":2,"apiDurationMs":8400,"modelUsage":{"grok-4.5-build":{"inputTokens":1000,"outputTokens":50,"totalTokens":1050,"cachedReadTokens":200,"modelCalls":2,"apiDurationMs":8400}}}},"_meta":{"eventId":"e1","agentTimestampMs":1785225600500}}}`,
		`{"timestamp":1785225660,"method":"_x.ai/session/update","params":{"sessionId":"019fa7cc-a6a2-7bb0-99c4-5a235935acaa","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"x"}},"_meta":{"totalTokens":123}}}`,
	)

	got := GrokParseFile(chat)
	if got == nil {
		t.Fatal("GrokParseFile returned nil")
	}
	if got.Agent != "grokbuild" {
		t.Fatalf("agent = %q", got.Agent)
	}
	if got.SessionID != "019fa7cc-a6a2-7bb0-99c4-5a235935acaa" || got.ShortID != "019fa7cc" {
		t.Fatalf("ids = %#v", got)
	}
	if got.Project != "grok-project" {
		t.Fatalf("project = %q", got.Project)
	}
	if got.Summary != "Wire Grok stats" {
		t.Fatalf("summary = %q", got.Summary)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Content != "Implement grokbuild stats" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
	if len(got.Outputs) != 2 {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	if got.Tools["read_file"] != 1 {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "atm" {
		t.Fatalf("skills = %#v", got.Skills)
	}
	if got.Skills[0].TS == 0 {
		t.Fatalf("skill timestamp should be inferred, got 0")
	}
	wantCreated := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC).Unix()
	wantUpdated := time.Date(2026, 7, 28, 8, 10, 0, 0, time.UTC).Unix()
	if got.Skills[0].TS < wantCreated || got.Skills[0].TS > wantUpdated {
		t.Fatalf("skill ts = %d, want in [%d, %d]", got.Skills[0].TS, wantCreated, wantUpdated)
	}
	if len(got.UsageEvents) != 1 {
		t.Fatalf("usage events = %#v", got.UsageEvents)
	}
	ev := got.UsageEvents[0]
	if ev.Model != "grok-4.5-build" || ev.InputTokens != 800 || ev.OutputTokens != 50 || ev.CacheReadTokens != 200 {
		t.Fatalf("usage event = %#v", ev)
	}
	if ev.RequestCount != 2 {
		t.Fatalf("usage event request count = %d, want 2 (modelCalls)", ev.RequestCount)
	}
	// Grok reports its own API time, so nothing is derived from timestamps here.
	// The figure covers both calls the row represents.
	if ev.DurationMS != 8400 {
		t.Fatalf("usage event duration = %d ms, want 8400 (apiDurationMs)", ev.DurationMS)
	}
	if ev.Fingerprint != "grokbuild:prompt-1:grok-4.5-build" {
		t.Fatalf("fingerprint = %q", ev.Fingerprint)
	}
	if got.Usage.InputTokens != 800 || got.Usage.OutputTokens != 50 || got.Usage.CacheReadTokens != 200 || got.Usage.RequestCount != 2 {
		t.Fatalf("usage = %#v", got.Usage)
	}
	if got.CreatedTS != wantCreated {
		t.Fatalf("created ts = %d, want %d", got.CreatedTS, wantCreated)
	}
}

func TestGrokSourceVersionTracksSiblingUpdates(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat_history.jsonl")
	updates := filepath.Join(dir, "updates.jsonl")
	summary := filepath.Join(dir, "summary.json")
	if err := os.WriteFile(chat, []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(updates, []byte("bb\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summary, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make updates appear newer than chat.
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(chat, past, past); err != nil {
		t.Fatal(err)
	}
	mtime, size, ok := GrokSourceVersion(chat)
	if !ok {
		t.Fatal("GrokSourceVersion returned ok=false")
	}
	chatInfo, _ := os.Stat(chat)
	updInfo, _ := os.Stat(updates)
	sumInfo, _ := os.Stat(summary)
	wantSize := chatInfo.Size() + updInfo.Size() + sumInfo.Size()
	if size != wantSize {
		t.Fatalf("size = %d, want %d", size, wantSize)
	}
	wantMtime := chatInfo.ModTime().Unix()
	if updInfo.ModTime().Unix() > wantMtime {
		wantMtime = updInfo.ModTime().Unix()
	}
	if sumInfo.ModTime().Unix() > wantMtime {
		wantMtime = sumInfo.ModTime().Unix()
	}
	if mtime != wantMtime {
		t.Fatalf("mtime = %d, want %d", mtime, wantMtime)
	}
	if !updInfo.ModTime().After(chatInfo.ModTime()) {
		t.Fatal("test setup: updates should be newer than chat")
	}
	if mtime == chatInfo.ModTime().Unix() {
		t.Fatal("mtime should prefer newer sibling over chat")
	}
}

func TestDiscoverGrokFindsChatHistory(t *testing.T) {
	old := config.GrokSessions
	t.Cleanup(func() { config.GrokSessions = old })
	root := t.TempDir()
	config.GrokSessions = root
	sessionDir := filepath.Join(root, "%2Ftmp%2Fproj", "session-abc")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(sessionDir, "chat_history.jsonl")
	if err := os.WriteFile(chat, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// noise file should be ignored
	if err := os.WriteFile(filepath.Join(root, "session_search.sqlite"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// ATM's own chat classifier runs Grok in a scratch directory, which Grok
	// records as a session like any other. It is not work anyone did, so it
	// must never reach the session index.
	classifierDir := filepath.Join(root,
		"%2Fprivate%2Fvar%2Ffolders%2Fkq%2FT%2F"+config.CollectionModelWorkdirPrefix+"2291227821",
		"session-def")
	if err := os.MkdirAll(classifierDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(classifierDir, "chat_history.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := DiscoverGrok()
	if len(got) != 1 || got[0] != chat {
		t.Fatalf("DiscoverGrok = %#v", got)
	}
}

func TestGrokQuotaReadsLatestBillingConfig(t *testing.T) {
	old := config.GrokSessions
	t.Cleanup(func() { config.GrokSessions = old })
	root := t.TempDir()
	// GrokLogPath is sibling logs/ under the sessions parent.
	config.GrokSessions = filepath.Join(root, "sessions")
	logPath := filepath.Join(root, "logs", "unified.jsonl")
	writeJSONL(t, logPath,
		`{"ts":"2026-07-28T08:00:00.000Z","src":"shell","msg":"noise"}`,
		`{"ts":"2026-07-28T08:10:00.000Z","src":"shell","msg":"billing: fetched credits config","ctx":{"subscriptionTier":"SuperGrok","config":{"creditUsagePercent":1.0,"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-07-28T07:28:53.500083+00:00","end":"2026-08-04T07:28:53.500083+00:00"},"billingPeriodStart":"2026-07-28T07:28:53.500083+00:00","billingPeriodEnd":"2026-08-04T07:28:53.500083+00:00","onDemandCap":{"val":0},"onDemandUsed":{"val":0}}}}`,
		// Later sample with higher usage and an on-demand window.
		`{"ts":"2026-07-29T01:00:00.000Z","src":"shell","msg":"billing: fetched credits config","ctx":{"subscriptionTier":"SuperGrok","config":{"creditUsagePercent":42.5,"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-07-28T07:28:53.500083+00:00","end":"2026-08-04T07:28:53.500083+00:00"},"billingPeriodStart":"2026-07-28T07:28:53.500083+00:00","billingPeriodEnd":"2026-08-04T07:28:53.500083+00:00","onDemandCap":{"val":20},"onDemandUsed":{"val":5}}}}`,
	)

	got := GrokQuota()
	if got == nil {
		t.Fatal("GrokQuota returned nil")
	}
	if got.Plan != "SuperGrok" {
		t.Fatalf("plan = %q", got.Plan)
	}
	if got.Primary == nil {
		t.Fatal("missing primary window")
	}
	if got.Primary.UsedPercent != 42.5 {
		t.Fatalf("primary used = %v", got.Primary.UsedPercent)
	}
	// Weekly period from the sample is exactly 7 days.
	if got.Primary.WindowMinutes != 7*24*60 {
		t.Fatalf("window minutes = %d", got.Primary.WindowMinutes)
	}
	wantReset := time.Date(2026, 8, 4, 7, 28, 53, 500083000, time.UTC).Unix()
	if got.Primary.ResetsAt != wantReset {
		t.Fatalf("resets_at = %d want %d", got.Primary.ResetsAt, wantReset)
	}
	if got.Secondary == nil {
		t.Fatal("expected secondary on-demand window")
	}
	if got.Secondary.UsedPercent != 25 {
		t.Fatalf("secondary used = %v", got.Secondary.UsedPercent)
	}
}

func TestGrokQuotaMissingLogReturnsNil(t *testing.T) {
	old := config.GrokSessions
	t.Cleanup(func() { config.GrokSessions = old })
	config.GrokSessions = filepath.Join(t.TempDir(), "sessions")
	if got := GrokQuota(); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}
