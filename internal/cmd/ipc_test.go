package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/guard"
	ipctransport "github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/store"
)

// The whole reason config get stopped keeping its own switch over the keys is
// that the two lists drifted. This is what keeps them from drifting again: a key
// that can be written but not read is a setting the desktop screen would appear
// to accept and then show unchanged.
func TestSettableAndReadableConfigKeysMatch(t *testing.T) {
	readable := config.SettingValues(config.Settings{})
	settable := map[string]bool{}
	for _, key := range config.SettableKeys() {
		settable[key] = true
		if _, ok := readable[key]; !ok {
			t.Errorf("%q is settable but config.SettingValues does not return it, so `atm config get %s` fails", key, key)
		}
	}
	for key := range readable {
		if !settable[key] {
			t.Errorf("%q is readable but not in config.SettableKeys, so `atm config set %s` fails", key, key)
		}
	}
}

func TestIPCWrapsEveryAnswerInAVersionedEnvelope(t *testing.T) {
	withTempAtmDir(t)
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	t.Cleanup(func() { ipcCmd.SetOut(nil) })

	if err := runIPC(ipcCmd, []string{"config.settings"}); err != nil {
		t.Fatalf("config.settings: %v", err)
	}
	var envelope struct {
		EnvelopeVersion int             `json:"envelope_version"`
		ProtocolVersion int             `json:"protocol_version"`
		RequestID       string          `json:"request_id"`
		Verb            string          `json:"verb"`
		Data            json.RawMessage `json:"data"`
		Error           json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	if envelope.ProtocolVersion != contract.IPCProtocolVersion {
		t.Errorf("protocol_version = %d, want %d", envelope.ProtocolVersion, contract.IPCProtocolVersion)
	}
	if envelope.EnvelopeVersion != ipctransport.EnvelopeVersion {
		t.Errorf("envelope_version = %d, want %d", envelope.EnvelopeVersion, ipctransport.EnvelopeVersion)
	}
	if !strings.HasPrefix(envelope.RequestID, "ipc-") {
		t.Errorf("request_id = %q, want generated ipc-* ID", envelope.RequestID)
	}
	if envelope.Verb != "config.settings" {
		t.Errorf("verb = %q, want config.settings", envelope.Verb)
	}
	if len(envelope.Data) == 0 {
		t.Error("data is empty")
	}
	if len(envelope.Error) != 0 && string(envelope.Error) != "null" {
		t.Errorf("success envelope has error: %s", envelope.Error)
	}
}

// The envelope must not depend on the caller having passed --json. A read command
// that ran earlier in the same process can leave text mode set, and a protocol
// that answers differently based on that is not a protocol.
func TestIPCIgnoresTextOutputMode(t *testing.T) {
	withTempAtmDir(t)
	previous := jsonOutput
	jsonOutput = false
	t.Cleanup(func() { jsonOutput = previous })

	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	t.Cleanup(func() { ipcCmd.SetOut(nil) })
	if err := runIPC(ipcCmd, []string{"config.settings"}); err != nil {
		t.Fatalf("config.settings: %v", err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("not JSON without --json:\n%s", out.String())
	}
}

func TestIPCDaySnapshotReturnsTheAggregateContract(t *testing.T) {
	withTempAtmDir(t)
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	t.Cleanup(func() { ipcCmd.SetOut(nil) })

	if err := runIPC(ipcCmd, []string{"day.snapshot"}); err != nil {
		t.Fatalf("day.snapshot: %v", err)
	}
	var envelope struct {
		RequestID string          `json:"request_id"`
		Verb      string          `json:"verb"`
		Data      aiday.Dashboard `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	if envelope.RequestID == "" || envelope.Verb != "day.snapshot" {
		t.Fatalf("identity = request:%q verb:%q", envelope.RequestID, envelope.Verb)
	}
	if envelope.Data.SchemaVersion != aiday.ContractVersion || envelope.Data.Today.Day == "" {
		t.Fatalf("dashboard = %+v", envelope.Data)
	}
}

func TestIPCDashboardSnapshotReturnsWorkThroughTheTypedEnvelope(t *testing.T) {
	withIsolatedCommandEnv(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Typed dashboard", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	seedCommandSession(t)

	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(strings.NewReader(`{"sections":["work"]}`))
	t.Cleanup(func() {
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
	})
	if err := runIPC(ipcCmd, []string{"dashboard.snapshot"}); err != nil {
		t.Fatalf("dashboard.snapshot: %v", err)
	}
	var envelope struct {
		Verb string             `json:"verb"`
		Data dashboard.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	if envelope.Verb != "dashboard.snapshot" || len(envelope.Data.Todos) != 1 ||
		envelope.Data.Todos[0].ID != "t1" || envelope.Data.Work.Summary.Open != 1 {
		t.Fatalf("dashboard envelope = %+v", envelope)
	}
	if len(envelope.Data.Ranges) != 0 || envelope.Data.DayStats == nil {
		t.Fatalf("work-only sections = ranges:%v day_stats:%#v", envelope.Data.Ranges, envelope.Data.DayStats)
	}
}

func TestIPCDashboardRejectsUnknownSectionBeforeReadingState(t *testing.T) {
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(strings.NewReader(`{"sections":["todos"]}`))
	t.Cleanup(func() {
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
	})
	err := runIPC(ipcCmd, []string{"dashboard.snapshot"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid_argument", err)
	}
	var envelope struct {
		Error *ipctransport.ErrorPayload `json:"error"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v\n%s", decodeErr, out.String())
	}
	if envelope.Error == nil || envelope.Error.Code != ipctransport.CodeInvalidArgument {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
}

func TestIPCDayValidationErrorKeepsApplicationDetails(t *testing.T) {
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(strings.NewReader(`{"day":"not-a-day"}`))
	t.Cleanup(func() {
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
	})

	err := runIPC(ipcCmd, []string{"day.show"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("day.show error = %v, want invalid_argument", err)
	}
	var envelope struct {
		RequestID string                     `json:"request_id"`
		Verb      string                     `json:"verb"`
		Data      json.RawMessage            `json:"data"`
		Error     *ipctransport.ErrorPayload `json:"error"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v\n%s", decodeErr, out.String())
	}
	if envelope.RequestID == "" || envelope.Verb != "day.show" {
		t.Fatalf("identity = request:%q verb:%q", envelope.RequestID, envelope.Verb)
	}
	if envelope.Error == nil || envelope.Error.Code != ipctransport.CodeInvalidArgument {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
	details, ok := envelope.Error.Details.(map[string]any)
	if !ok || details["field"] != "day" || details["value"] != "not-a-day" {
		t.Fatalf("error details = %#v", envelope.Error.Details)
	}
	if len(envelope.Data) != 0 && string(envelope.Data) != "null" {
		t.Fatalf("error envelope has data: %s", envelope.Data)
	}
}

// config.save exists because saving the model form as four sequential
// `atm config set` calls could not fail as a unit. These tests are about that
// property, not about the happy path.
func TestIPCConfigSaveWritesEveryFieldOrNone(t *testing.T) {
	withTempAtmDir(t)
	previousName, previousURL := config.TextModelName, config.TextModelBaseURL
	t.Cleanup(func() {
		config.TextModelName, config.TextModelBaseURL = previousName, previousURL
	})

	save := func(t *testing.T, body string) (config.Settings, error) {
		t.Helper()
		var out bytes.Buffer
		ipcCmd.SetOut(&out)
		ipcCmd.SetIn(strings.NewReader(body))
		t.Cleanup(func() {
			ipcCmd.SetOut(nil)
			ipcCmd.SetIn(nil)
		})
		if err := runIPC(ipcCmd, []string{"config.save"}); err != nil {
			return config.Settings{}, err
		}
		var envelope struct {
			Data config.Settings `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v\n%s", err, out.String())
		}
		return envelope.Data, nil
	}

	settings, err := save(t, `{"text_model_name":"good-model","todo_refine_on_add":true}`)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// The answer must be the state after the write. It used to be the state from
	// process start, because the in-memory values are loaded once and the write
	// did not reload them — so the screen redrew what the user had just replaced.
	if settings.TextModelName != "good-model" || !settings.TodoRefineOnAdd {
		t.Fatalf("save answered with stale state: %+v", settings)
	}

	// A batch with one bad value must leave nothing applied, including the fields
	// that came before it.
	if _, err := save(t, `{"text_model_source":"changed","text_model_base_url":"not-a-url"}`); err == nil {
		t.Fatal("an invalid endpoint was accepted")
	} else if !strings.Contains(err.Error(), "text_model_base_url") {
		t.Errorf("error does not name the offending key: %v", err)
	}
	after, err := save(t, `{"text_model_name":"good-model"}`)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.TextModelSource == "changed" {
		t.Error("a rejected batch applied one of its fields")
	}
}

func TestIPCConfigSaveRejectsWhatConfigSetWouldReject(t *testing.T) {
	withTempAtmDir(t)
	for _, testCase := range []struct{ name, body, wants string }{
		{"unknown key", `{"nope":1}`, "unknown field"},
		{"empty stdin", ``, "stdin was empty"},
		{"nothing to write", `{}`, "no settings given"},
		{"bad url", `{"text_model_base_url":"ftp://x"}`, "http or https"},
		{"empty model name", `{"text_model_name":"  "}`, "must not be empty"},
		{"negative interval", `{"collection_interval_minutes":-1}`, "invalid value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ipcCmd.SetOut(&bytes.Buffer{})
			ipcCmd.SetIn(strings.NewReader(testCase.body))
			t.Cleanup(func() {
				ipcCmd.SetOut(nil)
				ipcCmd.SetIn(nil)
			})
			err := runIPC(ipcCmd, []string{"config.save"})
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.wants) {
				t.Errorf("error = %v, want it to mention %q", err, testCase.wants)
			}
		})
	}
}

func TestIPCCredentialSaveAndDeleteNeverEchoTheSecret(t *testing.T) {
	withTempAtmDir(t)
	const secret = "sk-ipc-secret-must-not-escape"

	call := func(t *testing.T, verb, body string) []byte {
		t.Helper()
		var out bytes.Buffer
		ipcCmd.SetOut(&out)
		ipcCmd.SetIn(strings.NewReader(body))
		if err := runIPC(ipcCmd, []string{verb}); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
		return out.Bytes()
	}

	saved := call(t, "config.credential.save", `{"api_key":"`+secret+`"}`)
	if bytes.Contains(saved, []byte(secret)) {
		t.Fatalf("save response leaked credential: %s", saved)
	}
	var saveEnvelope struct {
		Data config.CredentialStatus `json:"data"`
	}
	if err := json.Unmarshal(saved, &saveEnvelope); err != nil || !saveEnvelope.Data.Configured {
		t.Fatalf("save envelope = %+v, %v\n%s", saveEnvelope, err, saved)
	}
	if value, err := config.ReadTextModelAPIKey(); err != nil || value != secret {
		t.Fatalf("saved credential = %q, %v", value, err)
	}

	deleted := call(t, "config.credential.delete", "")
	var deleteEnvelope struct {
		Data config.CredentialStatus `json:"data"`
	}
	if err := json.Unmarshal(deleted, &deleteEnvelope); err != nil || deleteEnvelope.Data.Configured {
		t.Fatalf("delete envelope = %+v, %v\n%s", deleteEnvelope, err, deleted)
	}
}

func TestIPCTextModelCheckUsesStdinDraftsWithoutLeakingTheKey(t *testing.T) {
	withTempAtmDir(t)
	const secret = "sk-unsaved-check-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Errorf("authorization = %q", got)
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "draft-model" {
			t.Errorf("model = %q", request.Model)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	body, err := json.Marshal(map[string]string{
		"api_key":  secret,
		"base_url": server.URL,
		"model":    "draft-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(bytes.NewReader(body))
	t.Cleanup(func() {
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
	})
	if err := runIPC(ipcCmd, []string{"config.text_model.check"}); err != nil {
		t.Fatalf("check: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte(secret)) {
		t.Fatalf("check response leaked credential: %s", out.String())
	}
	var envelope struct {
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !envelope.Data.OK {
		t.Fatalf("check envelope = %+v, %v\n%s", envelope, err, out.String())
	}
	configured, err := config.TextModelAPIKeyConfigured()
	if err != nil || configured {
		t.Fatalf("check persisted draft credential: configured=%v err=%v", configured, err)
	}
}

func TestIPCGuardListCallsTheGuardService(t *testing.T) {
	withTempAtmDir(t)
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

	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	ipcCmd.SetIn(strings.NewReader(`{"status":"pending","limit":50}`))
	t.Cleanup(func() {
		ipcCmd.SetOut(nil)
		ipcCmd.SetIn(nil)
	})
	if err := runIPC(ipcCmd, []string{"guard.list"}); err != nil {
		t.Fatalf("guard.list: %v", err)
	}
	var envelope struct {
		Data guard.ListResult `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode guard.list: %v\n%s", err, out.String())
	}
	if len(envelope.Data.Approvals) != 1 || envelope.Data.Approvals[0].ID != created.ID {
		t.Fatalf("approvals = %#v, want %s", envelope.Data.Approvals, created.ID)
	}
}

func TestIPCRejectsUnknownVerb(t *testing.T) {
	var out bytes.Buffer
	ipcCmd.SetOut(&out)
	t.Cleanup(func() { ipcCmd.SetOut(nil) })
	err := runIPC(ipcCmd, []string{"todo.nope"})
	if err == nil {
		t.Fatal("expected an error for an unknown verb")
	}
	// The message has to name the alternatives: the caller is a Swift build that
	// cannot read the source, and its author is reading this line in a log.
	for _, name := range ipcServer.Names() {
		if !bytes.Contains([]byte(err.Error()), []byte(name)) {
			t.Errorf("error does not mention known verb %q: %v", name, err)
		}
	}
	var envelope struct {
		RequestID string                     `json:"request_id"`
		Verb      string                     `json:"verb"`
		Data      json.RawMessage            `json:"data"`
		Error     *ipctransport.ErrorPayload `json:"error"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode error envelope: %v\n%s", decodeErr, out.String())
	}
	if envelope.RequestID == "" || envelope.Verb != "todo.nope" {
		t.Errorf("error identity = request:%q verb:%q", envelope.RequestID, envelope.Verb)
	}
	if envelope.Error == nil || envelope.Error.Code != ipctransport.CodeMethodNotFound {
		t.Errorf("error payload = %+v", envelope.Error)
	}
	if len(envelope.Data) != 0 && string(envelope.Data) != "null" {
		t.Errorf("error envelope has data: %s", envelope.Data)
	}
}
