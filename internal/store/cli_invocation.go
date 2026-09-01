package store

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/zane-byte-dev/atm/internal/config"
)

// CLIInvocation is the deliberately content-free audit record for one ATM CLI
// process. There is no argument, working-directory or error-message field here:
// command arguments and error strings can contain todo titles, search queries,
// file paths and credentials. Callers can observe which stable command failed
// and how, but never what content the command operated on.
type CLIInvocation struct {
	ID          int64  `json:"id"`
	OccurredAt  int64  `json:"occurred_at"`
	SessionID   string `json:"session_id,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Version     string `json:"version,omitempty"`
	CommandPath string `json:"command_path"`
	ExitCode    int    `json:"exit_code"`
	ErrorCode   string `json:"error_code,omitempty"`
	CauseClass  string `json:"cause_class,omitempty"`
	Retryable   bool   `json:"retryable"`
	DurationMS  int64  `json:"duration_ms"`
	Success     bool   `json:"success"`
}

type CLIInvocationQuery struct {
	SessionID string
	Agent     string
	SinceTS   int64
	UntilTS   int64
	Failed    bool
	Limit     int
	Offset    int
}

// CLIInvocationResult keeps the success denominator next to a filtered page.
// Total/Succeeded/Failed describe the full filtered set, not only the page.
type CLIInvocationResult struct {
	Invocations []CLIInvocation `json:"invocations"`
	Total       int             `json:"total"`
	Matched     int             `json:"matched"`
	Succeeded   int             `json:"succeeded"`
	Failed      int             `json:"failed"`
	Offset      int             `json:"offset"`
	Limit       int             `json:"limit"`
}

// RecordCLIInvocation writes a validated record through a caller-owned
// connection. Production callers normally use RecordCLIInvocationBestEffort;
// this strict form exists for migrations, services and deterministic tests.
func RecordCLIInvocation(db *sql.DB, invocation CLIInvocation) error {
	return recordCLIInvocation(context.Background(), db, invocation)
}

func recordCLIInvocation(ctx context.Context, db *sql.DB, invocation CLIInvocation) error {
	invocation = normalizeCLIInvocation(invocation)
	_, err := db.ExecContext(ctx, `INSERT INTO cli_invocations (
		occurred_at,session_id,agent,version,command_path,exit_code,error_code,
		cause_class,retryable,duration_ms,success
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		invocation.OccurredAt, invocation.SessionID, invocation.Agent, invocation.Version,
		invocation.CommandPath, invocation.ExitCode, invocation.ErrorCode,
		invocation.CauseClass, cliBoolInt(invocation.Retryable), invocation.DurationMS,
		cliBoolInt(invocation.Success),
	)
	return err
}

var recordingCLIInvocation atomic.Bool

const cliInvocationWriteBudget = 150 * time.Millisecond
const cliInvocationRetention = 90 * 24 * time.Hour

// RecordCLIInvocationBestEffort is the CLI process boundary. It intentionally
// does not create or migrate the database, waits only briefly for a concurrent
// SQLite writer, suppresses every error, and guards against accidental
// recursion. Telemetry must never change a command's result or turn a quick
// command into a multi-second busy wait.
func RecordCLIInvocationBestEffort(invocation CLIInvocation) {
	if !recordingCLIInvocation.CompareAndSwap(false, true) {
		return
	}
	defer recordingCLIInvocation.Store(false)

	if _, err := os.Stat(config.AtmDB); err != nil {
		return
	}
	dsn := (&url.URL{Scheme: "file", Path: config.AtmDB}).String() +
		"?mode=rw&_pragma=busy_timeout(100)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), cliInvocationWriteBudget)
	defer cancel()
	// Do not write an invocation into an unknown schema. In particular, an old
	// binary running beside a newer one must not invent a compatibility story.
	var version int
	if err := db.QueryRowContext(ctx, `SELECT version FROM schema_version LIMIT 1`).Scan(&version); err != nil || version != SchemaVersion {
		return
	}
	if err := recordCLIInvocation(ctx, db, invocation); err != nil {
		return
	}
	// Invocation telemetry is for recent reliability analysis, not permanent
	// activity surveillance. Keep a fixed rolling window; the timestamp index
	// makes the usually-empty cleanup cheap, and the same short context budget
	// keeps it fail-open under contention.
	_, _ = db.ExecContext(ctx, `DELETE FROM cli_invocations WHERE occurred_at < ?`,
		time.Now().Add(-cliInvocationRetention).Unix())
}

