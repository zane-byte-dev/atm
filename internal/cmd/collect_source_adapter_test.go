package cmd

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func withHumanCollectionCLI(t *testing.T) {
	t.Helper()
	withHumanCLI(t)
}

func TestCollectSourceToggleAdapterPreservesJSONShape(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	oldJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = oldJSON })

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "source-toggle-adapter",
		Priority: "P2", Enabled: true,
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	var runErr error
	encoded := captureStdout(t, func() {
		runErr = collectSourceDisableCmd.RunE(collectSourceDisableCmd, []string{source.ID})
	})
	if runErr != nil {
		t.Fatalf("disable source: %v", runErr)
	}
	var result struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, encoded)
	}
	if result.ID != source.ID || result.Enabled {
		t.Fatalf("result = %+v", result)
	}
}

func TestCollectSourceDeleteAdapterCannotTurnAgentYesIntoAuthority(t *testing.T) {
	withTempAtmDir(t)
	oldYes := collectYes
	collectYes = true
	t.Cleanup(func() { collectYes = oldYes })

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "agent-delete-adapter",
		Priority: "P2", Enabled: true,
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("CODEX_THREAD_ID", "agent-thread")
	err = collectSourceDeleteCmd.RunE(collectSourceDeleteCmd, []string{source.ID})
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent delete error = %v, want forbidden", err)
	}
	db, err = store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := store.GetCollectionSource(db, source.ID); err != nil {
		t.Fatalf("forbidden delete removed source: %v", err)
	}
}
