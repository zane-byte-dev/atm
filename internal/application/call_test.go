package application

import (
	"errors"
	"testing"
)

func TestActorKindAndOriginValidity(t *testing.T) {
	for _, kind := range []ActorKind{ActorHuman, ActorAgent, ActorController} {
		if !kind.Valid() {
			t.Errorf("ActorKind(%q).Valid() = false", kind)
		}
	}
	if ActorKind("robot").Valid() {
		t.Error("unknown actor kind is valid")
	}

	for _, origin := range []Origin{OriginCLI, OriginIPC, OriginController, OriginHook} {
		if !origin.Valid() {
			t.Errorf("Origin(%q).Valid() = false", origin)
		}
	}
	if Origin("http").Valid() {
		t.Error("unknown origin is valid")
	}
}

func TestCallValidateAcceptsKnownShapeWithoutUseCasePolicy(t *testing.T) {
	call := Call{
		RequestID: "req-42",
		Actor: Actor{
			Kind:      ActorAgent,
			Origin:    OriginHook,
			SessionID: "session-1",
			BindingID: 7,
			RunID:     "run-2",
			Agent:     "codex",
		},
	}
	if err := call.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCallValidateReturnsTypedFieldErrors(t *testing.T) {
	tests := []struct {
		name  string
		call  Call
		field string
	}{
		{
			name:  "request id",
			call:  Call{Actor: Actor{Kind: ActorHuman, Origin: OriginIPC}},
			field: "request_id",
		},
		{
			name:  "actor kind",
			call:  Call{RequestID: "req-1", Actor: Actor{Kind: "robot", Origin: OriginCLI}},
			field: "actor.kind",
		},
		{
			name:  "origin",
			call:  Call{RequestID: "req-1", Actor: Actor{Kind: ActorHuman, Origin: "http"}},
			field: "actor.origin",
		},
		{
			name: "negative binding id",
			call: Call{RequestID: "req-1", Actor: Actor{
				Kind: ActorController, Origin: OriginController, BindingID: -1,
			}},
			field: "actor.binding_id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call.Validate()
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("Validate() error = %v, want invalid_argument", err)
			}
			var appErr *Error
			if !errors.As(err, &appErr) {
				t.Fatalf("Validate() error type = %T, want *Error", err)
			}
			if got := appErr.Details["field"]; got != test.field {
				t.Errorf("details.field = %v, want %q", got, test.field)
			}
		})
	}
}
