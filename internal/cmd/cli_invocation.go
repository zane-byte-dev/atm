package cmd

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

var cliCommandEnteredRun atomic.Bool

func recordCLIInvocation(started time.Time, commandErr error, exitCode int) {
	store.RecordCLIInvocationBestEffort(cliInvocationForExecution(started, commandErr, exitCode))
}

func cliInvocationForExecution(started time.Time, commandErr error, exitCode int) store.CLIInvocation {
	duration := time.Since(started)
	if duration < 0 {
		duration = 0
	}
	sessionID, _ := resolveSessionID(false)
	invocation := store.CLIInvocation{
		OccurredAt:  started.Unix(),
		SessionID:   sessionID,
		Agent:       cliAgentFromEnvironment(),
		Version:     rootCmd.Version,
		CommandPath: failedCommandPath(),
		ExitCode:    exitCode,
		DurationMS:  duration.Milliseconds(),
		Success:     commandErr == nil,
	}
	if commandErr != nil {
		if !cliCommandEnteredRun.Load() {
			invocation.ErrorCode = "invalid_invocation"
			invocation.CauseClass = "command_contract"
		} else {
			invocation.ErrorCode, invocation.CauseClass, invocation.Retryable = classifyCLIInvocationError(commandErr)
		}
	}
	return invocation
}

// classifyCLIInvocationError reduces a concrete error to a closed diagnostic
// vocabulary. It never copies err.Error(): application messages, os.PathError
// paths and child-process stderr can all contain user content or credentials.
func classifyCLIInvocationError(err error) (errorCode, causeClass string, retryable bool) {
	var coded exitError
	if errors.As(err, &coded) {
		switch coded.ExitCode() {
		case guardExitBlocked:
			return "guard_blocked", "guard", false
		case guardExitPending:
			return "guard_pending", "guard", true
		case guardExitDenied:
			return "guard_denied", "guard", false
		default:
			return "child_exit", "child_process", false
		}
	}

	var appErr *application.Error
	if errors.As(err, &appErr) {
		cause := applicationCauseClass(appErr.Code)
		if underlying := infrastructureCauseClass(appErr.Cause); underlying != "" &&
			(appErr.Code == application.CodeUnavailable || appErr.Code == application.CodeInternal) {
			cause = underlying
		}
		return string(appErr.Code), cause, appErr.Retryable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded", "timeout", true
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", "cancellation", true
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission_denied", "filesystem", false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
		return "not_found", "resource", false
	}
	if cause := infrastructureCauseClass(err); cause != "" {
		return "unavailable", cause, cause == "database" || cause == "network"
	}
	return "command_failed", "command", false
}

func applicationCauseClass(code application.ErrorCode) string {
	switch code {
	case application.CodeInvalidArgument:
		return "validation"
	case application.CodeNotFound:
		return "resource"
	case application.CodeConflict:
		return "state_conflict"
	case application.CodeForbidden:
		return "authorization"
	case application.CodeBusy:
		return "contention"
	case application.CodeUnavailable:
		return "infrastructure"
	default:
		return "internal"
	}
}

func infrastructureCauseClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancellation"
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		return "filesystem"
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return "network"
	}
	// modernc SQLite errors expose Code(). Keeping this as a tiny interface
	// avoids coupling telemetry classification to driver-specific constants.
	var databaseError interface{ Code() int }
	if errors.As(err, &databaseError) {
		return "database"
	}
	return ""
}

func invocationCommandPath(args []string) string {
	command, _, err := rootCmd.Find(args)
	if err != nil || command == nil || command == rootCmd {
		// Cobra's Find stops at a root persistent flag placed before the command
		// (`atm --json session list`). Remove only the known root flags and their
		// values, then resolve again. Values are used transiently for navigation
		// and never copied into the invocation record.
		commandArgs := make([]string, 0, len(args))
		for index := 0; index < len(args); index++ {
			argument := args[index]
			if argument == "--" {
				break
			}
			if strings.HasPrefix(argument, "-") {
				name := strings.TrimLeft(strings.SplitN(argument, "=", 2)[0], "-")
				flag := rootCmd.PersistentFlags().Lookup(name)
				if flag != nil && !strings.Contains(argument, "=") && flag.NoOptDefVal == "" && index+1 < len(args) {
					index++
				}
				continue
			}
			commandArgs = append(commandArgs, argument)
		}
		command, _, err = rootCmd.Find(commandArgs)
		if err != nil || command == nil {
			return "atm"
		}
	}
	path := strings.TrimSpace(command.CommandPath())
	if path == "" {
		return "atm"
	}
	return path
}
