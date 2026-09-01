package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCLIInvocationClassificationNeverCopiesErrorContent(t *testing.T) {
	secret := "api-key=do-not-store /Users/person/private.txt"
	appErr := application.WrapError(application.CodeUnavailable, secret, os.ErrPermission)
	appErr.Retryable = true
	errorCode, causeClass, retryable := classifyCLIInvocationError(appErr)
	if errorCode != "unavailable" || causeClass != "filesystem" || !retryable {
		t.Fatalf("classification = %q/%q retry=%v", errorCode, causeClass, retryable)
	}
	if strings.Contains(errorCode+causeClass, secret) {
		t.Fatal("classification copied the error message")
	}

	for _, test := range []struct {
		err       error
		code      string
		cause     string
		retryable bool
	}{
		{exitError{code: guardExitDenied, err: errors.New(secret)}, "guard_denied", "guard", false},
		{exitError{code: guardExitPending, err: errors.New(secret)}, "guard_pending", "guard", true},
		{errors.New(secret), "command_failed", "command", false},
	} {
		code, cause, retry := classifyCLIInvocationError(test.err)
		if code != test.code || cause != test.cause || retry != test.retryable ||
			strings.Contains(code+cause, secret) {
			t.Fatalf("classification(%T) = %q/%q retry=%v", test.err, code, cause, retry)
		}
	}
}

func TestCLIInvocationAttributionAndCommandPathExcludeArguments(t *testing.T) {
	withCommandFlags(t)
	oldArgs, oldVersion := os.Args, rootCmd.Version
	t.Cleanup(func() { os.Args, rootCmd.Version = oldArgs, oldVersion })
	os.Args = []string{"atm", "todo", "add", "secret-title"}
	t.Setenv("CODEX_THREAD_ID", "codex-session-42")
	rootCmd.Version = "9.9.9-test"

	invocation := cliInvocationForExecution(time.Now().Add(-25*time.Millisecond), errors.New("secret-title"), 1)
	if invocation.SessionID != "codex-session-42" || invocation.Agent != "codex" ||
		invocation.Version != "9.9.9-test" || invocation.CommandPath != "atm todo add" ||
		invocation.Success || invocation.ExitCode != 1 || invocation.DurationMS < 0 {
		t.Fatalf("invocation = %#v", invocation)
	}
	encoded, err := json.Marshal(invocation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-title") {
		t.Fatalf("invocation retained a command argument: %s", encoded)
	}
}

func TestInvocationCommandPathHandlesRootFlagsWithoutKeepingValues(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"todo", "add", "secret-title"}, "atm todo add"},
		{[]string{"--json", "session", "tools", "--failed"}, "atm session tools"},
		{[]string{"--agent", "codex", "session", "list"}, "atm session list"},
		{[]string{"--agent=codex", "stats"}, "atm stats"},
	} {
		if got := invocationCommandPath(test.args); got != test.want {
			t.Errorf("invocationCommandPath(%q) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestCLIInvocationSeparatesCobraContractFailureFromCommandFailure(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() {
		os.Args = oldArgs
		cliCommandEnteredRun.Store(false)
	})
	os.Args = []string{"atm", "todo", "list", "unexpected"}

	cliCommandEnteredRun.Store(false)
	contract := cliInvocationForExecution(time.Now(), errors.New("unexpected argument"), 1)
	if contract.ErrorCode != "invalid_invocation" || contract.CauseClass != "command_contract" {
		t.Fatalf("contract classification = %#v", contract)
	}
	cliCommandEnteredRun.Store(true)
	runtime := cliInvocationForExecution(time.Now(), errors.New("runtime failure"), 1)
	if runtime.ErrorCode != "command_failed" || runtime.CauseClass != "command" {
		t.Fatalf("runtime classification = %#v", runtime)
	}
}

func TestSessionToolsAdapterReturnsFailedInvocationEnvelope(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for _, invocation := range []store.CLIInvocation{
		{OccurredAt: now - 3, SessionID: "codex-session", Agent: "codex", CommandPath: "atm todo list", Success: true, DurationMS: 4},
		{OccurredAt: now - 2, SessionID: "codex-session", Agent: "codex", CommandPath: "atm todo done", ExitCode: 1, ErrorCode: "forbidden", CauseClass: "authorization", Success: false, DurationMS: 5},
		{OccurredAt: now - 1, SessionID: "claude-session", Agent: "claude", CommandPath: "atm sync", ExitCode: 1, ErrorCode: "unavailable", CauseClass: "database", Success: false, DurationMS: 6},
	} {
		if err := store.RecordCLIInvocation(db, invocation); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	jsonOutput = true
	agentFlag = "codex"
	sessionToolsFailed = true
	var runErr error
	out := captureStdout(t, func() {
		runErr = runSessionTools(sessionToolsCmd, nil)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var payload struct {
		Total       int `json:"total"`
		Matched     int `json:"matched"`
		Returned    int `json:"returned"`
		Succeeded   int `json:"succeeded"`
		Failed      int `json:"failed"`
		Invocations []struct {
			CommandPath string `json:"command_path"`
			ErrorCode   string `json:"error_code"`
		} `json:"invocations"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal tools output: %v\n%s", err, out)
	}
	if payload.Total != 2 || payload.Matched != 1 || payload.Returned != 1 || payload.Succeeded != 1 || payload.Failed != 1 ||
		payload.Invocations[0].CommandPath != "atm todo done" || payload.Invocations[0].ErrorCode != "forbidden" {
		t.Fatalf("tools payload = %#v", payload)
	}
}
