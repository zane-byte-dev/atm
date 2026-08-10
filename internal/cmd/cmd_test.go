package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data)
}

func withTempAtmDir(t *testing.T) {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
	})
}

// seedTodos installs items as the whole todos table through the production write
// path, so fixtures are subject to the same constraints as real writes.
func seedTodos(items ...store.Todo) error {
	return workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		transaction.Todos().Items = items
		return nil
	})
}

func withIsolatedCommandEnv(t *testing.T) {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	oldClaude, oldCodex, oldCopilot, oldPi := config.ClaudeProjects, config.CodexSessions, config.CopilotWorkspaces, config.PiSessions
	oldQoder, oldQoderCLI, oldQoderWork := config.QoderDB, config.QoderCLIProjects, config.QoderWorkDB
	oldGrok := config.GrokSessions
	dir := t.TempDir()
	config.AtmDir = filepath.Join(dir, "atm")
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	config.ClaudeProjects = filepath.Join(dir, "claude-projects")
	config.CodexSessions = filepath.Join(dir, "codex-sessions")
	config.CopilotWorkspaces = filepath.Join(dir, "copilot-workspaces")
	config.PiSessions = filepath.Join(dir, "pi-sessions")
	config.QoderDB = filepath.Join(dir, "qoder", "local.db")
	config.QoderCLIProjects = filepath.Join(dir, "qodercli-projects")
	config.QoderWorkDB = filepath.Join(dir, "qoderwork", "agents.db")
	config.GrokSessions = filepath.Join(dir, "grok-sessions")
	for _, p := range []string{config.ClaudeProjects, config.CodexSessions, config.CopilotWorkspaces, config.PiSessions, config.QoderCLIProjects, config.GrokSessions} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
		config.ClaudeProjects = oldClaude
		config.CodexSessions = oldCodex
		config.CopilotWorkspaces = oldCopilot
		config.PiSessions = oldPi
		config.QoderDB, config.QoderCLIProjects, config.QoderWorkDB = oldQoder, oldQoderCLI, oldQoderWork
		config.GrokSessions = oldGrok
	})
}

func seedCommandSession(t *testing.T) int64 {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	created := time.Now().In(config.Loc).Add(-time.Hour)
	createdTS := created.Unix()
	stmts := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			args: []any{"cmd-session-full", "cmdsess", "codex", "atm", filepath.Join(config.AtmDir, "cmd-session.jsonl"),
				created.Format("01-02 15:04"), createdTS, "Seeded command session", createdTS + 120},
		},
		{
			query: "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)",
			args:  []any{"cmd-session-full", 0, "user", "Find deployment keyword in command tests", createdTS + 30},
		},
		{
			query: "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)",
			args:  []any{"cmd-session-full", 1, "assistant", "Deployment keyword answer from assistant", createdTS + 60},
		},
		{
			query: "INSERT INTO tools (session_id, name, count) VALUES (?, ?, ?)",
			args:  []any{"cmd-session-full", "exec_command", 2},
		},
		{
			query: "INSERT INTO skill_events (session_id, name, ts) VALUES (?, ?, ?)",
			args:  []any{"cmd-session-full", "atm", createdTS + 45},
		},
		{
			query: "INSERT INTO skill_events (session_id, name, ts) VALUES (?, ?, ?)",
			args:  []any{"cmd-session-full", "$s", createdTS + 46},
		},
		{
			query: `INSERT INTO usage (session_id, model, input_tokens, output_tokens,
				cache_create_tokens, cache_read_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			args: []any{"cmd-session-full", "claude-sonnet-4-6", int64(1000), int64(200), int64(0), int64(0), 0.006},
		},
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}
	return createdTS
}

func withCommandFlags(t *testing.T) {
	t.Helper()
	oldAgent, oldJSON, oldSync := agentFlag, jsonOutput, syncBeforeRead
	oldDays, oldProject := daysFlag, projectFlag
	oldSince, oldReview := sessionSinceFlag, sessionReviewFlag
	oldSearchLimit, oldSearchProject := searchLimitFlag, searchProjectFlag
	oldSearchSince, oldSearchDays := searchSinceFlag, searchDaysFlag
	oldSearchRole, oldSearchSnippet := searchRoleFlag, searchSnippetFlag
	oldStatsDays, oldStatsBy := statsDaysFlag, statsByFlag
	oldExportDays, oldExportFormat := exportDaysFlag, exportFormatFlag
	oldThinking, oldShowTurns := showThinking, showTurnsFlag
	oldShowLast, oldShowMaxChars := showLastFlag, showMaxCharsFlag
	t.Cleanup(func() {
		agentFlag = oldAgent
		jsonOutput = oldJSON
		syncBeforeRead = oldSync
		daysFlag = oldDays
		projectFlag = oldProject
		sessionSinceFlag = oldSince
		sessionReviewFlag = oldReview
		searchLimitFlag = oldSearchLimit
		searchProjectFlag = oldSearchProject
		searchSinceFlag = oldSearchSince
		searchDaysFlag = oldSearchDays
		searchRoleFlag = oldSearchRole
		searchSnippetFlag = oldSearchSnippet
		statsDaysFlag = oldStatsDays
		statsByFlag = oldStatsBy
		exportDaysFlag = oldExportDays
		exportFormatFlag = oldExportFormat
		showThinking = oldThinking
		showTurnsFlag = oldShowTurns
		showLastFlag = oldShowLast
		showMaxCharsFlag = oldShowMaxChars
	})
	agentFlag = ""
	jsonOutput = false
	syncBeforeRead = false
	daysFlag = 1
	projectFlag = ""
	sessionSinceFlag = ""
	sessionReviewFlag = "all"
	searchLimitFlag = defaultSearchLimit
	searchProjectFlag = ""
	searchSinceFlag = ""
	searchDaysFlag = 0
	searchRoleFlag = ""
	searchSnippetFlag = defaultSearchSnippet
	statsDaysFlag = 1
	statsByFlag = ""
	exportDaysFlag = 7
	exportFormatFlag = "json"
	showThinking = false
	showTurnsFlag = ""
	showLastFlag = 0
	showMaxCharsFlag = 0
}

// `--by speed` reports two tables and, in JSON, one object holding both plus what
// it could not measure. A caller that sees only the rates would read a partially
// measured agent as a fully measured one.
func TestStatsBySpeedReportsRatesWaitsAndWhatWasLeftOut(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	createdTS := seedCommandSession(t)

	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	requests := [][3]int64{
		{createdTS + 40, 400, 10_000}, // 40 tok/s
		{createdTS + 70, 600, 10_000}, // 60 tok/s
		{createdTS + 90, 300, 0},      // unmeasurable
	}
	for i, r := range requests {
		if _, err := db.Exec(`INSERT INTO usage_events (session_id, model, ts, input_tokens, output_tokens,
			cost_usd, fingerprint, request_count, duration_ms)
			VALUES ('cmd-session-full', 'gpt-5-codex', ?, 100, ?, 0, ?, 1, ?)`,
			r[0], r[1], fmt.Sprintf("fp-%d", i), r[2]); err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}
	db.Close()

	statsDaysFlag = 2
	statsByFlag = "speed"

	text := captureStdout(t, func() {
		if err := runStats(statsCmd, nil); err != nil {
			t.Fatalf("runStats speed: %v", err)
		}
	})
	for _, want := range []string{"gpt-5-codex", "Turn wait", "Left out: 1 requests"} {
		if !strings.Contains(text, want) {
			t.Fatalf("speed table missing %q:\n%s", want, text)
		}
	}

	jsonOutput = true
	out := captureStdout(t, func() {
		if err := runStats(statsCmd, nil); err != nil {
			t.Fatalf("runStats speed json: %v", err)
		}
	})
	var report store.SpeedReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal speed report: %v\n%s", err, out)
	}
	if len(report.Models) != 1 || report.Models[0].Model != "gpt-5-codex" {
		t.Fatalf("models = %#v", report.Models)
	}
	if report.Models[0].Requests != 3 || report.Models[0].Sampled != 2 {
		t.Fatalf("requests = %d, sampled = %d; want 3 and 2",
			report.Models[0].Requests, report.Models[0].Sampled)
	}
	if report.Models[0].TokensPerSecondP50 != 40 {
		t.Fatalf("p50 = %.1f, want 40", report.Models[0].TokensPerSecondP50)
	}
	if report.Untimed != 1 {
		t.Fatalf("untimed = %d, want 1", report.Untimed)
	}
	// The seeded session has one user message, so the whole run is one turn.
	if len(report.Turns) != 1 || report.Turns[0].Turns != 1 {
		t.Fatalf("turns = %#v", report.Turns)
	}
}

func TestVersionCommandUsesConfiguredVersion(t *testing.T) {
	oldVersion := rootCmd.Version
	t.Cleanup(func() {
		rootCmd.Version = oldVersion
	})

	SetVersion("9.9.9-test")
	out := captureStdout(t, func() {
		versionCmd.Run(versionCmd, nil)
	})
	if out != "atm 9.9.9-test\n" {
		t.Fatalf("version output = %q", out)
	}
}

func TestStatsDefaultsToToday(t *testing.T) {
	daysFlag := statsCmd.Flags().Lookup("days")
	if daysFlag == nil {
		t.Fatal("stats --days flag not found")
	}
	if daysFlag.DefValue != "1" {
		t.Fatalf("stats --days default = %q, want 1", daysFlag.DefValue)
	}
}

func TestStatsRejectsUnknownGroup(t *testing.T) {
	previous := statsByFlag
	t.Cleanup(func() { statsByFlag = previous })
	statsByFlag = "model-typo"

	err := runStats(statsCmd, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown stats group "model-typo"`) {
		t.Fatalf("runStats error = %v", err)
	}
	// The message lists what is valid, so a group added later has to appear in it.
	if !strings.Contains(err.Error(), "speed") {
		t.Fatalf("error does not offer the speed group: %v", err)
	}
}

