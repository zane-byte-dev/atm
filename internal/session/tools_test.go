package session

import (
	"context"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestServiceToolsReturnsFilteredLedgerWithDenominator(t *testing.T) {
	fixture := newServiceFixture(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, invocation := range []store.CLIInvocation{
		{OccurredAt: fixture.now.Unix() - 2, SessionID: "session-recent-full", Agent: "codex", Version: "dev", CommandPath: "atm todo list", Success: true, DurationMS: 10},
		{OccurredAt: fixture.now.Unix() - 1, SessionID: "session-recent-full", Agent: "codex", Version: "dev", CommandPath: "atm todo done", ExitCode: 1, ErrorCode: "forbidden", CauseClass: "authorization", Success: false, DurationMS: 20},
		{OccurredAt: fixture.now.Unix() - 1, SessionID: "other", Agent: "claude", Version: "dev", CommandPath: "atm sync", ExitCode: 1, ErrorCode: "unavailable", CauseClass: "database", Success: false, DurationMS: 30},
	} {
		if err := store.RecordCLIInvocation(db, invocation); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := fixture.service.Tools(context.Background(), ToolsInput{
		SessionID: "session-recent-full", Agent: "codex", Days: 1, Failed: true, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Matched != 1 || result.Succeeded != 1 || result.Failed != 1 || len(result.Invocations) != 1 {
		t.Fatalf("tools result = %#v", result)
	}
	invocation := result.Invocations[0]
	if invocation.CommandPath != "atm todo done" || invocation.ErrorCode != "forbidden" || invocation.OccurredAt == "" {
		t.Fatalf("tool invocation = %#v", invocation)
	}
}

func TestServiceToolsValidatesBoundedQuery(t *testing.T) {
	fixture := newServiceFixture(t)
	for _, input := range []ToolsInput{
		{Days: -1},
		{Since: "2026-08-20", Days: 1},
		{Limit: 1001},
		{Offset: -1},
	} {
		_, err := fixture.service.Tools(context.Background(), input)
		if !applicationErrorIs(err, application.CodeInvalidArgument) {
			t.Fatalf("Tools(%#v) error = %v, want invalid_argument", input, err)
		}
	}
}

func applicationErrorIs(err error, code application.ErrorCode) bool {
	appErr, ok := err.(*application.Error)
	return ok && appErr.Code == code
}
