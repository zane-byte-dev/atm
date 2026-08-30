package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
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

func TestSessionListDefaultsToLatestActivityAndBoundPage(t *testing.T) {
	if got := listCmd.Flags().Lookup("order").DefValue; got != "activity-desc" {
		t.Fatalf("list --order default = %q, want activity-desc", got)
	}
	if got := listCmd.Flags().Lookup("limit").DefValue; got != "200" {
		t.Fatalf("list --limit default = %q, want 200", got)
	}
}

func TestSessionSearchAcceptsQueryAliasAndReportsSchema(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	jsonOutput = true
	searchQueryFlag = "deployment"
	out := captureStdout(t, func() {
		if err := runSearch(searchCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		SchemaVersion int    `json:"schema_version"`
		Keyword       string `json:"keyword"`
		Returned      int    `json:"returned"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode search alias: %v\n%s", err, out)
	}
	if payload.SchemaVersion != sessionCLIOutputSchemaVersion || payload.Keyword != "deployment" || payload.Returned == 0 {
		t.Fatalf("search alias payload = %#v", payload)
	}
	if err := runSearch(searchCmd, []string{"second"}); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("positional plus --query error = %v", err)
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

func TestSessionTimelineAdapterPreservesJSONArrayAndTextRendering(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	jsonOutput = true
	var runErr error
	encoded := captureStdout(t, func() {
		runErr = runTimeline(timelineCmd, []string{"cmdsess"})
	})
	if runErr != nil {
		t.Fatalf("runTimeline JSON: %v", runErr)
	}
	var events []struct {
		Kind    string `json:"kind"`
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(encoded), &events); err != nil {
		t.Fatalf("decode timeline array: %v\n%s", err, encoded)
	}
	if len(events) != 2 || events[0].Kind != "message" || events[0].Role != "user" ||
		!strings.Contains(events[1].Content, "Deployment keyword answer") {
		t.Fatalf("timeline events = %#v", events)
	}

	jsonOutput = false
	text := captureStdout(t, func() {
		runErr = runTimeline(timelineCmd, []string{"cmdsess"})
	})
	if runErr != nil || !strings.Contains(text, "user") || !strings.Contains(text, "Find deployment keyword") {
		t.Fatalf("timeline text = %q, err = %v", text, runErr)
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

func TestSessionListEnvelopeKeepsPaginationMetadata(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	jsonOutput = true
	sessionListAllFlag = true
	sessionListLimit = 1
	sessionListEnvelope = true
	out := captureStdout(t, func() {
		if err := runList(listCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Total         int `json:"total"`
		Returned      int `json:"returned"`
		Sessions      []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode list envelope: %v\n%s", err, out)
	}
	if payload.SchemaVersion != sessionCLIOutputSchemaVersion || payload.Total != 1 ||
		payload.Returned != 1 || len(payload.Sessions) != 1 {
		t.Fatalf("list envelope = %#v", payload)
	}
}

func TestSessionExportFiltersPaginatesAndSupportsJSONL(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	exportDaysFlag = 2
	exportQueryFlag = "deployment"
	exportLimitFlag = 1
	exportOffsetFlag = 1
	exportEnvelopeFlag = true
	out := captureStdout(t, func() {
		if err := runExport(exportCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		SchemaVersion int               `json:"schema_version"`
		Total         int               `json:"total"`
		Returned      int               `json:"returned"`
		Messages      []store.ExportRow `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode export envelope: %v\n%s", err, out)
	}
	if payload.SchemaVersion != sessionCLIOutputSchemaVersion || payload.Total != 2 ||
		payload.Returned != 1 || len(payload.Messages) != 1 {
		t.Fatalf("export envelope = %#v", payload)
	}

	exportFormatFlag = "jsonl"
	exportEnvelopeFlag = false
	exportOffsetFlag = 0
	jsonl := captureStdout(t, func() {
		if err := runExport(exportCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != 1 {
		t.Fatalf("jsonl lines = %d, want paged 1\n%s", len(lines), jsonl)
	}
	var row store.ExportRow
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil || !strings.Contains(strings.ToLower(row.Content), "deployment") {
		t.Fatalf("jsonl row = %#v, err = %v", row, err)
	}
}
