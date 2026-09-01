package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func resolveSessionID(required bool) (string, error) {
	for _, value := range []string{
		sessionIDFlag,
		os.Getenv("ATM_SESSION_ID"),
		os.Getenv("CODEX_THREAD_ID"),
		os.Getenv("CLAUDE_CODE_SESSION_ID"),
		os.Getenv("PI_SESSION_ID"),
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value, nil
		}
	}
	if required {
		return "", fmt.Errorf("current session ID unavailable; pass --agent-session or set ATM_SESSION_ID")
	}
	return "", nil
}

func resolveCurrentTodoID() (string, error) {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return "", err
	}
	result, err := workapp.Default.Current(
		context.Background(), cliApplicationCall("session-resolve-current", sessionID), workapp.CurrentInput{},
	)
	if err != nil {
		return "", err
	}
	if result.Context == nil {
		return "", fmt.Errorf("no todo bound to current session; run `atm todo match --prompt` then `atm session bind <id>`")
	}
	return result.Context.Binding.TodoID, nil
}

func optionalTodoID(args []string) (string, error) {
	if len(args) > 0 && args[0] != "current" {
		return args[0], nil
	}
	return resolveCurrentTodoID()
}

func normalizeBindingAgent(value string) string {
	if agent := config.NormalizeAgent(strings.TrimSpace(value)); agent != "" {
		return agent
	}
	if os.Getenv("CODEX_THREAD_ID") != "" {
		return "codex"
	}
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return "claude"
	}
	if os.Getenv("PI_SESSION_ID") != "" {
		return "pi"
	}
	return ""
}

// sessionBindingCLICall remains for the live-status compatibility adapter. New
// Work command adapters call cliApplicationCall directly.
func sessionBindingCLICall(operation, sessionID string) application.Call {
	return cliApplicationCall("session-"+operation, sessionID)
}

func shortSessionID(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

// todoSourceFromSession labels a todo with the agent session that filed it.
func todoSourceFromSession() string {
	sid := os.Getenv("CLAUDE_CODE_SESSION_ID")
	if sid == "" {
		return ""
	}
	return "session:" + shortSessionID(sid)
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func cleanBindingCWD(value string) string {
	if value == "" {
		return ""
	}
	cleaned, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return cleaned
}
