// Package logging records what went wrong on disk, so a failure that happened
// yesterday can still be diagnosed today.
//
// Before this, a CLI error reached stderr and died with the process, and the App
// had one NSLog line in the whole codebase. That is fine while someone is
// watching a terminal and useless for everything else ATM does: the App refreshes
// on a timer, collection runs in the background, and hooks run unattended. Those
// failures were invisible unless they happened to be on screen at the time, and
// an intermittent one — failing once a day, fine otherwise — looked identical to
// no failure at all.
//
// Three rules the format and the call sites both depend on:
//
//   - Failures only. A log that records successful work fills with noise, ages
//     out the one line that mattered, and nobody reads it. Lifecycle events are
//     the exception, because "did the last run exit cleanly" cannot be answered
//     without them.
//   - No content, ever. Not session text, not todo/memory/knowledge bodies, not
//     credentials, not command arguments that could carry any of them. The log is
//     collected by `atm diagnose --bundle` and attached to public bug reports; it
//     holds what failed and where, never what the user was working on.
//   - Never fail the caller. A log that cannot be written is a worse outcome than
//     no log, but it is a far better outcome than a command that refuses to run
//     because its log directory is read-only.
package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

const (
	// maxBytes caps one log file. Small on purpose: this is a tail of recent
	// failures, not an archive, and it has to be small enough to attach to a bug
	// report without anyone thinking twice.
	maxBytes = 5 << 20
	// keptRotations is how many previous files survive. One is enough to cover a
	// rotation that happens between a failure and someone noticing it.
	keptRotations = 1
)

// Dir is where both the CLI and the App write. Under ~/.atm rather than
// ~/Library/Logs/ATM so that ATM's own data stays in one place, which is what
// makes `atm backup` and `atm diagnose` able to reason about it — see the "ATM
// 数据自有" principle in DESIGN.md.
func Dir() string { return filepath.Join(config.AtmDir, "logs") }

// Path is the CLI's log file. The App writes app.log beside it.
func Path() string { return filepath.Join(Dir(), "cli.log") }

var mutex sync.Mutex

// entry is one line. Kept flat and short: this file is read by a person under
// time pressure and embedded whole into a support bundle.
type entry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Event   string         `json:"event"`
	Command string         `json:"command,omitempty"`
	Error   string         `json:"error,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Failure records that something did not work. err may be nil for a failure that
// has no error value, which is why the event name carries the meaning rather than
// the message.
//
// Fields must hold identifiers, counts and statuses — never user content. See the
// package comment.
func Failure(event, command string, err error, fields map[string]any) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	write(entry{Level: "error", Event: event, Command: command, Error: message, Fields: fields})
}

// Lifecycle records a process boundary. These are the only non-failure lines, and
// they exist so an unclean exit is detectable at all.
func Lifecycle(event string, fields map[string]any) {
	write(entry{Level: "info", Event: event, Fields: fields})
}

func write(record entry) {
	record.Time = time.Now().UTC().Format(time.RFC3339)
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	line = append(line, '\n')

	mutex.Lock()
	defer mutex.Unlock()
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		return
	}
	rotateIfNeeded(Path(), len(line))
	// O_APPEND so concurrent atm processes interleave whole lines instead of
	// overwriting each other. A single write under the pipe buffer size is
	// atomic in practice, which is enough for a diagnostic log.
	file, err := os.OpenFile(Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	// Deliberately unchecked: a failed log write must not become the caller's
	// problem, and there is nowhere left to report it to.
	_, _ = file.Write(line)
}

// rotateIfNeeded moves the current file aside when the next line would take it
// past the cap. Rotating before the write rather than after keeps the cap a real
// bound instead of a threshold the file always sits just above.
func rotateIfNeeded(path string, incoming int) {
	info, err := os.Stat(path)
	if err != nil || info.Size()+int64(incoming) <= maxBytes {
		return
	}
	// Oldest first, so nothing is overwritten before it has been shifted.
	for index := keptRotations; index >= 1; index-- {
		older := fmt.Sprintf("%s.%d", path, index)
		if index == keptRotations {
			os.Remove(older)
		}
		if index > 1 {
			os.Rename(fmt.Sprintf("%s.%d", path, index-1), older)
		}
	}
	os.Rename(path, path+".1")
}

// Tail returns the last count lines of a log file, oldest first, for
// `atm diagnose --bundle`. Missing file is not an error: no failures logged is
// the normal state.
//
// It reads whole files, which is sound because they are capped at maxBytes.
func Tail(path string, count int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	start := 0
	for index := 0; index <= len(data); index++ {
		if index == len(data) || data[index] == '\n' {
			if index > start {
				lines = append(lines, string(data[start:index]))
			}
			start = index + 1
		}
	}
	if count > 0 && len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return lines, nil
}