func QueryCLIInvocations(db *sql.DB, query CLIInvocationQuery) (CLIInvocationResult, error) {
	query.SessionID = strings.TrimSpace(query.SessionID)
	query.Agent = strings.TrimSpace(query.Agent)
	if query.Limit < 0 {
		query.Limit = 0
	}
	if query.Offset < 0 {
		query.Offset = 0
	}

	where := []string{"1=1"}
	args := make([]any, 0, 8)
	if query.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, query.SessionID)
	}
	if query.Agent != "" {
		where = append(where, "agent = ?")
		args = append(args, query.Agent)
	}
	if query.SinceTS > 0 {
		where = append(where, "occurred_at >= ?")
		args = append(args, query.SinceTS)
	}
	if query.UntilTS > 0 {
		where = append(where, "occurred_at < ?")
		args = append(args, query.UntilTS)
	}
	clause := strings.Join(where, " AND ")

	result := CLIInvocationResult{Offset: query.Offset, Limit: query.Limit}
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(success),0),COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0)
		FROM cli_invocations WHERE `+clause, args...).Scan(&result.Total, &result.Succeeded, &result.Failed); err != nil {
		return CLIInvocationResult{}, err
	}
	result.Matched = result.Total
	pageClause := clause
	if query.Failed {
		pageClause += " AND success = 0"
		result.Matched = result.Failed
	}

	pageQuery := `SELECT id,occurred_at,session_id,agent,version,command_path,exit_code,
		error_code,cause_class,retryable,duration_ms,success
		FROM cli_invocations WHERE ` + pageClause + ` ORDER BY occurred_at DESC,id DESC`
	pageArgs := append([]any(nil), args...)
	if query.Limit > 0 {
		pageQuery += " LIMIT ? OFFSET ?"
		pageArgs = append(pageArgs, query.Limit, query.Offset)
	} else if query.Offset > 0 {
		pageQuery += " LIMIT -1 OFFSET ?"
		pageArgs = append(pageArgs, query.Offset)
	}
	rows, err := db.Query(pageQuery, pageArgs...)
	if err != nil {
		return CLIInvocationResult{}, err
	}
	defer rows.Close()
	result.Invocations = make([]CLIInvocation, 0)
	for rows.Next() {
		var invocation CLIInvocation
		var retryable, success int
		if err := rows.Scan(
			&invocation.ID, &invocation.OccurredAt, &invocation.SessionID,
			&invocation.Agent, &invocation.Version, &invocation.CommandPath,
			&invocation.ExitCode, &invocation.ErrorCode, &invocation.CauseClass,
			&retryable, &invocation.DurationMS, &success,
		); err != nil {
			return CLIInvocationResult{}, err
		}
		invocation.Retryable = retryable != 0
		invocation.Success = success != 0
		result.Invocations = append(result.Invocations, invocation)
	}
	return result, rows.Err()
}

func normalizeCLIInvocation(invocation CLIInvocation) CLIInvocation {
	if invocation.OccurredAt <= 0 {
		invocation.OccurredAt = time.Now().Unix()
	}
	if invocation.DurationMS < 0 {
		invocation.DurationMS = 0
	}
	invocation.SessionID = safeInvocationIdentifier(invocation.SessionID, 256, false)
	invocation.Agent = safeInvocationIdentifier(invocation.Agent, 32, false)
	invocation.Version = safeInvocationIdentifier(invocation.Version, 64, true)
	invocation.CommandPath = safeInvocationCommandPath(invocation.CommandPath)
	invocation.ErrorCode = safeInvocationIdentifier(invocation.ErrorCode, 64, false)
	invocation.CauseClass = safeInvocationIdentifier(invocation.CauseClass, 64, false)
	if invocation.Success {
		invocation.ExitCode = 0
		invocation.ErrorCode = ""
		invocation.CauseClass = ""
		invocation.Retryable = false
	} else if invocation.ExitCode == 0 {
		invocation.ExitCode = 1
	}
	return invocation
}

func safeInvocationIdentifier(value string, limit int, version bool) string {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > limit {
		return ""
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.:", r) || (version && r == '+') {
			continue
		}
		return ""
	}
	return value
}

func safeInvocationCommandPath(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) == 0 || parts[0] != "atm" || len(parts) > 8 {
		return "atm"
	}
	for _, part := range parts[1:] {
		if safeInvocationIdentifier(part, 48, false) != part {
			return "atm"
		}
	}
	return strings.Join(parts, " ")
}

func cliBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
