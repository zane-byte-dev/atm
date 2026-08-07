package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func withTempDataDir(t *testing.T) string {
	t.Helper()
	old := config.AtmDir
	dir := t.TempDir()
	config.AtmDir = dir
	t.Cleanup(func() { config.AtmDir = old })
	return dir
}

func readEntries(t *testing.T) []entry {
	t.Helper()
	lines, err := Tail(Path(), 0)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	var out []entry
	for _, line := range lines {
		var record entry
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON (%q): %v", line, err)
		}
		out = append(out, record)
	}
	return out
}

func TestFailureRecordsAReadableEntry(t *testing.T) {
	withTempDataDir(t)
	Failure("command_failed", "atm sync", fmt.Errorf("database is locked"), map[string]any{"attempt": 2})

	entries := readEntries(t)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	record := entries[0]
	if record.Level != "error" || record.Event != "command_failed" {
		t.Errorf("level/event = %q/%q", record.Level, record.Event)
	}
	if record.Command != "atm sync" || record.Error != "database is locked" {
		t.Errorf("command/error = %q/%q", record.Command, record.Error)
	}
	if record.Time == "" {
		t.Error("entry has no timestamp, so it cannot be correlated with anything")
	}
	if record.Fields["attempt"] != float64(2) {
		t.Errorf("fields = %v", record.Fields)
	}
}

// A log that cannot be written must not become the caller's problem: every call
// site is on a path that was already failing, or on a hot refresh loop.
func TestWriteNeverPanicsOnAnUnwritableDirectory(t *testing.T) {
	old := config.AtmDir
	// A path under a regular file can never be created.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	config.AtmDir = filepath.Join(blocker, "nested")
	t.Cleanup(func() { config.AtmDir = old })

	Failure("command_failed", "atm sync", fmt.Errorf("boom"), nil)
	Lifecycle("app_started", nil)
	// Reaching here without a panic is the assertion.
}

func TestRotationKeepsTheCapAndOnePreviousFile(t *testing.T) {
	withTempDataDir(t)
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Seed a file already at the cap so the next write has to rotate.
	if err := os.WriteFile(Path(), []byte(strings.Repeat("x", maxBytes)), 0600); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	Failure("command_failed", "atm sync", fmt.Errorf("after rotation"), nil)

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat current log: %v", err)
	}
	if info.Size() >= maxBytes {
		t.Errorf("current log is %d bytes; rotation did not happen before the write", info.Size())
	}
	if _, err := os.Stat(Path() + ".1"); err != nil {
		t.Errorf("previous log was not kept: %v", err)
	}
	entries := readEntries(t)
	if len(entries) != 1 || entries[0].Error != "after rotation" {
		t.Errorf("rotated log does not start with the new entry: %v", entries)
	}
}

func TestRotationDoesNotAccumulateFilesForever(t *testing.T) {
	withTempDataDir(t)
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for round := 0; round < 3; round++ {
		if err := os.WriteFile(Path(), []byte(strings.Repeat("x", maxBytes)), 0600); err != nil {
			t.Fatalf("seed log: %v", err)
		}
		Failure("command_failed", "atm sync", fmt.Errorf("round %d", round), nil)
	}
	matches, err := filepath.Glob(filepath.Join(Dir(), "cli.log*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	// cli.log plus exactly one rotation.
	if len(matches) != 2 {
		t.Errorf("log files = %v, want the current file and one rotation", matches)
	}
}

func TestTailReturnsTheLastLinesOldestFirst(t *testing.T) {
	withTempDataDir(t)
	for index := 0; index < 5; index++ {
		Failure("command_failed", "atm sync", fmt.Errorf("error %d", index), nil)
	}
	lines, err := Tail(Path(), 2)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "error 3") || !strings.Contains(lines[1], "error 4") {
		t.Errorf("tail is not the last two in order: %v", lines)
	}
}

// No log file is the normal state — nothing has failed.
func TestTailOnMissingFileIsNotAnError(t *testing.T) {
	withTempDataDir(t)
	lines, err := Tail(Path(), 10)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines = %v, want none", lines)
	}
}

// Every line is one JSON object. `atm diagnose --bundle` embeds these verbatim,
// so a malformed line would corrupt the bundle it is meant to help debug.
func TestEveryLineIsIndependentlyParseable(t *testing.T) {
	withTempDataDir(t)
	Failure("command_failed", "atm todo add", fmt.Errorf("multi\nline\nerror"), nil)
	Lifecycle("app_started", map[string]any{"version": "1.2.3"})

	lines, err := Tail(Path(), 0)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("a multi-line error broke the one-object-per-line rule: %d lines", len(lines))
	}
	for _, line := range lines {
		var probe map[string]any
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Errorf("line is not valid JSON: %q", line)
		}
	}
}

// The leak this closes: `atm todo add --batch` wraps its failures as
// `item %q: ...`, root.Execute logs that error verbatim, and `atm diagnose
// --bundle` embeds the log into a file the user attaches to a bug report. The
// title never chose to be there.
func TestFailureKeepsQuotedContentOutOfTheLog(t *testing.T) {
	withTempDataDir(t)
	title := "把 ACL 密钥换成 sifei 给的那把"
	Failure("command_failed", "atm todo add",
		fmt.Errorf("item %q: waiting todos require wake or review_at", title), nil)

	entries := readEntries(t)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if strings.Contains(entries[0].Error, title) {
		t.Fatalf("the todo title reached the log: %q", entries[0].Error)
	}
	// The sentence still has to say what went wrong, or redaction has traded one
	// unusable log for another.
	if !strings.Contains(entries[0].Error, "waiting todos require wake or review_at") {
		t.Fatalf("redaction ate the diagnosis: %q", entries[0].Error)
	}
}

func TestRedactQuoted(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"nothing quoted is left alone", "database is locked", "database is locked"},
		{"a quoted value goes", `item "secret": bad`, `item "…": bad`},
		{"every span goes", `"a" and "b"`, `"…" and "…"`},
		{
			// %q escapes an embedded quote, so the span does not end at it.
			"an escaped quote does not end the span",
			`item "he said \"hi\" loudly": bad`,
			`item "…": bad`,
		},
		{"an empty value is not content", `project "": missing`, `project "": missing`},
		{
			// Truncated by a log cap, or by an error that quotes badly. Dropping
			// the tail is the safe reading.
			"an unterminated quote redacts to the end",
			`item "half a tit`,
			`item "…"`,
		},
		{"a quote as the last byte", `item "`, `item "…"`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := RedactQuoted(testCase.in); got != testCase.want {
				t.Errorf("RedactQuoted(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}
