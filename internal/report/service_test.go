package report

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func reportTestCall() application.Call {
	return application.Call{
		RequestID: "report-service-test",
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginCLI,
		},
	}
}

func withTempAtmDir(t *testing.T) {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
}

// fixedService answers from the given rows so these tests are about the day's
// selection and cleaning rules, not about SQLite. The connection is real because
// the service closes it.
func fixedService(t *testing.T, rows []store.ReportResult) (Service, *[]int64) {
	t.Helper()
	withTempAtmDir(t)
	window := &[]int64{}
	return NewService(ServiceOptions{
		Now:      func() time.Time { return time.Date(2026, 8, 21, 15, 0, 0, 0, config.Loc) },
		OpenRead: store.Open,
		Query: func(_ *sql.DB, start, end int64, _ string) ([]store.ReportResult, error) {
			*window = append(*window, start, end)
			return rows, nil
		},
	}), window
}

func daily(t *testing.T, service Service, input Input) Report {
	t.Helper()
	report, err := service.Daily(context.Background(), reportTestCall(), input, SyncInput{})
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	return report
}

// The digest exists to be recognizable, so a session that left nothing readable
// behind is not worth a line. Verbose is a transcript instead, so there a stored
// summary is enough to earn one.
func TestDailyDropsASessionWithNothingReadableUnlessVerboseHasASummary(t *testing.T) {
	rows := []store.ReportResult{{
		Agent: "claude", Project: "atm", ShortID: "s1",
		Summary: "重构 quota",
		// Every input is harness scaffolding, so no prompt survives cleaning.
		Inputs: []string{"<system-reminder>internal</system-reminder>"},
		Tools:  map[string]int{"Read": 3},
	}}
	service, _ := fixedService(t, rows)

	digest := daily(t, service, Input{})
	if len(digest.Agents) != 1 || len(digest.Agents[0].Sessions) != 1 {
		t.Fatalf("a session with a summary and tool calls was dropped from the digest: %+v", digest)
	}
	if got := digest.Agents[0].Sessions[0].Prompts; len(got) != 0 {
		t.Errorf("harness scaffolding became a prompt: %#v", got)
	}

	// The same session with no summary has nothing to name it by.
	rows[0].Summary = ""
	bare, _ := fixedService(t, rows)
	if report := daily(t, bare, Input{}); len(report.Agents) != 0 {
		t.Fatalf("a nameless, promptless session was listed in the digest: %+v", report.Agents)
	}
	if report := daily(t, bare, Input{Verbose: true}); len(report.Agents) != 1 {
		t.Fatalf("verbose dropped a session whose tool calls are the only record: %+v", report.Agents)
	}
}

// A session ATM never summarized is still worth naming by what it opened with.
func TestDailyNamesAnUnsummarizedSessionByItsFirstPrompt(t *testing.T) {
	service, _ := fixedService(t, []store.ReportResult{{
		Agent: "codex", Project: "atm", ShortID: "s2",
		Inputs: []string{"# AGENTS.md instructions\ninternal", "把 doctor 拆出去"},
	}})
	report := daily(t, service, Input{})
	session := report.Agents[0].Sessions[0]
	if session.Summary != "把 doctor 拆出去" {
		t.Errorf("summary = %q, want the first readable prompt", session.Summary)
	}
	if len(session.Prompts) != 1 {
		t.Errorf("prompts = %#v; the instructions block is not something a person typed", session.Prompts)
	}
}

// Sections follow ATM's fixed order so a report read across days keeps its shape,
// and an agent with nothing to show gets no heading at all.
func TestDailyOrdersSectionsAndOmitsSilentAgents(t *testing.T) {
	service, _ := fixedService(t, []store.ReportResult{
		{Agent: "claude", ShortID: "c1", Inputs: []string{"claude 的问题"}},
		{Agent: "pi", ShortID: "p1", Inputs: []string{"pi 的问题"}},
		{Agent: "codex", ShortID: "x1", Inputs: []string{"codex 的问题"}},
	})
	report := daily(t, service, Input{})
	var agents []string
	for _, section := range report.Agents {
		agents = append(agents, section.Agent)
	}
	if len(agents) != 3 || agents[0] != "pi" || agents[1] != "codex" || agents[2] != "claude" {
		t.Fatalf("sections = %v, want pi, codex, claude", agents)
	}
	if report.Agents[2].DisplayName != "Claude Code" {
		t.Errorf("display name = %q, want Claude Code", report.Agents[2].DisplayName)
	}
}

