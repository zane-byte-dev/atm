package cmd

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestCollectionCLICallDerivesActorFromTrustedEnvironment(t *testing.T) {
	withHumanCLI(t)
	human := collectionCLICall("correct")
	if human.Actor.Kind != application.ActorHuman || human.Actor.Origin != application.OriginCLI {
		t.Fatalf("plain terminal actor = %+v", human.Actor)
	}
	if !strings.HasPrefix(human.RequestID, "cli-collect-correct-") {
		t.Fatalf("request ID = %q", human.RequestID)
	}

	t.Setenv("CODEX_THREAD_ID", "codex-thread-42")
	agent := collectionCLICall("promote")
	if agent.Actor.Kind != application.ActorAgent || agent.Actor.Agent != "codex" ||
		agent.Actor.SessionID != "codex-thread-42" {
		t.Fatalf("Agent CLI actor = %+v", agent.Actor)
	}
}
