package store

import (
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Creator answers "who filed this todo", which Source cannot: Source is free
// text describing where a request came from (a chat, a session, a roadmap
// split), so it is useful to read and useless to filter or count. Creator is a
// closed vocabulary instead — the human, automatic collection, or the agent that
// filed it — which is what makes `todo list --creator` a real question.
const (
	// TodoCreatorMe is the human behind this installation, whether they typed
	// `atm todo add` in a terminal or used the desktop app. Stored as a stable
	// token rather than a name: config.OwnerName only decorates the display, so
	// renaming yourself never rewrites a record.
	TodoCreatorMe = "me"
	// TodoCreatorCollect is automatic connector collection, which files todos
	// from chat without anyone asking for one.
	TodoCreatorCollect = "collect"
)

// TodoCreatorVocabulary lists what a creator may be, for help text and for the
// error a rejected value gets. Agents are named by the same identifiers the rest
// of ATM uses, so a creator and an `--agent` filter never disagree.
var TodoCreatorVocabulary = []string{
	TodoCreatorMe, TodoCreatorCollect,
	"claude", "codex", "pi", "copilot", "qoder", "qodercli", "qoderwork", "grokbuild",
}

// NormalizeTodoCreator resolves one creator to its stored token. An empty value
// stays empty: todos created before creator existed have no creator, and
// inventing one for them would be a guess dressed up as a record.
func NormalizeTodoCreator(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	switch strings.ToLower(trimmed) {
	case TodoCreatorMe, "self", "human", "我":
		return TodoCreatorMe, nil
	case TodoCreatorCollect, "collection", "收集":
		return TodoCreatorCollect, nil
	}
	if agent := config.NormalizeAgent(trimmed); agent != "" {
		return agent, nil
	}
	return "", fmt.Errorf("unknown creator: %s (use %s)", value, strings.Join(TodoCreatorVocabulary, ", "))
}

// TodoCreatorDisplay renders a creator for a human reading a command's output.
// Agents keep their own name because that is what the user calls them; only "me"
// is decorated, since "me" in a list of creators reads as a stray English word
// next to codex and 收集.
func TodoCreatorDisplay(creator string) string {
	switch creator {
	case "":
		return ""
	case TodoCreatorMe:
		if name := strings.TrimSpace(config.OwnerName); name != "" {
			return name + "（我）"
		}
		return "我"
	case TodoCreatorCollect:
		return "收集"
	}
	return creator
}

// TodoCreatorDocLabel renders a creator for the markdown task card, which is a
// stored artifact rather than a command's output. It deliberately ignores
// config.OwnerName: a card holding the nickname would be reported as drifting
// from the database the moment the nickname changed, and every card would have to
// be rewritten to agree with a setting that changed nothing about the record. The
// token plus a fixed gloss is the same shape 状态 already uses.
func TodoCreatorDocLabel(creator string) string {
	switch creator {
	case "":
		return ""
	case TodoCreatorMe:
		return "me（我）"
	case TodoCreatorCollect:
		return "collect（收集）"
	}
	return creator
}
