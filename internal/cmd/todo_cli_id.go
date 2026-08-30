package cmd

import (
	"regexp"
	"strings"
)

var todoCLIHintIDPattern = regexp.MustCompile(`^#?[tT]?([0-9]+)$`)

// canonicalTodoIDForHint keeps suggested commands copyable without making the
// Cobra adapter reach into persistence. Invalid references are left untouched
// so the application service can return its authoritative validation error.
func canonicalTodoIDForHint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	match := todoCLIHintIDPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return trimmed
	}
	digits := strings.TrimLeft(match[1], "0")
	if digits == "" {
		return trimmed
	}
	return "t" + digits
}
