package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// `mute` and `disable` are neighbours in the command list and easy to confuse, so
// what separates them is asserted here: muting silences the banner and leaves the
// source collecting, and `collect status` still says the source is enabled.
func TestCollectSourceMuteSilencesNotificationsWithoutPausingCollection(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	oldJSON, oldLimit := jsonOutput, collectLimit
	t.Cleanup(func() { jsonOutput, collectLimit = oldJSON, oldLimit })

	db, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-mute-cmd",
		Name: "静默测试群", Project: "atm", Priority: "P2", Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	jsonOutput = true
	var runErr error
	mutedJSON := captureStdout(t, func() {
		runErr = collectSourceMuteCmd.RunE(collectSourceMuteCmd, []string{source.ID})
	})
	if runErr != nil {
		t.Fatalf("source mute: %v", runErr)
	}
	var decision struct {
		ID    string `json:"id"`
		Muted bool   `json:"muted"`
	}
	if err := json.Unmarshal([]byte(mutedJSON), &decision); err != nil {
		t.Fatalf("decode mute: %v\n%s", err, mutedJSON)
	}
	if decision.ID != source.ID || !decision.Muted {
		t.Fatalf("unexpected mute result: %+v", decision)
	}

	collectLimit = 20
	statusJSON := captureStdout(t, func() {
		runErr = collectStatusCmd.RunE(collectStatusCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("collect status: %v", runErr)
	}
	var status struct {
		Summary store.CollectionSummary  `json:"summary"`
		Sources []store.CollectionSource `json:"sources"`
	}
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusJSON)
	}
	if len(status.Sources) != 1 || !status.Sources[0].Muted || !status.Sources[0].Enabled {
		t.Fatalf("a muted source must stay enabled: %+v", status.Sources)
	}
	if status.Summary.Enabled != 1 {
		t.Fatalf("muting must not change the enabled count: %+v", status.Summary)
	}

	// The plain-text listing has to say a source is muted, otherwise the missing
	// banner has no visible cause anywhere in the CLI.
	jsonOutput = false
	listing := captureStdout(t, func() {
		runErr = collectSourceListCmd.RunE(collectSourceListCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("source list: %v", runErr)
	}
	if !strings.Contains(listing, "桌面通知已静默") {
		t.Fatalf("listing does not report the mute:\n%s", listing)
	}

	jsonOutput = true
	unmutedJSON := captureStdout(t, func() {
		runErr = collectSourceUnmuteCmd.RunE(collectSourceUnmuteCmd, []string{source.ID})
	})
	if runErr != nil {
		t.Fatalf("source unmute: %v", runErr)
	}
	if err := json.Unmarshal([]byte(unmutedJSON), &decision); err != nil {
		t.Fatalf("decode unmute: %v\n%s", err, unmutedJSON)
	}
	if decision.Muted {
		t.Fatalf("unexpected unmute result: %+v", decision)
	}
	if err := collectSourceMuteCmd.RunE(collectSourceMuteCmd, []string{"cs_missing"}); err == nil {
		t.Fatal("muting an unknown source must fail")
	}
}
