package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestSessionSearchDefaultsBoundJSONOutput(t *testing.T) {
	if got := searchCmd.Flags().Lookup("limit").DefValue; got != "50" {
		t.Fatalf("search --limit default = %q, want 50", got)
	}
	if got := searchCmd.Flags().Lookup("snippet").DefValue; got != "400" {
		t.Fatalf("search --snippet default = %q, want 400", got)
	}
}

func TestSessionSearchJSONAppliesFiltersAndSnippetBudget(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	jsonOutput = true
	searchProjectFlag = "atm"
	searchRoleFlag = "assistant"
	// See seedCommandSession: an hour-old seed needs a window that survives midnight.
	searchDaysFlag = 2
	searchLimitFlag = 1
	searchSnippetFlag = 18

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSearch(searchCmd, []string{"deployment"})
	})
	if runErr != nil {
		t.Fatalf("runSearch: %v", runErr)
	}
	var payload struct {
		Keyword   string `json:"keyword"`
		Total     int    `json:"total"`
		Returned  int    `json:"returned"`
		Truncated bool   `json:"truncated"`
		Matches   []struct {
			ID               string `json:"id"`
			ShortID          string `json:"short_id"`
			CreatedAt        string `json:"created_at"`
			Role             string `json:"role"`
			Content          string `json:"content"`
			SnippetTruncated bool   `json:"snippet_truncated"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal search output: %v\n%s", err, out)
	}
	hits := payload.Matches
	if len(hits) != 1 || hits[0].ID != "cmd-session-full" || hits[0].ShortID != "cmdsess" ||
		hits[0].Role != "assistant" || !hits[0].SnippetTruncated {
		t.Fatalf("search hits = %#v", hits)
	}
	if payload.Keyword != "deployment" || payload.Returned != 1 {
		t.Fatalf("search envelope = %#v", payload)
	}
	if _, err := time.Parse(time.RFC3339, hits[0].CreatedAt); err != nil {
		t.Fatalf("created_at is not RFC3339: %q (%v)", hits[0].CreatedAt, err)
	}
	if got := len([]rune(hits[0].Content)); got > searchSnippetFlag {
		t.Fatalf("snippet length = %d, want <= %d: %q", got, searchSnippetFlag, hits[0].Content)
	}
	if !strings.Contains(strings.ToLower(hits[0].Content), "deployment") {
		t.Fatalf("snippet lost the match: %q", hits[0].Content)
	}
}

// A bounded page that reports its own length as the match count tells the
// caller the search is exhausted when it hid most of the hits. The total has to
// survive the limit, and boilerplate that never reaches the caller must not be
// counted into it either.
func TestSessionSearchReportsTotalBeyondLimit(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	createdTS := time.Now().In(config.Loc).Add(-time.Hour).Unix()
	for seq, content := range []string{
		"Another deployment mention",
		"Yet another deployment mention",
		"<system-reminder>deployment</system-reminder>",
	} {
		if _, err := db.Exec(
			"INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)",
			"cmd-session-full", seq+10, "assistant", content, createdTS+int64(seq)+10,
		); err != nil {
			db.Close()
			t.Fatalf("insert message: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	jsonOutput = true
	searchLimitFlag = 2

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSearch(searchCmd, []string{"deployment"})
	})
	if runErr != nil {
		t.Fatalf("runSearch: %v", runErr)
	}
	var payload struct {
		Total     int `json:"total"`
		Returned  int `json:"returned"`
		Limit     int `json:"limit"`
		Truncated bool
		Matches   []struct {
			Content string `json:"content"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal search output: %v\n%s", err, out)
	}
	// Four real mentions, the fifth is a system reminder cleanMsg drops.
	if payload.Total != 4 {
		t.Fatalf("total = %d, want 4 (limit and filtered noise must not change it)\n%s", payload.Total, out)
	}
	if payload.Returned != 2 || len(payload.Matches) != 2 {
		t.Fatalf("returned = %d with %d matches, want 2", payload.Returned, len(payload.Matches))
	}
	if !payload.Truncated || payload.Limit != 2 {
		t.Fatalf("payload must admit truncation at the limit: %#v", payload)
	}

	// The limit reserves the page for real matches; a filtered row must not
	// silently consume one of the slots.
	for _, match := range payload.Matches {
		if strings.Contains(match.Content, "system-reminder") {
			t.Fatalf("filtered boilerplate reached the page: %q", match.Content)
		}
	}

	jsonOutput = false
	text := captureStdout(t, func() {
		runErr = runSearch(searchCmd, []string{"deployment"})
	})
	if runErr != nil {
		t.Fatalf("runSearch text: %v", runErr)
	}
	if !strings.Contains(text, "(2 of 4 matches") {
		t.Fatalf("text header hides the truncation:\n%s", text)
	}
}

