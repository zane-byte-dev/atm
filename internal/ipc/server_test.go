package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

type greetingRequest struct {
	Name string `json:"name"`
}

type greetingResponse struct {
	Greeting string `json:"greeting"`
}

func testServer(t *testing.T, registry *Registry) *Server {
	t.Helper()
	return NewServer(7, registry, WithRequestIDGenerator(func() string { return "request-123" }))
}

func TestTypedBindDecodesRequestAndWrapsResponse(t *testing.T) {
	registry := NewRegistry()
	MustBind(registry, "example.greet", func(
		_ context.Context,
		call application.Call,
		request greetingRequest,
	) (greetingResponse, error) {
		if call.RequestID != "request-123" ||
			call.Actor.Kind != application.ActorHuman ||
			call.Actor.Origin != application.OriginIPC {
			t.Fatalf("call = %+v", call)
		}
		return greetingResponse{Greeting: "hello " + request.Name}, nil
	})

	var output bytes.Buffer
	if err := testServer(t, registry).Serve(
		context.Background(),
		"example.greet",
		strings.NewReader(`{"name":"ATM"}`),
		&output,
	); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var envelope struct {
		EnvelopeVersion int              `json:"envelope_version"`
		ProtocolVersion int              `json:"protocol_version"`
		RequestID       string           `json:"request_id"`
		Verb            string           `json:"verb"`
		Data            greetingResponse `json:"data"`
		Error           *ErrorPayload    `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output.String())
	}
	if envelope.EnvelopeVersion != EnvelopeVersion || envelope.ProtocolVersion != 7 {
		t.Fatalf("versions = envelope:%d protocol:%d", envelope.EnvelopeVersion, envelope.ProtocolVersion)
	}
	if envelope.RequestID != "request-123" || envelope.Verb != "example.greet" {
		t.Fatalf("identity = request:%q verb:%q", envelope.RequestID, envelope.Verb)
	}
	if envelope.Data.Greeting != "hello ATM" || envelope.Error != nil {
		t.Fatalf("response = %+v, error = %+v", envelope.Data, envelope.Error)
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("no-request handler read stdin") }

func TestNoRequestHandlerDoesNotReadStdin(t *testing.T) {
	registry := NewRegistry()
	MustBindNoRequest(registry, "example.snapshot", func(context.Context, application.Call) (greetingResponse, error) {
		return greetingResponse{Greeting: "ready"}, nil
	})
	if err := testServer(t, registry).Serve(
		context.Background(), "example.snapshot", panicReader{}, io.Discard,
	); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestInvalidRequestReturnsStableErrorEnvelope(t *testing.T) {
	registry := NewRegistry()
	MustBind(registry, "example.greet", func(
		_ context.Context,
		_ application.Call,
		request greetingRequest,
	) (greetingResponse, error) {
		return greetingResponse{Greeting: request.Name}, nil
	})

	var output bytes.Buffer
	err := testServer(t, registry).Serve(
		context.Background(),
		"example.greet",
		strings.NewReader(`{"name":"ATM","unexpected":true}`),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field", err)
	}
	var envelope Envelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v\n%s", decodeErr, output.String())
	}
	if envelope.RequestID != "request-123" || envelope.Error == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Error.Code != CodeInvalidArgument || envelope.Error.Retryable {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
	if envelope.Data != nil {
		t.Fatalf("error envelope unexpectedly has data: %+v", envelope.Data)
	}
}

func TestServiceFaultKeepsMachineCodeAndCause(t *testing.T) {
	registry := NewRegistry()
	cause := errors.New("revision changed")
	MustBindNoRequest(registry, "example.update", func(context.Context, application.Call) (greetingResponse, error) {
		appErr := application.WrapError(application.CodeConflict, "the value changed", cause)
		appErr.Details = map[string]any{"current_revision": 4}
		appErr.Retryable = true
		return greetingResponse{}, appErr
	})

	var output bytes.Buffer
	err := testServer(t, registry).Serve(context.Background(), "example.update", nil, &output)
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped cause", err)
	}
	var envelope Envelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v", decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != CodeConflict || !envelope.Error.Retryable {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
	details, ok := envelope.Error.Details.(map[string]any)
	if !ok || details["current_revision"] != float64(4) {
		t.Fatalf("error details = %#v", envelope.Error.Details)
	}
}

func TestUnknownFaultDoesNotLeakItsMessage(t *testing.T) {
	registry := NewRegistry()
	MustBindNoRequest(registry, "example.secret-failure", func(context.Context, application.Call) (greetingResponse, error) {
		return greetingResponse{}, errors.New("open /private/path/credentials.json: token=secret-value")
	})

	var output bytes.Buffer
	err := testServer(t, registry).Serve(context.Background(), "example.secret-failure", nil, &output)
	if err == nil || !strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("returned Go error = %v, want original cause for local diagnostics", err)
	}
	var envelope Envelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v", decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != CodeInternal || envelope.Error.Message != "internal IPC error" {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
	if strings.Contains(output.String(), "private/path") || strings.Contains(output.String(), "secret-value") {
		t.Fatalf("wire response leaked internal failure: %s", output.String())
	}
}

func TestServeCallPreservesTrustedCall(t *testing.T) {
	registry := NewRegistry()
	want := application.Call{
		RequestID: "client-request-9",
		Actor: application.Actor{
			Kind:      application.ActorAgent,
			Origin:    application.OriginHook,
			SessionID: "session-4",
		},
	}
	MustBindNoRequest(registry, "example.identity", func(
		_ context.Context,
		call application.Call,
	) (application.Call, error) {
		return call, nil
	})

	var output bytes.Buffer
	if err := testServer(t, registry).ServeCall(
		context.Background(), want, "example.identity", nil, &output,
	); err != nil {
		t.Fatalf("ServeCall: %v", err)
	}
	var envelope struct {
		RequestID string           `json:"request_id"`
		Data      application.Call `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.RequestID != want.RequestID || envelope.Data != want {
		t.Fatalf("envelope identity = request:%q call:%+v", envelope.RequestID, envelope.Data)
	}
}

func TestUnknownMethodListsRegistryInErrorEnvelope(t *testing.T) {
	registry := NewRegistry()
	MustBindNoRequest(registry, "z.last", func(context.Context, application.Call) (struct{}, error) {
		return struct{}{}, nil
	})
	MustBindNoRequest(registry, "a.first", func(context.Context, application.Call) (struct{}, error) {
		return struct{}{}, nil
	})

	var output bytes.Buffer
	err := testServer(t, registry).Serve(context.Background(), "missing", nil, &output)
	if err == nil || !strings.Contains(err.Error(), "a.first, z.last") {
		t.Fatalf("error = %v", err)
	}
	var envelope Envelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode envelope: %v", decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != CodeMethodNotFound {
		t.Fatalf("error payload = %+v", envelope.Error)
	}
}

func TestBindRejectsDuplicateMethod(t *testing.T) {
	registry := NewRegistry()
	handler := func(context.Context, application.Call) (struct{}, error) { return struct{}{}, nil }
	if err := BindNoRequest(registry, "example.same", handler); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if err := BindNoRequest(registry, "example.same", handler); err == nil {
		t.Fatal("duplicate method was accepted")
	}
}

func TestTypedBindRejectsTrailingJSONValue(t *testing.T) {
	registry := NewRegistry()
	MustBind(registry, "example.greet", func(
		_ context.Context,
		_ application.Call,
		request greetingRequest,
	) (greetingResponse, error) {
		return greetingResponse{Greeting: request.Name}, nil
	})
	err := testServer(t, registry).Serve(
		context.Background(),
		"example.greet",
		strings.NewReader(`{"name":"ATM"} {"name":"again"}`),
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "more than one JSON value") {
		t.Fatalf("error = %v", err)
	}
}
