package ipc

import (
	"errors"
	"fmt"

	"github.com/zane-byte-dev/atm/internal/application"
)

// ErrorCode is the stable, machine-readable part of an IPC failure. Messages
// are for people and may become more specific; callers branch on Code instead.
type ErrorCode string

const (
	CodeInvalidRequest  ErrorCode = "invalid_request"
	CodeMethodNotFound  ErrorCode = "method_not_found"
	CodeInvalidArgument ErrorCode = ErrorCode(application.CodeInvalidArgument)
	CodeNotFound        ErrorCode = ErrorCode(application.CodeNotFound)
	CodeConflict        ErrorCode = ErrorCode(application.CodeConflict)
	CodeForbidden       ErrorCode = ErrorCode(application.CodeForbidden)
	CodeBusy            ErrorCode = ErrorCode(application.CodeBusy)
	CodeUnavailable     ErrorCode = ErrorCode(application.CodeUnavailable)
	CodeInternal        ErrorCode = ErrorCode(application.CodeInternal)
)

// ErrorPayload is the wire representation of an application failure.
//
// Details is deliberately optional and structured. Stable fields such as a
// rejected parameter or current revision belong there; stack traces, command
// lines and secrets do not.
type ErrorPayload struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Details   any       `json:"details,omitempty"`
	Retryable bool      `json:"retryable"`
}

// transportError covers failures before an application use case is reached,
// such as an unknown method. Application failures use application.Error, so
// this package does not grow a second business-error vocabulary.
type transportError struct {
	payload ErrorPayload
	cause   error
}

func (fault *transportError) Error() string {
	if fault.cause != nil {
		return fault.cause.Error()
	}
	return fault.payload.Message
}

func (fault *transportError) Unwrap() error { return fault.cause }

func newTransportError(code ErrorCode, message string, details any, retryable bool, cause error) error {
	if message == "" && cause != nil {
		message = cause.Error()
	}
	return &transportError{
		payload: ErrorPayload{
			Code:      code,
			Message:   message,
			Details:   details,
			Retryable: retryable,
		},
		cause: cause,
	}
}

func InvalidArgument(err error) error {
	if err == nil {
		return application.NewError(application.CodeInvalidArgument, "invalid IPC argument")
	}
	return application.WrapError(application.CodeInvalidArgument, err.Error(), err)
}

func faultPayload(err error) ErrorPayload {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		code := CodeInternal
		if appErr.Code.Valid() {
			code = ErrorCode(appErr.Code)
		}
		message := appErr.Message
		if message == "" {
			message = string(code)
		}
		return ErrorPayload{
			Code:      code,
			Message:   message,
			Details:   appErr.Details,
			Retryable: appErr.Retryable,
		}
	}
	var fault *transportError
	if errors.As(err, &fault) {
		return fault.payload
	}
	// An unclassified error is an implementation failure, not a wire message.
	// err.Error() may contain filesystem paths, command lines, provider payloads
	// or credentials. The request ID in the envelope is the correlation handle;
	// services that need a specific user-facing failure must return a typed
	// application.Error with an intentionally safe Message.
	return ErrorPayload{Code: CodeInternal, Message: "internal IPC error", Retryable: false}
}

func methodNotFound(name string, known []string) error {
	return newTransportError(
		CodeMethodNotFound,
		fmt.Sprintf("unknown ipc verb: %s (known: %s)", name, joinNames(known)),
		map[string]any{"method": name, "known": known},
		false,
		nil,
	)
}
