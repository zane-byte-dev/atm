package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

// cliAgentEnvironment maps a launcher's environment variable to the agent it
// identifies, in precedence order. cliSessionEnvironment does the same for the
// session ID.
//
// Both are package-level tables rather than literals inside the lookups so that
// tests neutralizing ambient attribution can enumerate exactly what the lookups
// read. A hand-copied list in a test silently stops covering a key added here,
// and the test then passes or fails depending on which agent happens to be
// running `go test`.
var cliAgentEnvironment = []struct {
	environment string
	agent       string
}{
	{environment: "CODEX_THREAD_ID", agent: "codex"},
	{environment: "CLAUDE_CODE_SESSION_ID", agent: "claude"},
	{environment: "CLAUDECODE", agent: "claude"},
	{environment: "PI_SESSION_ID", agent: "pi"},
	{environment: "QWORK_SHIM_ROUTE", agent: "qoderwork"},
}

var cliSessionEnvironment = []string{
	"ATM_SESSION_ID",
	"CODEX_THREAD_ID",
	"CLAUDE_CODE_SESSION_ID",
	"PI_SESSION_ID",
}

// cliApplicationCall is the single provenance boundary for Cobra adapters.
// Command arguments never self-declare actor metadata; the adapter derives it
// consistently from the launching process. Ambient environment is still only
// best-effort attribution, not authentication: a service that protects a real
// human-presence boundary must use a stronger capability of its own.
func cliApplicationCall(scope, sessionID string) application.Call {
	agent := cliAgentFromEnvironment()
	kind := application.ActorHuman
	if agent != "" {
		kind = application.ActorAgent
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = cliSessionFromEnvironment()
	}
	return application.Call{
		RequestID: fmt.Sprintf("cli-%s-%d-%d", scope, os.Getpid(), time.Now().UnixNano()),
		Actor: application.Actor{
			Kind:      kind,
			Origin:    application.OriginCLI,
			SessionID: strings.TrimSpace(sessionID),
			Agent:     agent,
		},
	}
}

// cliAttributionEnvironment lists every variable cliApplicationCall consults,
// deduplicated in the order the lookups reach them.
func cliAttributionEnvironment() []string {
	keys := make([]string, 0, len(cliSessionEnvironment)+len(cliAgentEnvironment))
	seen := make(map[string]bool, cap(keys))
	add := func(key string) {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for _, key := range cliSessionEnvironment {
		add(key)
	}
	for _, candidate := range cliAgentEnvironment {
		add(candidate.environment)
	}
	return keys
}

func cliSessionFromEnvironment() string {
	for _, key := range cliSessionEnvironment {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func cliAgentFromEnvironment() string {
	for _, candidate := range cliAgentEnvironment {
		if strings.TrimSpace(os.Getenv(candidate.environment)) != "" {
			return candidate.agent
		}
	}
	return ""
}
