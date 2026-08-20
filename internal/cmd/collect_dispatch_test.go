package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestCollectionProjectWorkDirUsesConfiguredProjectRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, "mox", "atm")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := collectionProjectWorkDir("atm")
	if err != nil || got != want {
		t.Fatalf("work dir=%q err=%v, want %q", got, err, want)
	}
	if _, err := collectionProjectWorkDir(""); err == nil {
		t.Fatal("empty project resolved for automatic dispatch")
	}
}

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
	t.Setenv("ATM_RUN_ID", "run-42")
	agent := collectionCLICall("promote")
	if agent.Actor.Kind != application.ActorAgent || agent.Actor.Agent != "codex" ||
		agent.Actor.SessionID != "codex-thread-42" || agent.Actor.RunID != "run-42" {
		t.Fatalf("Agent CLI actor = %+v", agent.Actor)
	}
}
