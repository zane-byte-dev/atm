package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
)

func TestIPCKnowledgeSaveQueryAndConfirmedDeleteUseServiceContracts(t *testing.T) {
	withTempAtmDir(t)

	document := runKnowledgeIPCSuccess[knowledge.SaveDocumentInput, knowledge.Document](
		t,
		"knowledge.document.save",
		knowledge.SaveDocumentInput{Create: &knowledge.CreateDocumentInput{
			Title: "Typed IPC", Content: "cross language marker", Collection: "notes",
			Tags: []string{"ipc"}, Producer: "human",
		}},
	)
	if document.Metadata.ID == "" || document.Metadata.Title != "Typed IPC" || document.Collection != "notes" {
		t.Fatalf("saved document = %#v", document)
	}
	if _, err := knowledge.Get(config.AtmDir, document.Metadata.ID); err != nil {
		t.Fatalf("typed IPC did not use isolated Knowledge data dir %s: %v", config.AtmDir, err)
	}

	query := runKnowledgeIPCSuccess[knowledge.QueryInput, knowledge.QueryResult](
		t,
		"knowledge.query",
		knowledge.QueryInput{Text: "cross language marker", Status: "active", Limit: 20},
	)
	if len(query.Documents) != 1 || query.Documents[0].DocumentID != document.Metadata.ID || query.Documents[0].Score == nil {
		t.Fatalf("query = %#v", query)
	}

	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(strings.NewReader(mustKnowledgeJSON(t, knowledge.DeleteDocumentInput{
		DocumentID: document.Metadata.ID,
	})))
	err := runIPC(ipcCmd, []string{"knowledge.document.delete"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed delete error = %v", err)
	}
	if _, err := knowledge.Get(config.AtmDir, document.Metadata.ID); err != nil {
		t.Fatalf("unconfirmed delete removed document: %v", err)
	}

	deleted := runKnowledgeIPCSuccess[knowledge.DeleteDocumentInput, knowledge.Document](
		t,
		"knowledge.document.delete",
		knowledge.DeleteDocumentInput{DocumentID: document.Metadata.ID, Confirmed: true},
	)
	if deleted.Metadata.ID != document.Metadata.ID {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func runKnowledgeIPCSuccess[Request any, Response any](t *testing.T, verb string, request Request) Response {
	t.Helper()
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(strings.NewReader(mustKnowledgeJSON(t, request)))
	t.Cleanup(func() {
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
	})
	if err := runIPC(ipcCmd, []string{verb}); err != nil {
		t.Fatalf("%s: %v\n%s", verb, err, out.String())
	}
	var envelope struct {
		Verb string   `json:"verb"`
		Data Response `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s envelope: %v\n%s", verb, err, out.String())
	}
	if envelope.Verb != verb {
		t.Fatalf("verb = %q, want %q", envelope.Verb, verb)
	}
	return envelope.Data
}

func mustKnowledgeJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