func TestSessionShowJSONAppliesTurnAndCharacterBudgets(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	createdTS := time.Now().In(config.Loc).Add(-time.Hour).Unix()
	for _, row := range []struct {
		seq     int
		role    string
		content string
	}{
		{2, "user", "Second question has enough text to hit the budget"},
		{3, "assistant", "Second answer"},
		{4, "user", "Third question"},
		{5, "assistant", "Third answer"},
	} {
		if _, err := db.Exec(
			"INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)",
			"cmd-session-full", row.seq, row.role, row.content, createdTS+int64(row.seq),
		); err != nil {
			db.Close()
			t.Fatalf("insert message: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	jsonOutput = true
	showTurnsFlag = "2-3"
	showMaxCharsFlag = 20
	var runErr error
	out := captureStdout(t, func() {
		runErr = runShow(showCmd, []string{"cmdsess"})
	})
	if runErr != nil {
		t.Fatalf("runShow range: %v", runErr)
	}
	var ranged struct {
		TotalTurns    int  `json:"total_turns"`
		ReturnedTurns int  `json:"returned_turns"`
		Truncated     bool `json:"truncated"`
		QA            []struct {
			Turn int    `json:"turn"`
			Q    string `json:"q"`
		} `json:"qa"`
	}
	if err := json.Unmarshal([]byte(out), &ranged); err != nil {
		t.Fatalf("unmarshal ranged show: %v\n%s", err, out)
	}
	if ranged.TotalTurns != 3 || ranged.ReturnedTurns != 1 || !ranged.Truncated ||
		len(ranged.QA) != 1 || ranged.QA[0].Turn != 2 || len([]rune(ranged.QA[0].Q)) > 20 {
		t.Fatalf("ranged show = %#v", ranged)
	}

	showTurnsFlag = ""
	showMaxCharsFlag = 0
	showLastFlag = 1
	out = captureStdout(t, func() {
		runErr = runShow(showCmd, []string{"cmdsess"})
	})
	if runErr != nil {
		t.Fatalf("runShow last: %v", runErr)
	}
	var last struct {
		TotalTurns    int `json:"total_turns"`
		ReturnedTurns int `json:"returned_turns"`
		QA            []struct {
			Turn int `json:"turn"`
		} `json:"qa"`
	}
	if err := json.Unmarshal([]byte(out), &last); err != nil {
		t.Fatalf("unmarshal last show: %v\n%s", err, out)
	}
	if last.TotalTurns != 3 || last.ReturnedTurns != 1 || len(last.QA) != 1 || last.QA[0].Turn != 3 {
		t.Fatalf("last show = %#v", last)
	}
}

func TestMatchSnippetIsUnicodeSafeAndCentered(t *testing.T) {
	snippet, truncated := matchSnippet("开头甲乙丙丁关键字戊己庚辛结尾", "关键字", 8)
	if !truncated || len([]rune(snippet)) > 8 || !strings.Contains(snippet, "关键字") {
		t.Fatalf("snippet = %q, truncated = %v", snippet, truncated)
	}
}

func TestParseTurnRange(t *testing.T) {
	start, end, err := parseTurnRange("2-5")
	if err != nil || start != 2 || end != 5 {
		t.Fatalf("parseTurnRange = %d-%d, %v", start, end, err)
	}
	for _, invalid := range []string{"0", "3-2", "x-y", "1-2-3"} {
		if _, _, err := parseTurnRange(invalid); err == nil {
			t.Errorf("parseTurnRange(%q) unexpectedly succeeded", invalid)
		}
	}
}

// The index is browsed, not just triaged: paging over the whole thing in
// newest-first order is what makes a session reachable after it leaves the
// activity window, and the printed total must describe the window rather than
// the page so a capped page never reads as "that is all there is".
func TestSessionListPagesTheWholeIndexNewestFirst(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	// Far outside any --days window, which is exactly what --all has to reach.
	old := time.Now().In(config.Loc).AddDate(0, -6, 0)
	if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
		VALUES ('cmd-session-old', 'cmdold', 'codex', 'atm', '', ?, ?, 'Ancient session', ?)`,
		old.Format("01-02 15:04"), old.Unix(), old.Unix()+60); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	sessionListAllFlag = true
	sessionListOrder = "desc"
	sessionListLimit = 1

	jsonOutput = true
	page := captureStdout(t, func() {
		if err := runList(listCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(page), &rows); err != nil {
		t.Fatalf("decode page: %v\n%s", err, page)
	}
	if len(rows) != 1 || rows[0].ID != "cmd-session-full" {
		t.Fatalf("newest page = %#v", rows)
	}

	sessionListOffset = 1
	second := captureStdout(t, func() {
		if err := runList(listCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	rows = nil
	if err := json.Unmarshal([]byte(second), &rows); err != nil {
		t.Fatalf("decode second page: %v\n%s", err, second)
	}
	if len(rows) != 1 || rows[0].ID != "cmd-session-old" {
		t.Fatalf("second page = %#v", rows)
	}

	jsonOutput = false
	sessionListOffset = 0
	text := captureStdout(t, func() {
		if err := runList(listCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(text, "Sessions (all, 2 total)") || !strings.Contains(text, "Showing 1-1") {
		t.Fatalf("text output = %q", text)
	}

	sessionListOrder = "sideways"
	if err := runList(listCmd, nil); err == nil {
		t.Fatal("an unknown --order was accepted")
	}
}

// `--thinking` routed on the display name ("Grok Build") while the switch compared
// stored keys ("grokbuild"), so every transcript fell through to Claude's
// extractor and no agent ever showed a thinking chain.
func TestExtractSessionThinkingRoutesOnTheStoredAgentKey(t *testing.T) {
	dir := t.TempDir()
	reasoning := filepath.Join(dir, "grok.jsonl")
	if err := os.WriteFile(reasoning, []byte(
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"想清楚再动手"}]}`+"\n"+
			`{"type":"assistant","content":"做完了"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"grokbuild", "codex"} {
		blocks := extractSessionThinking(agent, reasoning)
		if len(blocks) != 1 || blocks[0].Thinking != "想清楚再动手" {
			t.Fatalf("%s blocks = %#v", agent, blocks)
		}
	}
	// The display name must not resolve: it would silently pick Claude's shape.
	if blocks := extractSessionThinking("Grok Build", reasoning); len(blocks) != 0 {
		t.Fatalf("display name matched a reasoning extractor: %#v", blocks)
	}
}

// Reasoning models emit a block per model response and a turn spans several of
// them, so one-block-per-turn attributed later turns' thinking to earlier ones.
func TestCollectTurnThinkingGroupsEveryBlockOfATurn(t *testing.T) {
	blocks := []parser.ThinkingBlock{
		{Thinking: "看一下文件", Response: "先读代码"},
		{Thinking: "改这里", Response: "改完了"},
		{Thinking: "下一轮", Response: "第二轮答案"},
	}
	thinking, next := collectTurnThinking(blocks, 0, "改完了")
	if !strings.Contains(thinking, "看一下文件") || !strings.Contains(thinking, "改这里") ||
		strings.Contains(thinking, "下一轮") || next != 2 {
		t.Fatalf("thinking = %q next = %d", thinking, next)
	}
	// An answer no block claims must not swallow the rest of the chain.
	thinking, next = collectTurnThinking(blocks, 2, "无人认领")
	if thinking != "下一轮" || next != 3 {
		t.Fatalf("fallback thinking = %q next = %d", thinking, next)
	}
	if thinking, next := collectTurnThinking(blocks, 3, "改完了"); thinking != "" || next != 3 {
		t.Fatalf("exhausted blocks = %q %d", thinking, next)
	}
}