// Exchanges pair positionally because that is the only relationship the index
// records. A prompt whose reply is missing is reported alone rather than dropped:
// what a person asked is worth showing even when the answer was not captured.
func TestDailyVerbosePairsPromptsPositionally(t *testing.T) {
	service, _ := fixedService(t, []store.ReportResult{{
		Agent: "claude", ShortID: "s3",
		Inputs:  []string{"第一个问题", "<system-reminder>noise</system-reminder>", "第二个问题"},
		Outputs: []string{"第一个回答"},
	}})
	report := daily(t, service, Input{Verbose: true})
	exchanges := report.Agents[0].Sessions[0].Exchanges
	if len(exchanges) != 2 {
		t.Fatalf("exchanges = %#v, want the two readable prompts", exchanges)
	}
	if exchanges[0].Question != "第一个问题" || exchanges[0].Answer != "第一个回答" {
		t.Errorf("first exchange = %+v", exchanges[0])
	}
	if exchanges[1].Question != "第二个问题" || exchanges[1].Answer != "" {
		t.Errorf("an unanswered prompt was dropped or invented an answer: %+v", exchanges[1])
	}
}

// The non-verbose digest reports counts, and the tool count is the sum rather than
// the number of distinct tools.
func TestDailySumsToolCalls(t *testing.T) {
	service, _ := fixedService(t, []store.ReportResult{{
		Agent: "claude", ShortID: "s4",
		Inputs: []string{"问题"},
		Tools:  map[string]int{"Read": 3, "Write": 2},
	}})
	if got := daily(t, service, Input{}).Agents[0].Sessions[0].ToolCalls; got != 5 {
		t.Errorf("tool calls = %d, want 5", got)
	}
}

// A caller that asked for "yesterday" gets the resolved date back, so the header
// says which day was actually read.
func TestDailyResolvesRelativeDatesAndReportsTheOne(t *testing.T) {
	service, window := fixedService(t, nil)

	today := daily(t, service, Input{})
	if today.Date != "2026-08-21" {
		t.Errorf("default date = %q, want the injected clock's day", today.Date)
	}
	yesterday := daily(t, service, Input{Date: "yesterday"})
	if yesterday.Date != "2026-08-20" {
		t.Errorf("yesterday = %q", yesterday.Date)
	}
	explicit := daily(t, service, Input{Date: "2026-01-02"})
	if explicit.Date != "2026-01-02" {
		t.Errorf("explicit date = %q", explicit.Date)
	}
	// Each read is one day wide.
	for index := 0; index+1 < len(*window); index += 2 {
		if span := (*window)[index+1] - (*window)[index]; span != 86400 {
			t.Errorf("window %d is %d seconds wide, want one day", index/2, span)
		}
	}
	if !today.Empty() {
		t.Error("a day with no rows is not reported as empty")
	}
}

func TestDailyRejectsAnUnusableDateAndAnUnknownAgent(t *testing.T) {
	service, _ := fixedService(t, nil)

	_, err := service.Daily(context.Background(), reportTestCall(), Input{Date: "last tuesday"}, SyncInput{})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeInvalidArgument {
		t.Fatalf("date err = %v, want invalid_argument", err)
	}
	_, err = service.Daily(context.Background(), reportTestCall(), Input{Agent: "nope"}, SyncInput{})
	if !errors.As(err, &appErr) || appErr.Code != application.CodeInvalidArgument {
		t.Fatalf("agent err = %v, want invalid_argument", err)
	}
}
