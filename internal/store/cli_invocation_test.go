package store

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestCLIInvocationQueryKeepsSuccessDenominatorAndFilters(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows := []CLIInvocation{
		{OccurredAt: 100, SessionID: "session-a", Agent: "codex", Version: "1.2.3", CommandPath: "atm todo list", Success: true, DurationMS: 12},
		{OccurredAt: 110, SessionID: "session-a", Agent: "codex", Version: "1.2.3", CommandPath: "atm todo done", ExitCode: 1, ErrorCode: "forbidden", CauseClass: "authorization", Success: false, DurationMS: 8},
		{OccurredAt: 120, SessionID: "session-b", Agent: "claude", Version: "1.2.3", CommandPath: "atm sync", ExitCode: 1, ErrorCode: "unavailable", CauseClass: "database", Retryable: true, Success: false, DurationMS: 25},
	}
	for _, row := range rows {
		if err := RecordCLIInvocation(db, row); err != nil {
			t.Fatalf("record invocation: %v", err)
		}
	}

	all, err := QueryCLIInvocations(db, CLIInvocationQuery{SinceTS: 90, UntilTS: 130, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || all.Matched != 3 || all.Succeeded != 1 || all.Failed != 2 || len(all.Invocations) != 2 {
		t.Fatalf("all invocations = %#v", all)
	}
	if all.Invocations[0].SessionID != "session-b" || all.Invocations[1].CommandPath != "atm todo done" {
		t.Fatalf("invocations are not newest first: %#v", all.Invocations)
	}

	failed, err := QueryCLIInvocations(db, CLIInvocationQuery{
		SessionID: "session-a", Agent: "codex", Failed: true, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if failed.Total != 2 || failed.Matched != 1 || failed.Succeeded != 1 || failed.Failed != 1 ||
		len(failed.Invocations) != 1 || failed.Invocations[0].ErrorCode != "forbidden" {
		t.Fatalf("failed invocations = %#v", failed)
	}
}

func TestCLIInvocationNormalizationCannotPersistFreeFormContent(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	secret := "secret query with spaces /Users/person/private.txt"
	if err := RecordCLIInvocation(db, CLIInvocation{
		OccurredAt: 1, SessionID: secret, Agent: secret, Version: secret,
		CommandPath: "atm todo add " + secret,
		ExitCode:    1, ErrorCode: secret, CauseClass: secret, Success: false,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := QueryCLIInvocations(db, CLIInvocationQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Invocations) != 1 {
		t.Fatalf("invocations = %#v", result.Invocations)
	}
	got := result.Invocations[0]
	if got.SessionID != "" || got.Agent != "" || got.Version != "" ||
		got.CommandPath != "atm" || got.ErrorCode != "" || got.CauseClass != "" {
		t.Fatalf("free-form fields survived normalization: %#v", got)
	}

	var createSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='cli_invocations'`).Scan(&createSQL); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"argv", "argument", "error_message", "working_directory", "cwd", "content"} {
		if strings.Contains(strings.ToLower(createSQL), forbidden) {
			t.Fatalf("cli_invocations schema can store %q: %s", forbidden, createSQL)
		}
	}
}

func TestCLIInvocationBestEffortDoesNotCreateDatabaseAndRecordsWhenReady(t *testing.T) {
	withTempStore(t)
	RecordCLIInvocationBestEffort(CLIInvocation{CommandPath: "atm version", Success: true})
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("best-effort telemetry created a missing database: %v", err)
	}

	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordCLIInvocation(db, CLIInvocation{
		OccurredAt:  time.Now().Add(-cliInvocationRetention - time.Hour).Unix(),
		CommandPath: "atm old", Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	RecordCLIInvocationBestEffort(CLIInvocation{
		OccurredAt: time.Now().Unix(), CommandPath: "atm version", Version: "dev", Success: true,
	})
	db, err = OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := QueryCLIInvocations(db, CLIInvocationQuery{Limit: 10})
	if err != nil || result.Total != 1 || !result.Invocations[0].Success {
		t.Fatalf("best-effort result = %#v, err = %v", result, err)
	}
}