func TestReadCommandsUseSeededDatabase(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	jsonOutput = true
	daysFlag = 2
	projectFlag = "atm"
	var runErr error
	listOut := captureStdout(t, func() {
		runErr = runList(listCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("runList: %v", runErr)
	}
	var listRows []struct {
		ShortID string `json:"short_id"`
		Project string `json:"project"`
		QCount  int    `json:"q_count"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(listOut), &listRows); err != nil {
		t.Fatalf("unmarshal list: %v\n%s", err, listOut)
	}
	if len(listRows) != 1 || listRows[0].ShortID != "cmdsess" || listRows[0].Project != "atm" ||
		listRows[0].QCount != 1 || listRows[0].Summary != "Seeded command session" {
		t.Fatalf("list rows = %#v", listRows)
	}

	searchOut := captureStdout(t, func() {
		runErr = runSearch(searchCmd, []string{"deployment"})
	})
	if runErr != nil {
		t.Fatalf("runSearch: %v", runErr)
	}
	var searchPayload struct {
		Total     int  `json:"total"`
		Returned  int  `json:"returned"`
		Truncated bool `json:"truncated"`
		Matches   []struct {
			ShortID string `json:"short_id"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(searchOut), &searchPayload); err != nil {
		t.Fatalf("unmarshal search: %v\n%s", err, searchOut)
	}
	searchRows := searchPayload.Matches
	if len(searchRows) != 2 || searchRows[0].ShortID != "cmdsess" || searchRows[1].Role != "assistant" {
		t.Fatalf("search rows = %#v", searchRows)
	}
	if searchPayload.Total != 2 || searchPayload.Returned != 2 || searchPayload.Truncated {
		t.Fatalf("search envelope = %#v", searchPayload)
	}

	showOut := captureStdout(t, func() {
		runErr = runShow(showCmd, []string{"cmdsess"})
	})
	if runErr != nil {
		t.Fatalf("runShow: %v", runErr)
	}
	var showRow struct {
		ID      string         `json:"id"`
		Agent   string         `json:"agent"`
		Project string         `json:"project"`
		Tools   map[string]int `json:"tools"`
		QA      []struct {
			Q string `json:"q"`
			A string `json:"a"`
		} `json:"qa"`
	}
	if err := json.Unmarshal([]byte(showOut), &showRow); err != nil {
		t.Fatalf("unmarshal show: %v\n%s", err, showOut)
	}
	if showRow.ID != "cmd-session-full" || showRow.Agent != "Codex" || showRow.Project != "atm" ||
		showRow.Tools["exec_command"] != 2 || len(showRow.QA) != 1 ||
		!strings.Contains(showRow.QA[0].A, "Deployment keyword answer") {
		t.Fatalf("show row = %#v", showRow)
	}

	statsDaysFlag = 2
	statsOut := captureStdout(t, func() {
		runErr = runStats(statsCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("runStats: %v", runErr)
	}
	var statsRows []store.StatsResult
	if err := json.Unmarshal([]byte(statsOut), &statsRows); err != nil {
		t.Fatalf("unmarshal stats: %v\n%s", err, statsOut)
	}
	if len(statsRows) != 1 || statsRows[0].Project != "atm" || statsRows[0].Agent != "codex" ||
		statsRows[0].Sessions != 1 || statsRows[0].Queries != 1 || statsRows[0].ToolCalls != 2 {
		t.Fatalf("stats rows = %#v", statsRows)
	}

	statsByFlag = "skill"
	skillOut := captureStdout(t, func() {
		runErr = runStats(statsCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("runStats skill: %v", runErr)
	}
	var skillRows []store.SkillStatsResult
	if err := json.Unmarshal([]byte(skillOut), &skillRows); err != nil {
		t.Fatalf("unmarshal skill stats: %v\n%s", err, skillOut)
	}
	if len(skillRows) != 1 || skillRows[0].Skill != "atm" || skillRows[0].Calls != 1 || skillRows[0].Sessions != 1 {
		t.Fatalf("skill stats = %#v", skillRows)
	}
	statsByFlag = ""

	exportDaysFlag = 2
	exportFormatFlag = "json"
	exportOut := captureStdout(t, func() {
		runErr = runExport(exportCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("runExport: %v", runErr)
	}
	var exportRows []store.ExportRow
	if err := json.Unmarshal([]byte(exportOut), &exportRows); err != nil {
		t.Fatalf("unmarshal export: %v\n%s", err, exportOut)
	}
	if len(exportRows) != 2 || exportRows[0].SessionID != "cmdsess" || exportRows[0].Role != "user" ||
		exportRows[1].Role != "assistant" {
		t.Fatalf("export rows = %#v", exportRows)
	}
}

func TestSessionReviewFiltersPendingSessions(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	oldOutcome, oldNote := sessionReviewOutcome, sessionReviewNote
	t.Cleanup(func() {
		sessionReviewOutcome, sessionReviewNote = oldOutcome, oldNote
	})
	seedCommandSession(t)
	jsonOutput = true
	daysFlag = 2

	sessionReviewOutcome = "memory"
	sessionReviewNote = "stored a stable decision"
	var runErr error
	reviewOut := captureStdout(t, func() {
		runErr = runSessionReview(sessionReviewCmd, []string{"cmdsess"})
	})
	if runErr != nil {
		t.Fatalf("review session: %v", runErr)
	}
	var review struct {
		SessionID string `json:"sessionId"`
		Outcome   string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(reviewOut), &review); err != nil {
		t.Fatalf("review output = %q: %v", reviewOut, err)
	}
	if review.SessionID != "cmd-session-full" || review.Outcome != "memory" {
		t.Fatalf("review = %#v", review)
	}

	sessionReviewFlag = "pending"
	pendingOut := captureStdout(t, func() {
		runErr = runList(listCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("list pending: %v", runErr)
	}
	var pending []map[string]any
	if err := json.Unmarshal([]byte(pendingOut), &pending); err != nil {
		t.Fatalf("pending output = %q: %v", pendingOut, err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending sessions = %#v", pending)
	}

	sessionReviewFlag = "reviewed"
	reviewedOut := captureStdout(t, func() {
		runErr = runList(listCmd, nil)
	})
	if runErr != nil || !strings.Contains(reviewedOut, `"outcome": "memory"`) {
		t.Fatalf("reviewed output = %q, err = %v", reviewedOut, runErr)
	}
}

func TestParseSessionSince(t *testing.T) {
	date, err := parseSessionSince("2026-07-14")
	if err != nil || date.Year() != 2026 || date.Month() != time.July || date.Day() != 14 {
		t.Fatalf("date = %s, err = %v", date, err)
	}
	stamp, err := parseSessionSince("2026-07-14T10:00:00+08:00")
	if err != nil || stamp.Hour() != 10 {
		t.Fatalf("stamp = %s, err = %v", stamp, err)
	}
	if _, err := parseSessionSince("yesterday"); err == nil {
		t.Fatal("invalid --since should fail")
	}
}

func TestResolveAgentNormalizesAliases(t *testing.T) {
	old := agentFlag
	t.Cleanup(func() {
		agentFlag = old
	})

	tests := map[string]string{
		"":               "",
		"Claude-Code":    "claude",
		"openai-codex":   "codex",
		"github-copilot": "copilot",
		"qoder-cli":      "qodercli",
		"qoder-work":     "qoderwork",
		"grok-build":     "grokbuild",
		"grok":           "grokbuild",
	}
	for in, want := range tests {
		agentFlag = in
		got, err := resolveAgent()
		if err != nil {
			t.Fatalf("resolveAgent(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("resolveAgent(%q) = %q, want %q", in, got, want)
		}
	}

	agentFlag = "unknown"
	if _, err := resolveAgent(); err == nil {
		t.Fatal("resolveAgent(unknown) expected error")
	}
}

func TestFormattingHelpers(t *testing.T) {
	if got := formatAge(75); got != "1m ago" {
		t.Fatalf("formatAge = %q", got)
	}
	if got := truncLine("你好世界\nsecond", 2); got != "你好" {
		t.Fatalf("truncLine = %q", got)
	}
	if got := cleanMsg("before <system-reminder>secret</system-reminder> after"); got != "before  after" {
		t.Fatalf("cleanMsg = %q", got)
	}
	if got := cleanMsg("<recommended_plugins>\nnot user activity"); got != "" {
		t.Fatalf("cleanMsg injected context = %q", got)
	}
	if got := cleanMsg(`<image name=[Image #1] path="/tmp/screenshot.png">`); got != "" {
		t.Fatalf("cleanMsg image placeholder = %q", got)
	}
	if got := cleanMsg("# Files mentioned by the user:\nattachment.png\n## My request for Codex:\n修复启动选择"); got != "修复启动选择" {
		t.Fatalf("cleanMsg attachment request = %q", got)
	}
	if got := meaningfulInputs([]string{"# AGENTS.md instructions\ninternal", "真实问题"}); len(got) != 1 || got[0] != "真实问题" {
		t.Fatalf("meaningfulInputs = %#v", got)
	}
	if got := formatTools(map[string]int{"Write": 1, "Read": 2}); got != "Read:2, Write:1" {
		t.Fatalf("formatTools = %q", got)
	}
}

func TestPrintNowShowsWaitingWakeCondition(t *testing.T) {
	view := nowView{
		GeneratedAt: "2026-07-18T12:00:00+08:00",
		Waiting: []store.Todo{{
			ID:            "t65",
			Title:         "Wait for dependency",
			Priority:      "P0",
			Status:        store.TodoStatusWaiting,
			WakeCondition: "waiting for todos: t79",
		}},
	}
	out := captureStdout(t, func() { printNow(view) })
	if !strings.Contains(out, "Waiting (1)") || !strings.Contains(out, "wake=waiting for todos: t79") {
		t.Fatalf("now waiting output = %q", out)
	}
	if strings.Contains(out, "waiting=1") {
		t.Fatalf("now still hides waiting details in summary: %q", out)
	}
}

func TestRunTodoAddPersistsTodo(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	oldJSON := jsonOutput
	oldPriority, oldProject := todoAddPriorityFlag, todoAddProjectFlag
	oldSource, oldDesc, oldDescFile := todoSourceFlag, todoDescFlag, todoDescFileFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoAddPriorityFlag = oldPriority
		todoAddProjectFlag = oldProject
		todoSourceFlag = oldSource
		todoDescFlag = oldDesc
		todoDescFileFlag = oldDescFile
		todoAddCmd.SetErr(os.Stderr)
	})

	jsonOutput = false
	todoAddPriorityFlag = "P0"
	todoAddProjectFlag = "atm"
	todoSourceFlag = "test-suite"
	todoDescFlag = "Make the command layer observable."
	todoDescFileFlag = ""
	var stderr bytes.Buffer
	todoAddCmd.SetErr(&stderr)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runTodoAdd(todoAddCmd, []string{"Add", "command", "tests"})
	})
	if runErr != nil {
		t.Fatalf("runTodoAdd: %v", runErr)
	}
	if out != "t1\n" {
		t.Fatalf("output = %q", out)
	}
	if stderr.String() != "Created t1: Add command tests\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}

	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("load todos: %v", err)
	}
	if len(tf.Items) != 1 {
		t.Fatalf("items = %#v", tf.Items)
	}
	got := tf.Items[0]
	if got.ID != "t1" || got.Title != "Add command tests" || got.Priority != "P0" ||
		got.Project != "atm" || got.Source != "test-suite" || got.Description != "Make the command layer observable." ||
		got.Status != "open" {
		t.Fatalf("todo = %#v", got)
	}
	if !store.TodoDocExists("t1") {
		t.Fatal("todo add should create the markdown card for agent handoff")
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(doc, "Make the command layer observable.") {
		t.Fatalf("todo doc = %q, err=%v", doc, err)
	}
}

func TestRunTodoAddReadsDescriptionFromFileOrStdin(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	oldJSON := jsonOutput
	oldPriority, oldProject := todoAddPriorityFlag, todoAddProjectFlag
	oldSource, oldDesc, oldDescFile := todoSourceFlag, todoDescFlag, todoDescFileFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoAddPriorityFlag, todoAddProjectFlag = oldPriority, oldProject
		todoSourceFlag, todoDescFlag, todoDescFileFlag = oldSource, oldDesc, oldDescFile
		todoAddCmd.SetIn(os.Stdin)
		todoAddCmd.SetErr(os.Stderr)
	})

	jsonOutput = false
	todoAddPriorityFlag = "P1"
	todoAddProjectFlag = "atm"
	todoSourceFlag = "test-suite"
	todoDescFlag = ""
	todoAddCmd.SetErr(io.Discard)

	todoDescFileFlag = "-"
	todoAddCmd.SetIn(strings.NewReader("first line\nsecond line\n"))
	var runErr error
	captureStdout(t, func() {
		runErr = runTodoAdd(todoAddCmd, []string{"Description", "from", "stdin"})
	})
	if runErr != nil {
		t.Fatalf("add from stdin: %v", runErr)
	}

	descPath := filepath.Join(t.TempDir(), "description.md")
	if err := os.WriteFile(descPath, []byte("description from file\n"), 0644); err != nil {
		t.Fatalf("write description: %v", err)
	}
	todoDescFileFlag = descPath
	captureStdout(t, func() {
		runErr = runTodoAdd(todoAddCmd, []string{"Description", "from", "file"})
	})
	if runErr != nil {
		t.Fatalf("add from file: %v", runErr)
	}

	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(tf.Items) != 2 || tf.Items[0].Description != "first line\nsecond line\n" || tf.Items[1].Description != "description from file\n" {
		t.Fatalf("descriptions = %#v", tf.Items)
	}
}

// bodyWithShellMetacharacters is the text a technical requirement actually
// contains. Every one of these characters is one a shell would act on, so this
// doubles as the fixture proving the file path writes bytes rather than whatever
// the shell made of them.
const bodyWithShellMetacharacters = "## 问题\n\n" +
	"`lookupPricing` 未命中时返回 `defaultPricing = {15.0, 75.0}`，即 $15/$75。\n" +
	"环境变量 $HOME 与 ${PATH} 不该被展开，$(date) 与 `date` 不该被执行。\n" +
	"引号：\"double\" 'single' —— 反斜杠 \\ 与感叹号 ! 也要原样保留。\n"

func TestRunTodoEditReadsDescriptionFromFileOrStdin(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{ID: "t1", Title: "Edit me", Priority: "P1", Status: "open", Created: store.Today()}); err != nil {
		t.Fatal(err)
	}

	oldDesc, oldDescFile := todoEditDescFlag, todoEditDescFileFlag
	t.Cleanup(func() {
		todoEditDescFlag, todoEditDescFileFlag = oldDesc, oldDescFile
		todoEditCmd.SetIn(os.Stdin)
		todoEditCmd.SetErr(os.Stderr)
		todoEditCmd.Flags().Set("desc-file", "")
		todoEditCmd.Flags().Lookup("desc-file").Changed = false
	})
	todoEditCmd.SetErr(io.Discard)
	todoEditDescFlag = ""

	descPath := filepath.Join(t.TempDir(), "description.md")
	if err := os.WriteFile(descPath, []byte(bodyWithShellMetacharacters), 0644); err != nil {
		t.Fatal(err)
	}
	todoEditDescFileFlag = descPath
	todoEditCmd.Flags().Lookup("desc-file").Changed = true
	captureStdout(t, func() {
		if err := runTodoEdit(todoEditCmd, []string{"t1"}); err != nil {
			t.Fatalf("edit from file: %v", err)
		}
	})
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if got := tf.Items[0].Description; got != bodyWithShellMetacharacters {
		t.Fatalf("description from file is not byte-identical:\n got %q\nwant %q", got, bodyWithShellMetacharacters)
	}

	// stdin takes the same path, so `printf ... | atm todo edit t1 --desc-file -`
	// behaves like `todo add --desc-file -`.
	todoEditDescFileFlag = "-"
	todoEditCmd.SetIn(strings.NewReader("from stdin\n"))
	captureStdout(t, func() {
		if err := runTodoEdit(todoEditCmd, []string{"t1"}); err != nil {
			t.Fatalf("edit from stdin: %v", err)
		}
	})
	tf, err = store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if got := tf.Items[0].Description; got != "from stdin\n" {
		t.Fatalf("description from stdin = %q", got)
	}
}

// Giving both is a caller who does not know which one wins, so it has to fail
// rather than have ATM silently pick one.
func TestReadBodyFlagOrFileRejectsBothSources(t *testing.T) {
	_, err := readBodyFlagOrFile(todoEditCmd, "desc", "inline text", "/tmp/whatever.md")
	if err == nil {
		t.Fatal("expected an error when both --desc and --desc-file are given")
	}
	if !strings.Contains(err.Error(), "--desc and --desc-file cannot be used together") {
		t.Fatalf("error = %v", err)
	}
}

// The 分析 section is the longest prose ATM accepts and the likeliest to carry
// code, so it needs the same door as the description.
func TestRunTodoLogReadsMessageFromFile(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{ID: "t1", Title: "Log me", Priority: "P1", Status: "in_progress", Created: store.Today()}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}

	oldSection, oldFile := todoLogSectionFlag, todoLogMessageFileFlag
	t.Cleanup(func() { todoLogSectionFlag, todoLogMessageFileFlag = oldSection, oldFile })

	analysis := "候选方案：\n\n```go\nmarker := \"## \" + section\n```\n\n费用 $15/$75，变量 $HOME 不展开。"
	path := filepath.Join(t.TempDir(), "analysis.md")
	if err := os.WriteFile(path, []byte(analysis), 0644); err != nil {
		t.Fatal(err)
	}
	todoLogSectionFlag = "分析"
	todoLogMessageFileFlag = path
	captureStdout(t, func() {
		if err := runTodoLog(todoLogCmd, []string{"t1"}); err != nil {
			t.Fatalf("log from file: %v", err)
		}
	})

	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "费用 $15/$75，变量 $HOME 不展开。") || !strings.Contains(doc, `marker := "## " + section`) {
		t.Fatalf("analysis body was not written verbatim:\n%s", doc)
	}
}

func TestRunTodoDeleteConfirmationAndYesFlag(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{ID: "t1", Title: "Keep until confirmed", Priority: "P1", Status: "open", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Delete interactively", Priority: "P1", Status: "open", Created: store.Today()}); err != nil {
		t.Fatal(err)
	}

	oldYes, oldProject := todoDeleteYesFlag, todoDeleteProjectFlag
	t.Cleanup(func() {
		todoDeleteYesFlag, todoDeleteProjectFlag = oldYes, oldProject
		todoDeleteCmd.SetIn(os.Stdin)
		todoDeleteCmd.SetErr(os.Stderr)
	})
	var stderr bytes.Buffer
	todoDeleteCmd.SetErr(&stderr)
	todoDeleteProjectFlag = ""

	todoDeleteYesFlag = false
	todoDeleteCmd.SetIn(strings.NewReader("n\n"))
	if err := runTodoDelete(todoDeleteCmd, []string{"t1"}); err != nil {
		t.Fatalf("decline delete: %v", err)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(tf, "t1") == nil {
		t.Fatalf("declined todo should remain: %#v, err=%v", tf, err)
	}

	// A GUI or script that forgot --yes has no stdin to answer with. That must
	// fail loudly instead of reporting a cancel the caller reads as success.
	todoDeleteCmd.SetIn(strings.NewReader(""))
	if err := runTodoDelete(todoDeleteCmd, []string{"t1"}); err == nil {
		t.Fatal("delete on non-interactive stdin should error, not silently cancel")
	}
	tf, err = store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(tf, "t1") == nil {
		t.Fatalf("todo should survive a failed confirmation: %#v, err=%v", tf, err)
	}

	todoDeleteYesFlag = true
	if err := runTodoDelete(todoDeleteCmd, []string{"t1"}); err != nil {
		t.Fatalf("delete with --yes: %v", err)
	}

	todoDeleteYesFlag = false
	todoDeleteCmd.SetIn(strings.NewReader("yes\n"))
	if err := runTodoDelete(todoDeleteCmd, []string{"t2"}); err != nil {
		t.Fatalf("confirmed delete: %v", err)
	}
	tf, err = store.LoadTodosReadOnly()
	if err != nil || len(tf.Items) != 0 {
		t.Fatalf("todos after delete = %#v, err=%v", tf, err)
	}
	if flag := todoDeleteCmd.Flags().Lookup("yes"); flag == nil || flag.Shorthand != "y" {
		t.Fatalf("--yes shorthand = %#v", flag)
	}

	if err := seedTodos(store.Todo{ID: "t3", Title: "Project delete one", Priority: "P1", Status: "open", Project: "atm", Created: store.Today()},
		store.Todo{ID: "t4", Title: "Project delete two", Priority: "P1", Status: "open", Project: "atm", Created: store.Today()},
		store.Todo{ID: "t5", Title: "Other project", Priority: "P1", Status: "open", Project: "other", Created: store.Today()}); err != nil {
		t.Fatal(err)
	}
	todoDeleteYesFlag = true
	todoDeleteProjectFlag = "atm"
	out := captureStdout(t, func() {
		err = runTodoDelete(todoDeleteCmd, nil)
	})
	if err != nil || out != "Deleted 2 todos from project atm\n" {
		t.Fatalf("project delete output = %q, err=%v", out, err)
	}
	tf, err = store.LoadTodosReadOnly()
	if err != nil || len(tf.Items) != 1 || tf.Items[0].ID != "t5" {
		t.Fatalf("todos after project delete = %#v, err=%v", tf, err)
	}
}

func TestRunTodoTrashRestoreAndPermanentDelete(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Recover me", Priority: "P1", Status: "open", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	trashOut := captureStdout(t, func() {
		if err := runTodoTrash(todoTrashCmd, []string{"t1"}); err != nil {
			t.Fatalf("trash: %v", err)
		}
	})
	if trashOut != "Trashed t1\n" {
		t.Fatalf("trash output = %q", trashOut)
	}
	archived, err := store.LoadArchivedTodos()
	if err != nil || len(archived) != 1 || archived[0].Status != "open" {
		t.Fatalf("trash contents = %#v, err=%v", archived, err)
	}

	restoreOut := captureStdout(t, func() {
		if err := runTodoRestore(todoRestoreCmd, []string{"t1"}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	if restoreOut != "Restored t1\n" {
		t.Fatalf("restore output = %q", restoreOut)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(todos, "t1") == nil {
		t.Fatalf("restored working set = %#v, err=%v", todos, err)
	}

	if err := runTodoTrash(todoTrashCmd, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	oldYes := todoDeleteYesFlag
	t.Cleanup(func() { todoDeleteYesFlag = oldYes })
	todoDeleteYesFlag = true
	if err := runTodoDelete(todoDeleteCmd, []string{"t1"}); err != nil {
		t.Fatalf("delete from trash: %v", err)
	}
	archived, err = store.LoadArchivedTodos()
	if err != nil || len(archived) != 0 {
		t.Fatalf("trash after permanent delete = %#v, err=%v", archived, err)
	}
}

func TestTodoHelpIncludesBatchAndDependencyExamples(t *testing.T) {
	if !strings.Contains(todoAddCmd.Example, "atm todo add --batch") || !strings.Contains(todoAddCmd.Example, "--desc-file -") {
		t.Fatalf("todo add examples = %q", todoAddCmd.Example)
	}
	if !strings.Contains(todoDependAddCmd.Example, "t77 t76") || !strings.Contains(todoDependAddCmd.Example, "t77 waits") {
		t.Fatalf("todo depend add example = %q", todoDependAddCmd.Example)
	}
}

func TestTodoShowListsExplicitlyBoundSessions(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	createdTS := seedCommandSession(t)
	doneTS := createdTS + 90
	todo := store.Todo{
		ID:          "t1",
		Title:       "Ship command tests",
		Description: "The same todo shape must be available at the top level.",
		Priority:    "P1",
		Status:      "done",
		Project:     "atm",
		Created:     store.Today(),
		StartTS:     &createdTS,
		DoneTS:      &doneTS,
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "cmd-session-full", TodoID: "t1", Agent: "codex", Project: "atm", CWD: "/tmp/atm",
	}); err != nil {
		t.Fatalf("bind session: %v", err)
	}

	jsonOutput = true
	var runErr error
	out := captureStdout(t, func() {
		runErr = runTodoShow(todoShowCmd, []string{"t1"})
	})
	if runErr != nil {
		t.Fatalf("runTodoShow: %v", runErr)
	}

	var got struct {
		ID          string                   `json:"id"`
		Description string                   `json:"description"`
		Todo        store.Todo               `json:"todo"`
		Sessions    []store.TodoBoundSession `json:"sessions"`
		Summary     struct {
			Sessions  int     `json:"sessions"`
			Queries   int     `json:"queries"`
			ToolCalls int     `json:"tool_calls"`
			CostUSD   float64 `json:"cost_usd"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal todo show: %v\n%s", err, out)
	}
	if got.ID != got.Todo.ID || got.Description != got.Todo.Description ||
		got.Todo.ID != "t1" || len(got.Sessions) != 1 || got.Sessions[0].SessionID != "cmd-session-full" ||
		got.Sessions[0].ShortID != "cmdsess" || !got.Sessions[0].Indexed || got.Sessions[0].BindingCount != 1 ||
		got.Sessions[0].Queries != 1 || got.Sessions[0].ToolCalls != 2 {
		t.Fatalf("todo show = %#v", got)
	}
	if got.Summary.Sessions != 1 || got.Summary.Queries != 1 || got.Summary.ToolCalls != 2 || got.Summary.CostUSD != 0.006 {
		t.Fatalf("summary = %#v", got.Summary)
	}
}

// A bound session that reads as a bare id tells the human nothing, and codex —
// the agent most of these bindings come from — writes no title into the
// transcript it indexes. So `todo show` names each session itself: from codex's
// own thread index, then from the first thing the human actually typed.
func TestTodoShowNamesBoundSessionsWithoutStoredSummary(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)

	threadID := "019fc6cb-c6ce-71e2-9b65-8f0af41c825b"
	indexPath := filepath.Join(filepath.Dir(config.CodexSessions), "session_index.jsonl")
	if err := os.WriteFile(indexPath,
		[]byte(`{"id":"`+threadID+`","thread_name":"优化 UI 设计"}`+"\n"), 0644); err != nil {
		t.Fatalf("write codex thread index: %v", err)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	titled := "rollout-2026-08-03T16-44-31-" + threadID
	if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
		VALUES (?, '8f0af41c', 'codex', 'atm', '/tmp/titled.jsonl', '', 100, '', 400),
		       ('untitled-session', 'untitle', 'codex', 'atm', '/tmp/untitled.jsonl', '', 100, '', 400)`, titled); err != nil {
		db.Close()
		t.Fatalf("seed sessions: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages (session_id, seq, role, content, ts) VALUES
		('untitled-session', 0, 'user', '<recommended_plugins> Atlassian Rovo', 110),
		('untitled-session', 1, 'user', '# AGENTS.md instructions', 120),
		('untitled-session', 2, 'user', '任务详情看不到 session 信息' || char(10) || '第二行不该出现在标题里', 130)`); err != nil {
		db.Close()
		t.Fatalf("seed messages: %v", err)
	}
	db.Close()

	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Name bound sessions", Priority: "P1",
		Status: "in_progress", Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	for _, binding := range []store.TodoSessionBinding{
		{SessionID: threadID, TodoID: "t1", Agent: "codex", Project: "atm", CWD: "/tmp/atm"},
		{SessionID: "untitled-session", TodoID: "t1", Agent: "codex", Project: "atm", CWD: "/tmp/atm"},
	} {
		if _, err := store.BindTodoSession(binding); err != nil {
			t.Fatalf("bind session: %v", err)
		}
	}

	jsonOutput = true
	var runErr error
	out := captureStdout(t, func() {
		runErr = runTodoShow(todoShowCmd, []string{"t1"})
	})
	if runErr != nil {
		t.Fatalf("runTodoShow: %v", runErr)
	}

	var got struct {
		Sessions []store.TodoBoundSession `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal todo show: %v\n%s", err, out)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %#v", got.Sessions)
	}
	byID := map[string]store.TodoBoundSession{}
	for _, session := range got.Sessions {
		byID[session.SessionID] = session
	}
	if byID[threadID].Summary != "优化 UI 设计" {
		t.Fatalf("codex thread title = %#v", byID[threadID])
	}
	if byID[threadID].IndexedID != titled {
		t.Fatalf("indexed id = %#v", byID[threadID])
	}
	if byID["untitled-session"].Summary != "任务详情看不到 session 信息" {
		t.Fatalf("prompt fallback = %#v", byID["untitled-session"])
	}
}

func TestTodoWorkStateCommandsAndNow(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	oldWaitWake, oldWaitReview := todoWaitWakeFlag, todoWaitReviewAtFlag
	oldMaintainLimit := todoMaintainLimitFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoWaitWakeFlag, todoWaitReviewAtFlag = oldWaitWake, oldWaitReview
		todoMaintainLimitFlag = oldMaintainLimit
	})

	todos := []store.Todo{
		{ID: "t1", Title: "New work", Priority: "P0", Status: store.TodoStatusOpen, Created: store.Today()},
		{ID: "t2", Title: "Waiting work", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
		{ID: "t3", Title: "Personal maintenance", Priority: "P2", Status: store.TodoStatusOpen, Created: store.Today()},
	}
	if err := seedTodos(todos...); err != nil {
		t.Fatalf("save todos: %v", err)
	}

	jsonOutput = true
	var commandErr error
	captureStdout(t, func() { commandErr = runTodoFocus(todoFocusCmd, []string{"t1"}) })
	if commandErr != nil {
		t.Fatalf("focus: %v", commandErr)
	}

	todoWaitWakeFlag = "new business input"
	todoWaitReviewAtFlag = time.Now().In(config.Loc).AddDate(0, 0, 2).Format("2006-01-02")
	captureStdout(t, func() { commandErr = runTodoWait(todoWaitCmd, []string{"t2"}) })
	if commandErr != nil {
		t.Fatalf("wait: %v", commandErr)
	}

	todoMaintainLimitFlag = 2
	captureStdout(t, func() { commandErr = runTodoMaintain(todoMaintainCmd, []string{"t3"}) })
	if commandErr != nil {
		t.Fatalf("maintain: %v", commandErr)
	}

	var runErr error
	nowOut := captureStdout(t, func() {
		runErr = runNow(nowCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("now: %v", runErr)
	}
	var view nowView
	if err := json.Unmarshal([]byte(nowOut), &view); err != nil {
		t.Fatalf("unmarshal now: %v\n%s", err, nowOut)
	}
	if len(view.Working) != 1 || view.Working[0].ID != "t1" {
		t.Fatalf("working = %#v", view.Working)
	}
	if len(view.Waiting) != 1 || view.Waiting[0].ID != "t2" || view.Waiting[0].WakeCondition != "new business input" {
		t.Fatalf("waiting = %#v", view.Waiting)
	}
	if view.Summary.Maintenance != 1 {
		t.Fatalf("maintenance count = %d", view.Summary.Maintenance)
	}

	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("load todos: %v", err)
	}
	maintenance := store.FindTodo(tf, "t3")
	if maintenance == nil || !store.TodoHasTag(*maintenance, store.TodoTagMaintenance) || maintenance.MaintenanceLimit != 2 {
		t.Fatalf("maintenance = %#v", maintenance)
	}
}

func TestReadCommandsRequireExplicitSyncWhenDatabaseIsMissing(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	jsonOutput = true

	err := runList(listCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "run `atm sync` first") {
		t.Fatalf("read without database error = %v", err)
	}
	if _, statErr := os.Stat(config.AtmDB); !os.IsNotExist(statErr) {
		t.Fatalf("read command created database: %v", statErr)
	}

	syncBeforeRead = true
	var runErr error
	out := captureStdout(t, func() {
		runErr = runList(listCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("read with explicit sync: %v", runErr)
	}
	var sessions []any
	if err := json.Unmarshal([]byte(out), &sessions); err != nil {
		t.Fatalf("json output = %q: %v", out, err)
	}
	if _, statErr := os.Stat(config.AtmDB); statErr != nil {
		t.Fatalf("explicit sync did not create database: %v", statErr)
	}
}

func TestSyncStatusReportsMissingIndexWithoutCreatingIt(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	jsonOutput = true

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSyncStatus(syncStatusCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("sync status: %v", runErr)
	}
	var report syncStatusReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal sync status: %v\n%s", err, out)
	}
	if report.Index.Exists || report.Sync.Status != "missing" || report.Sync.RunStatus != "never" {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("sync status created database: %v", err)
	}
}

func TestSyncStatusReportsSuccessfulExplicitSync(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)

	if err := runSync(syncCmd, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	jsonOutput = true
	var runErr error
	out := captureStdout(t, func() {
		runErr = runSyncStatus(syncStatusCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("sync status: %v", runErr)
	}
	var report syncStatusReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal sync status: %v\n%s", err, out)
	}
	if !report.Index.Exists || report.Index.SchemaVersion != store.SchemaVersion || report.Sync.Status != "fresh" ||
		report.Sync.RunStatus != "succeeded" || report.Sync.LastSuccessAt == nil {
		t.Fatalf("report = %#v", report)
	}

	agentFlag = "codex"
	out = captureStdout(t, func() {
		runErr = runSyncStatus(syncStatusCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("agent sync status: %v", runErr)
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal agent sync status: %v\n%s", err, out)
	}
	if report.Sync.Scope != "codex" || report.Sync.Status != "fresh" {
		t.Fatalf("agent report = %#v", report)
	}
}

func TestTodoLinkCommandsAreSafeAndIdempotent(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	oldKind, oldTitle, oldRelation := todoLinkKindFlag, todoLinkTitleFlag, todoLinkRelationFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoLinkKindFlag, todoLinkTitleFlag, todoLinkRelationFlag = oldKind, oldTitle, oldRelation
	})
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Track a change request", Priority: "P1", Status: "open", Created: store.Today(),
	}); err != nil {
		t.Fatalf("save todos: %v", err)
	}

	jsonOutput = true
	todoLinkKindFlag = "cr"
	todoLinkTitleFlag = "Release CR"
	todoLinkRelationFlag = "tracks"
	var runErr error
	firstOut := captureStdout(t, func() {
		runErr = runTodoLinkAdd(todoLinkAddCmd, []string{"t1", "HTTPS://Example.COM/cr/42?b=2&a=1"})
	})
	if runErr != nil {
		t.Fatalf("link add: %v", runErr)
	}
	var first struct {
		Created bool           `json:"created"`
		Link    store.TodoLink `json:"link"`
	}
	if err := json.Unmarshal([]byte(firstOut), &first); err != nil {
		t.Fatalf("unmarshal first add: %v\n%s", err, firstOut)
	}
	if !first.Created || first.Link.URL != "https://example.com/cr/42?a=1&b=2" || first.Link.Kind != "cr" {
		t.Fatalf("first link = %#v", first)
	}

	todoLinkTitleFlag = "Updated CR title"
	secondOut := captureStdout(t, func() {
		runErr = runTodoLinkAdd(todoLinkAddCmd, []string{"t1", "https://example.com/cr/42?a=1&b=2"})
	})
	if runErr != nil {
		t.Fatalf("link update: %v", runErr)
	}
	var second struct {
		Created bool           `json:"created"`
		Link    store.TodoLink `json:"link"`
	}
	if err := json.Unmarshal([]byte(secondOut), &second); err != nil {
		t.Fatalf("unmarshal second add: %v\n%s", err, secondOut)
	}
	if second.Created || second.Link.Title != "Updated CR title" {
		t.Fatalf("updated link = %#v", second)
	}

	listOut := captureStdout(t, func() {
		runErr = runTodoLinkList(todoLinkListCmd, []string{"t1"})
	})
	if runErr != nil {
		t.Fatalf("link list: %v", runErr)
	}
	var links []store.TodoLink
	if err := json.Unmarshal([]byte(listOut), &links); err != nil {
		t.Fatalf("unmarshal links: %v\n%s", err, listOut)
	}
	if len(links) != 1 || links[0].Relation != "tracks" {
		t.Fatalf("links = %#v", links)
	}

	removeOut := captureStdout(t, func() {
		runErr = runTodoLinkRemove(todoLinkRemoveCmd, []string{"t1", links[0].URL})
	})
	if runErr != nil || !strings.Contains(removeOut, `"removed"`) {
		t.Fatalf("remove output = %q, err = %v", removeOut, runErr)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("load todos: %v", err)
	}
	if len(tf.Items[0].Links) != 0 {
		t.Fatalf("todo after remove = %#v", tf.Items[0])
	}
}

func TestTodoLinkRejectsCredentials(t *testing.T) {
	for _, raw := range []string{
		"example.com/cr/1",
		"ftp://example.com/cr/1",
		"https://user:password@example.com/cr/1",
		"https://example.com/cr/1?access_token=secret",
		"https://example.com/cr/1#access_token=secret",
	} {
		if _, err := normalizeTodoLinkURL(raw); err == nil {
			t.Fatalf("normalizeTodoLinkURL(%q) expected error", raw)
		}
	}
}

func TestKnowledgeMemoryAndArtifactCommands(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	oldKnowledgeLimit := knowledgeLimit
	oldAddFile, oldAddProducer := knowledgeAddFile, knowledgeAddProducer
	oldUpdateFile := knowledgeUpdateFile
	oldAddCollection := knowledgeAddCollection
	oldRecallScope, oldWriteScope, oldMemoryLimit := memoryRecallScope, memoryWriteScope, memoryLimit
	oldMemorySource := memorySource
	oldArtifactFile, oldProducer, oldRunID := artifactFile, artifactProducer, artifactRunID
	t.Cleanup(func() {
		jsonOutput = oldJSON
		knowledgeLimit = oldKnowledgeLimit
		knowledgeAddFile, knowledgeAddProducer = oldAddFile, oldAddProducer
		knowledgeUpdateFile = oldUpdateFile
		knowledgeAddCollection = oldAddCollection
		memoryRecallScope, memoryWriteScope, memoryLimit = oldRecallScope, oldWriteScope, oldMemoryLimit
		memorySource = oldMemorySource
		artifactFile, artifactProducer, artifactRunID = oldArtifactFile, oldProducer, oldRunID
	})

	jsonOutput = true
	knowledgeLimit = 5
	knowledgeAddFile, knowledgeAddProducer = "", "test"
	knowledgeAddCollection = "inbox"

	var runErr error
	addOut := captureStdout(t, func() {
		runErr = knowledgeAddCmd.RunE(knowledgeAddCmd, []string{"Source", "ATM knowledge command marker."})
	})
	if runErr != nil || !strings.Contains(addOut, "document:") {
		t.Fatalf("knowledge add output = %q, err = %v", addOut, runErr)
	}
	var addedDocument struct {
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(addOut), &addedDocument); err != nil || addedDocument.Metadata.ID == "" {
		t.Fatalf("decode knowledge add output = %q, err = %v", addOut, err)
	}
	updateOut := captureStdout(t, func() {
		runErr = knowledgeUpdateCmd.RunE(knowledgeUpdateCmd, []string{addedDocument.Metadata.ID, "Updated knowledge command marker."})
	})
	if runErr != nil || !strings.Contains(updateOut, "Updated knowledge command marker.") {
		t.Fatalf("knowledge update output = %q, err = %v", updateOut, runErr)
	}
	searchOut := captureStdout(t, func() {
		runErr = knowledgeSearchCmd.RunE(knowledgeSearchCmd, []string{"Updated knowledge"})
	})
	if runErr != nil || !strings.Contains(searchOut, "Updated knowledge command marker") {
		t.Fatalf("knowledge search output = %q, err = %v", searchOut, runErr)
	}
	catalogOut := captureStdout(t, func() {
		runErr = knowledgeCatalogCmd.RunE(knowledgeCatalogCmd, nil)
	})
	if runErr != nil || !strings.Contains(catalogOut, `"id": "inbox"`) {
		t.Fatalf("knowledge catalog output = %q, err = %v", catalogOut, runErr)
	}
	deleteOut := captureStdout(t, func() {
		runErr = knowledgeDeleteCmd.RunE(knowledgeDeleteCmd, []string{addedDocument.Metadata.ID})
	})
	if runErr != nil || !strings.Contains(deleteOut, addedDocument.Metadata.ID) {
		t.Fatalf("knowledge delete output = %q, err = %v", deleteOut, runErr)
	}
	if _, err := knowledge.Get(config.AtmDir, addedDocument.Metadata.ID); err == nil {
		t.Fatal("knowledge delete left the document readable")
	}

	memoryWriteScope = "project:mox"
	memorySource = "session:test#turn:1"
	rememberOut := captureStdout(t, func() {
		runErr = memoryRememberCmd.RunE(memoryRememberCmd, []string{"remember command marker"})
	})
	if runErr != nil || !strings.Contains(rememberOut, "memory:") {
		t.Fatalf("memory remember output = %q, err = %v", rememberOut, runErr)
	}
	if !strings.Contains(rememberOut, `"source": "session:test#turn:1"`) {
		t.Fatalf("memory remember provenance missing: %q", rememberOut)
	}
	memoryRecallScope = "project:mox"
	memoryLimit = 5
	recallOut := captureStdout(t, func() {
		runErr = memoryRecallCmd.RunE(memoryRecallCmd, []string{"command marker"})
	})
	if runErr != nil || !strings.Contains(recallOut, "remember command marker") {
		t.Fatalf("memory recall output = %q, err = %v", recallOut, runErr)
	}

	artifactProducer, artifactRunID, artifactFile = "test", "run-test", ""
	artifactSourceRaw = nil
	artifactOut := captureStdout(t, func() {
		runErr = artifactSaveCmd.RunE(artifactSaveCmd, []string{"Command report", "# Report\n\nDone."})
	})
	if runErr != nil {
		t.Fatalf("artifact save: %v", runErr)
	}
	var artifact struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(artifactOut), &artifact); err != nil {
		t.Fatalf("artifact output = %q: %v", artifactOut, err)
	}
	if _, err := os.Stat(artifact.Path); err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
}

func TestSkipLocalNotification(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", value)
			if !skipLocalNotification() {
				t.Fatalf("skipLocalNotification() = false for %q", value)
			}
		})
	}

	t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "0")
	if skipLocalNotification() {
		t.Fatal("skipLocalNotification() = true for 0")
	}
}

func TestNotifyCopyHumanFacingEvents(t *testing.T) {
	start := int64(1_000)
	done := int64(1_000 + 3_661)
	todo := &store.Todo{
		ID: "t42", Title: "Ship notify", Project: "atm",
		StartTS: &start, DoneTS: &done,
	}

	title, subtitle, body := notifyCopy(todo, notifyEventCreated)
	if title != "ATM · atm" || subtitle != "t42 新建" || body != "Ship notify" {
		t.Fatalf("created = %q / %q / %q", title, subtitle, body)
	}

	title, subtitle, body = notifyCopy(todo, notifyEventReview)
	if title != "ATM · atm" || subtitle != "t42 待验收" || body != "Ship notify" {
		t.Fatalf("review = %q / %q / %q", title, subtitle, body)
	}

	title, subtitle, body = notifyCopy(todo, notifyEventDone)
	if title != "ATM · atm" || subtitle != "t42 已完成" || body != "Ship notify (1h1m)" {
		t.Fatalf("done = %q / %q / %q", title, subtitle, body)
	}

	title, subtitle, body = notifyCopy(todo, notifyEventDropped)
	if title != "ATM · atm" || subtitle != "t42 已放弃" || body != "Ship notify" {
		t.Fatalf("dropped = %q / %q / %q", title, subtitle, body)
	}

	noProject := &store.Todo{ID: "t1", Title: "Solo"}
	title, _, _ = notifyCopy(noProject, notifyEventCreated)
	if title != "ATM" {
		t.Fatalf("no project title = %q", title)
	}
}

func TestTodoQueryIncludesDocumentAndRequiresAllTerms(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "ATM 持续优化", Priority: "P1", Status: store.TodoStatusInProgress,
		Project: "atm", Created: "2026-07-18",
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendTodoLog(&todo, "结果：修复 Skill 统计为空；下一步：验收排行", "进展"); err != nil {
		t.Fatal(err)
	}
	if !todoMatchesQuery(todo, "Skill 统计") {
		t.Fatal("todo document was not searched")
	}
	if todoMatchesQuery(todo, "Skill 不存在") {
		t.Fatal("todo query matched only one term")
	}
}

func TestTodoQueryRelevancePrefersTitleOverIncidentalDescription(t *testing.T) {
	titleMatch := store.Todo{ID: "t900", Title: "搜索相关性优化", Description: "调整排序"}
	incidental := store.Todo{ID: "t901", Title: "整理界面", Description: "最后检查搜索相关性优化是否受影响"}
	if todoQueryRelevance(titleMatch, "搜索相关性") <= todoQueryRelevance(incidental, "搜索相关性") {
		t.Fatalf("title score %d should beat description score %d",
			todoQueryRelevance(titleMatch, "搜索相关性"),
			todoQueryRelevance(incidental, "搜索相关性"))
	}
}

func TestFormatQuotaWindow(t *testing.T) {
	tests := map[int]string{
		300:   "5h",
		1440:  "1d",
		10080: "1w",
		90:    "90m",
	}
	for minutes, want := range tests {
		if got := formatQuotaWindow(minutes); got != want {
			t.Fatalf("formatQuotaWindow(%d) = %q, want %q", minutes, got, want)
		}
	}
}

// Forgetting only makes sense for retained history. While the source is still
// tracked the next sync would bring the session straight back, so the command
// refuses instead of pretending to delete it.
func TestSessionForgetOnlyDropsRetainedSessions(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	oldYes := sessionForgetYesFlag
	t.Cleanup(func() {
		sessionForgetYesFlag = oldYes
		forgetCmd.SetIn(os.Stdin)
		forgetCmd.SetErr(os.Stderr)
	})
	var stderr bytes.Buffer
	forgetCmd.SetErr(&stderr)
	sessionForgetYesFlag = true

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	var filePath string
	if err := db.QueryRow("SELECT file_path FROM sessions WHERE id = 'cmd-session-full'").Scan(&filePath); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_state (file_path, agent, mtime_unix, size_bytes, offset_bytes)
		VALUES (?, 'codex', 1, 2, 0)`, filePath); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	err = runSessionForget(forgetCmd, []string{"cmdsess"})
	if err == nil || !strings.Contains(err.Error(), "still backed by") {
		t.Fatalf("forget with the source still tracked = %v, want a refusal", err)
	}

	// The transcript is gone now: the sync bookkeeping went with it.
	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM sync_state WHERE file_path = ?", filePath); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		err = runSessionForget(forgetCmd, []string{"cmdsess"})
	})
	if err != nil {
		t.Fatalf("forget retained session: %v", err)
	}
	if !strings.Contains(out, "Forgot session cmdsess") {
		t.Fatalf("forget output = %q", out)
	}

	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sessions, messages, usageRows int
	if err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM sessions WHERE id = 'cmd-session-full'),
		(SELECT COUNT(*) FROM messages WHERE session_id = 'cmd-session-full'),
		(SELECT COUNT(*) FROM usage WHERE session_id = 'cmd-session-full')`).
		Scan(&sessions, &messages, &usageRows); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || messages != 0 || usageRows != 0 {
		t.Fatalf("after forget: %d sessions, %d messages, %d usage rows; want all gone",
			sessions, messages, usageRows)
	}

	if err := runSessionForget(forgetCmd, []string{"cmdsess"}); err == nil ||
		!strings.Contains(err.Error(), "session not found") {
		t.Fatalf("forget again = %v, want not found", err)
	}
}

// The indexed count includes sessions kept after their transcript was removed,
// so the line has to say how many, or a monotonically growing number reads as an
// agent that never cleans up.
func TestSyncStatusLabelsRetainedSessions(t *testing.T) {
	withCommandFlags(t)
	jsonOutput = false
	report := syncStatusReport{
		Index: syncStatusIndex{Path: "/tmp/atm.db", Exists: true, IndexedSessions: 660, RetainedSessions: 3},
		Sync:  syncStatusState{Status: "fresh"},
	}
	out := captureStdout(t, func() {
		if err := printSyncStatus(report); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "sessions: 660 (3 retained after their source was removed)") {
		t.Fatalf("sync status output = %q", out)
	}

	report.Index.RetainedSessions = 0
	out = captureStdout(t, func() {
		if err := printSyncStatus(report); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "sessions: 660\n") {
		t.Fatalf("sync status without retained sessions = %q", out)
	}
}
