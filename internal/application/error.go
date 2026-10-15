package application

// ErrorCode is the stable, machine-readable class of an application failure.
// Adapters may change human wording without changing these values.
type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "conflict"
	CodeForbidden       ErrorCode = "forbidden"
	CodeBusy            ErrorCode = "busy"
	CodeUnavailable     ErrorCode = "unavailable"
	// CodeFailedPrecondition reports an environment that cannot satisfy the
	// operation, such as a binary older than the database it opened. It is
	// separate from CodeUnavailable because unavailable means "transient, retry
	// later" while a failed precondition never succeeds on retry: only changing
	// the environment does. Collapsing the two tells an agent to retry forever.
	CodeFailedPrecondition ErrorCode = "failed_precondition"
	CodeInternal           ErrorCode = "internal"
)

// AllErrorCodes returns the public error vocabulary in declaration order. It is
// the single source of truth for Valid, and it exists so adapters can prove
// their own mapping is total: a transport that switches on codes and silently
// defaults would otherwise turn a newly added code into a generic failure
// instead of its real meaning.
func AllErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeInvalidArgument,
		CodeNotFound,
		CodeConflict,
		CodeForbidden,
		CodeBusy,
		CodeUnavailable,
		CodeFailedPrecondition,
		CodeInternal,
	}
}

// Valid reports whether code is part of the public application error
// vocabulary.
func (code ErrorCode) Valid() bool {
	for _, candidate := range AllErrorCodes() {
		if code == candidate {
			return true
		}
	}
	return false
}

// Error is a transport-independent application failure. Code is stable and is
// intended for branching; Message is human-facing context; Details carries
// structured diagnostics such as a field name or current revision. Cause is
// retained for logs and errors.Is/errors.As but is never serialized.
type Error struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Retryable bool           `json:"retryable"`
	Cause     error          `json:"-"`
}

// NewError constructs an application error without an underlying cause.
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WrapError constructs an application error while retaining the underlying
// cause for errors.Is and errors.As.
func WrapError(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Code == "" {
		return err.Message
	}
	if err.Message == "" {
		return string(err.Code)
	}
	return string(err.Code) + ": " + err.Message
}

// Unwrap exposes the original infrastructure or domain failure without making
// it part of the serialized application contract.
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Is makes application error categories usable as sentinels through arbitrary
// wrapping. Errors with the same stable code match even when their messages,
// details, or causes differ.
func (err *Error) Is(target error) bool {
	if err == nil || err.Code == "" {
		return false
	}
	coded, ok := target.(interface{ applicationErrorCode() ErrorCode })
	return ok && err.Code == coded.applicationErrorCode()
}

func (err *Error) applicationErrorCode() ErrorCode {
	if err == nil {
		return ""
	}
	return err.Code
}

type codeSentinel ErrorCode

func (sentinel codeSentinel) Error() string {
	return string(sentinel)
}

func (sentinel codeSentinel) applicationErrorCode() ErrorCode {
	return ErrorCode(sentinel)
}

func (sentinel codeSentinel) Is(target error) bool {
	coded, ok := target.(interface{ applicationErrorCode() ErrorCode })
	return ok && ErrorCode(sentinel) == coded.applicationErrorCode()
}

// Stable category sentinels support errors.Is without discarding a concrete
// *Error's message, details, retryability, or cause.
var (
	ErrInvalidArgument    error = codeSentinel(CodeInvalidArgument)
	ErrNotFound           error = codeSentinel(CodeNotFound)
	ErrConflict           error = codeSentinel(CodeConflict)
	ErrForbidden          error = codeSentinel(CodeForbidden)
	ErrBusy               error = codeSentinel(CodeBusy)
	ErrUnavailable        error = codeSentinel(CodeUnavailable)
	ErrFailedPrecondition error = codeSentinel(CodeFailedPrecondition)
	ErrInternal           error = codeSentinel(CodeInternal)
)
