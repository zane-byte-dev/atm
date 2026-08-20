package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	ipctransport "github.com/zane-byte-dev/atm/internal/ipc"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestIPCSessionReadModelsReturnTypedEnvelopes(t *testing.T) {
	withIsolatedCommandEnv(t)
	seedCommandSession(t)

	tests := []struct {
		verb  string
		input string
		check func(*testing.T, json.RawMessage)
	}{
		{
			verb:  "session.list",
			input: `{"all":true,"order":"desc","limit":10}`,
			check: func(t *testing.T, data json.RawMessage) {
				var result sessionapp.ListResult
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Sessions) != 1 || result.Sessions[0].ID != "cmd-session-full" {
					t.Fatalf("list result = %+v", result)
				}
			},
		},
		{
			verb:  "session.search",
			input: `{"keyword":"deployment","limit":200,"snippet":400}`,
			check: func(t *testing.T, data json.RawMessage) {
				var result sessionapp.SearchResult
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if result.Returned != 2 || result.Matches[0].ShortID != "cmdsess" {
					t.Fatalf("search result = %+v", result)
				}
			},
		},
		{
			verb:  "session.show",
			input: `{"session_id":"cmdsess","last":1,"max_chars":6000}`,
			check: func(t *testing.T, data json.RawMessage) {
				var result sessionapp.ShowResult
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if result.ID != "cmd-session-full" || len(result.QA) != 1 || result.QA[0].A == "" {
					t.Fatalf("show result = %+v", result)
				}
			},
		},
		{
			verb:  "session.timeline",
			input: `{"session_id":"cmdsess"}`,
			check: func(t *testing.T, data json.RawMessage) {
				var result sessionapp.TimelineResult
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Events) < 2 || result.Events[0].Kind != "message" {
					t.Fatalf("timeline result = %+v", result)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.verb, func(t *testing.T) {
			var output bytes.Buffer
			err := ipcServer.Serve(
				context.Background(), test.verb, strings.NewReader(test.input), &output,
			)
			if err != nil {
				t.Fatalf("Serve: %v\n%s", err, output.String())
			}
			var envelope struct {
				Verb  string          `json:"verb"`
				Data  json.RawMessage `json:"data"`
				Error json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v\n%s", err, output.String())
			}
			if envelope.Verb != test.verb || len(envelope.Data) == 0 ||
				(len(envelope.Error) != 0 && string(envelope.Error) != "null") {
				t.Fatalf("envelope = %+v", envelope)
			}
			test.check(t, envelope.Data)
		})
	}
}

func TestIPCSessionUnavailableMessageDoesNotLeakDatabasePath(t *testing.T) {
	withTempAtmDir(t)
	var output bytes.Buffer
	err := ipcServer.Serve(
		context.Background(), "session.list", strings.NewReader(`{"all":true}`), &output,
	)
	if err == nil {
		t.Fatal("session.list unexpectedly succeeded without a database")
	}
	var envelope struct {
		Data  json.RawMessage            `json:"data"`
		Error *ipctransport.ErrorPayload `json:"error"`
	}
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v\n%s", decodeErr, output.String())
	}
	if envelope.Error == nil || envelope.Error.Message != store.ErrDatabaseMissing.Error() {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
	if strings.Contains(envelope.Error.Message, config.AtmDB) || len(envelope.Data) != 0 {
		t.Fatalf("unsafe unavailable envelope = %s", output.String())
	}
}
