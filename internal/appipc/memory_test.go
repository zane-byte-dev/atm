package appipc

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
)

func TestMemoryMethodsReturnKnowledgeServiceResultsInIPCEnvelope(t *testing.T) {
	oldDir, oldDB, oldConfig := config.AtmDir, config.AtmDB, config.ConfigPath
	dataDir := t.TempDir()
	config.AtmDir = dataDir
	config.AtmDB = filepath.Join(dataDir, "atm.db")
	config.ConfigPath = filepath.Join(dataDir, "config.json")
	t.Cleanup(func() {
		config.AtmDir, config.AtmDB, config.ConfigPath = oldDir, oldDB, oldConfig
	})

	target, err := knowledge.RememberWithMetadata(
		"global", "ATM memory bridge", []string{"ipc"}, map[string]string{"source": "session:old"},
	)
	if err != nil {
		t.Fatal(err)
	}
	service := knowledge.NewService(knowledge.ServiceOptions{
		DataDir: dataDir,
		Now:     func() time.Time { return time.Date(2026, 8, 20, 5, 0, 0, 0, time.UTC) },
	})
	server := New(Dependencies{Knowledge: service})

	var recallOutput bytes.Buffer
	if err := server.Serve(
		context.Background(),
		"memory.recall",
		strings.NewReader(`{"query":"memory bridge","limit":200}`),
		&recallOutput,
	); err != nil {
		t.Fatalf("recall: %v\n%s", err, recallOutput.String())
	}
	var recalled struct {
		Verb string                       `json:"verb"`
		Data knowledge.RecallMemoryResult `json:"data"`
	}
	if err := json.Unmarshal(recallOutput.Bytes(), &recalled); err != nil {
		t.Fatalf("decode recall: %v\n%s", err, recallOutput.String())
	}
	if recalled.Verb != "memory.recall" || len(recalled.Data.Hits) != 1 || recalled.Data.Hits[0].ID != target.ID {
		t.Fatalf("recall envelope = %+v", recalled)
	}

	request, err := json.Marshal(knowledge.SupersedeMemoryInput{
		TargetID: target.ID,
		Scope:    "global",
		Content:  "Typed memory bridge",
		Tags:     []string{"ipc", "typed"},
		Source:   "session:new",
	})
	if err != nil {
		t.Fatal(err)
	}
	var supersedeOutput bytes.Buffer
	if err := server.Serve(
		context.Background(),
		"memory.supersede",
		bytes.NewReader(request),
		&supersedeOutput,
	); err != nil {
		t.Fatalf("supersede: %v\n%s", err, supersedeOutput.String())
	}
	var superseded struct {
		Verb string                          `json:"verb"`
		Data knowledge.SupersedeMemoryResult `json:"data"`
	}
	if err := json.Unmarshal(supersedeOutput.Bytes(), &superseded); err != nil {
		t.Fatalf("decode supersede: %v\n%s", err, supersedeOutput.String())
	}
	if superseded.Verb != "memory.supersede" || superseded.Data.Event.TargetID != target.ID ||
		superseded.Data.Event.Content != "Typed memory bridge" ||
		superseded.Data.Event.Metadata["source"] != "session:new" {
		t.Fatalf("supersede envelope = %+v", superseded)
	}
}
