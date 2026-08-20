package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestIPCRefreshesConfigBackedConnectorsAfterConfigLoad(t *testing.T) {
	withTempAtmDir(t)
	oldConnectors := config.CollectionConnectors
	config.CollectionConnectors = nil
	ipcServer = newAppIPCServer()
	t.Cleanup(func() { config.CollectionConnectors = oldConnectors })

	connectorPath := filepath.Join(t.TempDir(), "connector")
	body := `#!/bin/sh
read request
printf '%s\n' '{"candidates":[{"kind":"bot","external_id":"bot-1","name":"Code助手"}]}'
`
	if err := os.WriteFile(connectorPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{
		"dingtalk": {Command: connectorPath, TimeoutSeconds: 5},
	}
	refreshAppIPCServer()

	var envelope struct {
		Data collector.SearchSourcesResult `json:"data"`
	}
	callCollectIPC(t, "collect.source.search",
		`{"connector":"dingtalk","kind":"all","keyword":"Code助手","limit":1}`, &envelope)
	if len(envelope.Data.Candidates) != 1 || envelope.Data.Candidates[0].ExternalID != "bot-1" {
		t.Fatalf("search candidates = %+v", envelope.Data.Candidates)
	}
}

func TestIPCCollectSnapshotReturnsTheSharedReadModel(t *testing.T) {
	withTempAtmDir(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "ipc-snapshot",
		Priority: "P1", Enabled: true,
	})
	if err == nil {
		_, _, err = store.PutCollectionItem(db, store.CollectionItem{
			SourceID: source.ID, Connector: "test", Fingerprint: "ipc-snapshot-item",
			MessageIDs: []string{"m1"}, Action: "insight", Status: "processed",
		})
	}
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		Verb string             `json:"verb"`
		Data collector.Snapshot `json:"data"`
	}
	callCollectIPC(t, "collect.snapshot", `{"item_limit":20}`, &envelope)
	if envelope.Verb != "collect.snapshot" || len(envelope.Data.Sources) != 1 ||
		len(envelope.Data.Items) != 1 || envelope.Data.Items[0].Fingerprint != "ipc-snapshot-item" {
		t.Fatalf("snapshot envelope = %+v", envelope)
	}
}

func TestIPCCollectDestructiveMethodsRequireServiceConfirmation(t *testing.T) {
	withTempAtmDir(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "ipc-delete-source",
		Priority: "P2", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	item, _, err := store.PutCollectionItem(db, store.CollectionItem{
		SourceID: source.ID, Connector: "test", Fingerprint: "ipc-delete-item",
		MessageIDs: []string{"m1"}, Action: "ignore", Status: "processed",
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	if err := rawCollectIPC("collect.source.delete",
		`{"source_id":"`+source.ID+`","confirmed":false}`, nil); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed source delete = %v", err)
	}
	if err := rawCollectIPC("collect.item.delete",
		`{"item_ids":["`+item.ID+`"],"confirmed":false}`, nil); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed item delete = %v", err)
	}

	var deletedItems struct {
		Data collector.DeleteItemsResult `json:"data"`
	}
	callCollectIPC(t, "collect.item.delete",
		`{"item_ids":["`+item.ID+`"],"confirmed":true}`, &deletedItems)
	if deletedItems.Data.Count != 1 || deletedItems.Data.Deleted[0].ID != item.ID {
		t.Fatalf("deleted items = %+v", deletedItems.Data)
	}
	var deletedSource struct {
		Data collector.DeleteSourceResult `json:"data"`
	}
	callCollectIPC(t, "collect.source.delete",
		`{"source_id":"`+source.ID+`","confirmed":true}`, &deletedSource)
	if deletedSource.Data.Source.ID != source.ID {
		t.Fatalf("deleted source = %+v", deletedSource.Data)
	}
}

func TestIPCCollectReadStateDoesNotAcceptActionOrArgvBags(t *testing.T) {
	withTempAtmDir(t)
	err := rawCollectIPC(
		"collect.item.read",
		`{"item_ids":[],"all":true,"read":true,"action":"delete"}`,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("untyped action bag error = %v", err)
	}
}

func callCollectIPC(t *testing.T, verb, input string, target any) {
	t.Helper()
	var output bytes.Buffer
	if err := rawCollectIPC(verb, input, &output); err != nil {
		t.Fatalf("%s: %v\n%s", verb, err, output.String())
	}
	if err := json.Unmarshal(output.Bytes(), target); err != nil {
		t.Fatalf("decode %s: %v\n%s", verb, err, output.String())
	}
}

func rawCollectIPC(verb, input string, output *bytes.Buffer) error {
	if output == nil {
		output = &bytes.Buffer{}
	}
	previousIn, previousOut := ipcCmd.InOrStdin(), ipcCmd.OutOrStdout()
	ipcCmd.SetIn(strings.NewReader(input))
	ipcCmd.SetOut(output)
	defer func() {
		ipcCmd.SetIn(previousIn)
		ipcCmd.SetOut(previousOut)
	}()
	return runIPC(ipcCmd, []string{verb})
}
