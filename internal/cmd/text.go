package cmd

import (
	"strings"

	"github.com/zane-byte-dev/atm/internal/parser"
)

// The two rendering rules almost every command shares: strip a transcript line
// down to what a person actually wrote, and fit it in a column.
//
// They live here rather than beside one command because nine adapters use them,
// and while they were in report.go a reader had to know that `atm report` owned
// the helper that `atm status` was calling.

// cleanMsg extracts the visible human text from a transcript message, dropping
// the harness scaffolding — tool results, system reminders — that surrounds it.
func cleanMsg(s string) string { return parser.VisibleUserText(s) }

// truncLine takes the first line and clips it to max runes. Runes rather than
// bytes: these lines are frequently Chinese, and cutting mid-codepoint would
// print a replacement character.
func truncLine(s string, max int) string {
	s = strings.SplitN(s, "\n", 2)[0]
	runes := []rune(s)
	if len(runes) > max {
		s = string(runes[:max])
	}
	return s
}
