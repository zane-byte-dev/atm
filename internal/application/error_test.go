package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorCodesAreStableAndValid(t *testing.T) {
	tests := map[ErrorCode]string{
		CodeInvalidArgument: "invalid_argument",
		CodeNotFound:        "not_found",
		CodeConflict:        "conflict",
		CodeForbidden:       "forbidden",
		CodeBusy:            "busy",
		CodeUnavailable:     "unavailable",
		CodeInternal:        "internal",
	}
	for code, want := range tests {
		if !code.Valid() {
			t.Errorf("ErrorCode(%q).Valid() = false", code)
		}
		if string(code) != want {
			t.Errorf("ErrorCode = %q, want %q", code, want)
		}
	}
	if ErrorCode("timeout").Valid() {
		t.Error("unknown error code is valid")
	}
}

func TestErrorSupportsIsAsAndCause(t *testing.T) {
	cause := errors.New("sqlite busy")
	appErr := WrapError(CodeBusy, "work state is being updated", cause)
	appErr.Details = map[string]any{"wait_ms": 250}
	appErr.Retryable = true
	err := fmt.Errorf("submit todo: %w", appErr)

	if !errors.Is(err, ErrBusy) {
		t.Fatal("wrapped Error does not match ErrBusy")
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatal("busy Error unexpectedly matches ErrUnavailable")
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped Error does not retain its cause")
	}

	var got *Error
	if !errors.As(err, &got) {
		t.Fatalf("errors.As(%T) failed", err)
	}
	if got.Code != CodeBusy || !got.Retryable || got.Details["wait_ms"] != 250 {
		t.Errorf("errors.As result = %#v", got)
	}
}

func TestErrorsWithSameCodeMatch(t *testing.T) {
	left := NewError(CodeConflict, "base revision is stale")
	right := NewError(CodeConflict, "different wording")
	if !errors.Is(left, right) || !errors.Is(right, left) {
		t.Fatal("errors with the same code do not match")
	}
}

func TestErrorJSONOmitsCauseAndKeepsProtocolFields(t *testing.T) {
	err := WrapError(CodeConflict, "base revision is stale", errors.New("private database detail"))
	err.Details = map[string]any{"current_revision": 4}

	raw, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	encoded := string(raw)
	for _, want := range []string{
		`"code":"conflict"`,
		`"message":"base revision is stale"`,
		`"current_revision":4`,
		`"retryable":false`,
	} {
		if !strings.Contains(encoded, want) {
			t.Errorf("JSON %s does not contain %s", encoded, want)
		}
	}
	if strings.Contains(encoded, "private database detail") || strings.Contains(encoded, "Cause") {
		t.Errorf("JSON exposes cause: %s", encoded)
	}
}

func TestErrorStringHandlesPartialAndNilValues(t *testing.T) {
	tests := []struct {
		err  *Error
		want string
	}{
		{err: NewError(CodeNotFound, "todo t9 was not found"), want: "not_found: todo t9 was not found"},
		{err: NewError(CodeNotFound, ""), want: "not_found"},
		{err: &Error{Message: "plain failure"}, want: "plain failure"},
		{err: nil, want: "<nil>"},
	}
	for _, test := range tests {
		if got := test.err.Error(); got != test.want {
			t.Errorf("Error() = %q, want %q", got, test.want)
		}
	}
}
