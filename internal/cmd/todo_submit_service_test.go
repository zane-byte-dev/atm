package cmd

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoSubmitCLICallUsesHumanForPlainTerminal(t *testing.T) {
	oldSessionFlag := sessionIDFlag
	sessionIDFlag = "untrusted-flag-session"
	t.Cleanup(func() { sessionIDFlag = oldSessionFlag })
	withHumanCLI(t)

	call := todoSubmitCLICall()
	if err := call.Validate(); err != nil {
		t.Fatalf("call.Validate(): %v", err)
	}
	if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginCLI {
		t.Fatalf("actor = %+v, want human@cli", call.Actor)
	}
	if call.Actor.SessionID != "" || call.Actor.Agent != "" || call.Actor.RunID != "" {
		t.Fatalf("plain terminal provenance = %+v", call.Actor)
	}
	if !strings.HasPrefix(call.RequestID, "cli-todo-submit-") {
		t.Fatalf("request ID = %q", call.RequestID)
	}
}

func TestTodoSubmitCLICallUsesTrustedAgentEnvironment(t *testing.T) {
	oldSessionFlag := sessionIDFlag
	sessionIDFlag = "untrusted-flag-session"
	t.Cleanup(func() { sessionIDFlag = oldSessionFlag })
	withHumanCLI(t)
	t.Setenv("CODEX_THREAD_ID", "codex-thread-42")
	t.Setenv("ATM_RUN_ID", "run-42")

	call := todoSubmitCLICall()
	if err := call.Validate(); err != nil {
		t.Fatalf("call.Validate(): %v", err)
	}
	if call.Actor.Kind != application.ActorAgent || call.Actor.Origin != application.OriginCLI ||
		call.Actor.Agent != "codex" || call.Actor.SessionID != "codex-thread-42" || call.Actor.RunID != "run-42" {
		t.Fatalf("actor = %+v, want codex Agent CLI provenance", call.Actor)
	}
}

func TestTaskRunSubmitCallUsesControllerProvenance(t *testing.T) {
	run := store.TaskRun{ID: "run-42", TodoID: "t9", Agent: "codex"}
	call := taskRunSubmitCall(run, "  codex-thread-42  ")
	if err := call.Validate(); err != nil {
		t.Fatalf("call.Validate(): %v", err)
	}
	if call.RequestID != "run:run-42:submit" {
		t.Fatalf("request ID = %q", call.RequestID)
	}
	if call.Actor.Kind != application.ActorController || call.Actor.Origin != application.OriginController ||
		call.Actor.RunID != run.ID || call.Actor.SessionID != "codex-thread-42" || call.Actor.Agent != "codex" {
		t.Fatalf("actor = %+v, want controller provenance", call.Actor)
	}
}
