package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestGuardCLICallDerivesAgentAuthorityFromEnvironment(t *testing.T) {
	withHumanCLI(t)
	if call := guardCLICall(); call.Actor.Kind != application.ActorHuman {
		t.Fatalf("plain CLI actor = %q, want human", call.Actor.Kind)
	}

	t.Setenv("CODEX_THREAD_ID", "codex-thread-42")
	call := guardCLICall()
	if call.Actor.Kind != application.ActorAgent || call.Actor.Agent != "codex" ||
		call.Actor.SessionID != "codex-thread-42" {
		t.Fatalf("Agent CLI call = %#v", call.Actor)
	}
}

func TestAgentGuardCLICannotDecideAHumanApproval(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	t.Setenv("CODEX_THREAD_ID", "codex-thread-42")

	now := time.Now().Unix()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateApproval(db, store.Approval{
		Tool: "dws", RealBin: "/tmp/dws-atm-real", Argv: []string{"chat", "send"},
		RequestedAt: now, ExpiresAt: now + 600, GateDeadline: now + 60,
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	command := &cobra.Command{}
	command.SetContext(context.Background())
	err = runGuardDecision(command, created.ID, true)
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent Guard decision error = %v, want forbidden", err)
	}

	db, err = store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	approval, err := store.GetApproval(db, created.ID, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil || approval.Status != store.ApprovalPending {
		t.Fatalf("approval after Agent decision = %#v, want pending", approval)
	}
}
