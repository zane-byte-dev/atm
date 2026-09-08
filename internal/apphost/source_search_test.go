package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
)

func TestWorkspaceSourceSearchUsesConnector(t *testing.T) {
	h := testHost(t)
	before := config.CollectionConnectors
	t.Cleanup(func() { config.CollectionConnectors = before })
	script := filepath.Join(t.TempDir(), "search.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n[ \"$1\" = search ] || exit 1\nread -r request\nprintf '%s' '{\"candidates\":[{\"kind\":\"group\",\"external_id\":\"fixture-group\",\"name\":\"研发群\"}]}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{"fixture": {Command: script}}
	method, ok := contract.LookupWorkspaceMethod("collect.source.search")
	if !ok || method.Write {
		t.Fatal("search must be registered as a read")
	}
	result, err := h.Call(context.Background(), webCall(), "collect.source.search", json.RawMessage(`{"connector":"fixture","keyword":"研发","kind":"group","limit":30}`), "")
	if err != nil {
		t.Fatal(err)
	}
	candidates := result.(collector.SearchSourcesResult).Candidates
	if len(candidates) != 1 || candidates[0].ExternalID != "fixture-group" || candidates[0].Name != "研发群" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
	for _, input := range []string{`{"connector":"fixture","keyword":""}`, `{"connector":"fixture","keyword":"x","limit":101}`, `{"connector":"fixture","keyword":"x","command":"arbitrary"}`} {
		_, err := h.Call(context.Background(), webCall(), "collect.source.search", json.RawMessage(input), "")
		if !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("input=%s err=%v", input, err)
		}
	}
}
