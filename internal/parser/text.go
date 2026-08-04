package parser

import "strings"

// VisibleUserText strips the parts of a "user" message that the human never
// typed: harness preambles the agent injects on their behalf (plugin lists,
// AGENTS.md, permission blurbs) and inline blocks the editor appends. It
// returns "" when nothing the human wrote remains.
//
// Every surface that shows a prompt back to a human needs this, and two of them
// live in different packages — the CLI renders stored messages while the codex
// parser has to name a session from its first real prompt — so the prefix list
// lives here rather than being copied per caller.
func VisibleUserText(s string) string {
	s = strings.TrimSpace(s)
	if marker := "## My request for Codex:"; strings.Contains(s, marker) {
		s = s[strings.Index(s, marker)+len(marker):]
	}
	for _, prefix := range []string{
		"<recommended_plugins>",
		"# AGENTS.md instructions",
		"<permissions instructions>",
		"<app-context>",
		"<environment_context>",
		"<skills_instructions>",
		"<image name=",
		"</image>",
		"Some conversation entries were omitted.",
	} {
		if strings.HasPrefix(strings.TrimSpace(s), prefix) {
			return ""
		}
	}
	for _, tag := range []string{"ide_opened_file", "ide_selection", "system-reminder", "system_context"} {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		for {
			i := strings.Index(s, open)
			if i == -1 {
				break
			}
			j := strings.Index(s, close)
			if j == -1 {
				s = s[:i]
				break
			}
			s = s[:i] + s[j+len(close):]
		}
	}
	return strings.TrimSpace(s)
}

// FirstLine keeps a one-line label out of a possibly multi-line prompt.
func FirstLine(value string) string {
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(value)
}
