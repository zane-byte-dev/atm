package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

// captureCommandStderr swaps os.Stderr for a pipe, the way captureStdout already
// does for stdout, because writeCommandError deliberately reports failures on
// stderr: a JSON consumer must be able to parse stdout without an error envelope
// landing in the middle of it.
func captureCommandStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		os.Stderr = old
		t.Fatalf("close stderr writer: %v", err)
	}
	os.Stderr = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(data)
}

// TestWriteCommandErrorMatchesRequestedShape covers why --json failures used to
// be undiagnosable: the flag only shaped success output, so an agent that asked
// for JSON still received one line of prose and could not read the code or the
// retryable flag that separate a permanent environment failure from a transient
// one. That is exactly how a stale binary came to be retried until the caller
// gave up and reported "unavailable, retrying did not help".
func TestWriteCommandErrorMatchesRequestedShape(t *testing.T) {
	old := jsonOutput
	t.Cleanup(func() { jsonOutput = old })

	// The cause stands in for driver detail that must never be serialized.
	cause := errors.New("sqlite: /Users/person/private/atm.db malformed")
	appErr := application.WrapError(
		application.CodeFailedPrecondition,
		"database schema v58 is newer than this atm build (v57): upgrade atm",
		cause,
	)
	appErr.Details = map[string]any{"database_schema": 58, "build_schema": 57}

	jsonOutput = false
	plain := captureCommandStderr(t, func() { writeCommandError(appErr) })
	if !strings.Contains(plain, "failed_precondition: database schema v58") {
		t.Fatalf("plain output = %q", plain)
	}
	if strings.Contains(strings.TrimSpace(plain), "{") {
		t.Fatalf("plain output emitted JSON: %q", plain)
	}

	jsonOutput = true
	var envelope struct {
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Details   map[string]any `json:"details"`
			Retryable bool           `json:"retryable"`
		} `json:"error"`
	}
	structured := captureCommandStderr(t, func() { writeCommandError(appErr) })
	if err := json.Unmarshal([]byte(structured), &envelope); err != nil {
		t.Fatalf("unmarshal %q: %v", structured, err)
	}
	if envelope.Error.Code != "failed_precondition" {
		t.Errorf("code = %q", envelope.Error.Code)
	}
	if envelope.Error.Retryable {
		t.Error("envelope reports a permanent failure as retryable")
	}
	if !strings.Contains(envelope.Error.Message, "upgrade atm") {
		t.Errorf("message lost the remedy: %q", envelope.Error.Message)
	}
	// JSON numbers decode as float64; the versions are the actionable detail.
	if envelope.Error.Details["database_schema"] != float64(58) ||
		envelope.Error.Details["build_schema"] != float64(57) {
		t.Errorf("details = %#v", envelope.Error.Details)
	}
	if strings.Contains(structured, "private") || strings.Contains(structured, "malformed") {
		t.Errorf("envelope leaked the cause: %s", structured)
	}
}

// TestWriteCommandErrorFallsBackToStableEnvelope keeps the JSON shape total: an
// error from outside the application vocabulary still produces a parseable
// envelope rather than bare text a machine caller would choke on.
func TestWriteCommandErrorFallsBackToStableEnvelope(t *testing.T) {
	old := jsonOutput
	t.Cleanup(func() { jsonOutput = old })
	jsonOutput = true

	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	// Wrapped, because the real path rarely hands over a bare application error:
	// withDB annotates before returning, and errors.As has to see through that.
	out := captureCommandStderr(t, func() {
		writeCommandError(fmt.Errorf("failed to open database: %w",
			application.NewError(application.CodeNotFound, "todo t9 was not found")))
	})
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if envelope.Error.Code != "not_found" || envelope.Error.Retryable {
		t.Fatalf("envelope = %#v", envelope.Error)
	}

	var fallback struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	uncoded := captureCommandStderr(t, func() { writeCommandError(errors.New("boom")) })
	if err := json.Unmarshal([]byte(uncoded), &fallback); err != nil {
		t.Fatalf("unmarshal %q: %v", uncoded, err)
	}
	if fallback.Error.Code != "internal" || fallback.Error.Message != "boom" {
		t.Fatalf("fallback envelope = %#v", fallback.Error)
	}
}
