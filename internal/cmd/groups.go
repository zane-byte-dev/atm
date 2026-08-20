package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// Help is navigation for a person at a terminal; it is not the Agent's capability
// catalog. App-only actions use typed IPC and do not need public Cobra mirrors,
// while commands retained here have an Agent, human repair, or diagnostic
// consumer. Grouping makes that surface scannable.
//
// parent is the command path the group hangs under, "" for the root. Order is
// significant: cobra prints groups in the order they are added, so the table
// reads top-to-bottom the way the help does.
var commandGroups = []struct {
	parent string
	id     string
	title  string
	names  []string
}{
	{"", "work", "Work:", []string{"todo", "collect", "guard"}},
	{"", "observe", "Observe:", []string{"now", "session", "stats", "quota", "report"}},
	{"", "brain", "Second brain:", []string{"knowledge", "memory", "artifact"}},
	{"", "setup", "Setup and maintenance:", []string{
		"agent", "config", "day", "sync", "doctor", "diagnose", "backup", "restore", "version",
	}},

	// `todo` earns its own grouping because it is the one command whose children
	// do not fit on a screen. The lifecycle group is deliberately first and small:
	// add → start → submit → done, with archive ↔ restore beside it, is the whole
	// everyday path, and an alphabetical list of twenty-one names buried it among
	// commands most readers need once a month.
	{"todo", "todo-lifecycle", "Lifecycle:", []string{
		"add", "start", "submit", "done", "archive", "restore",
	}},
	{"todo", "todo-content", "Reading and content:", []string{
		"list", "show", "doc", "log", "edit", "refine",
	}},
	{"todo", "todo-collab", "Collaboration and relations:", []string{
		"handoff", "match", "plan", "depend", "link",
	}},
	{"todo", "todo-batch", "Batch and permanent removal:", []string{"bulk", "delete"}},
	{"todo", "todo-diagnose", "Diagnostics:", []string{"context", "lint"}},
}

// applyCommandGroups runs from Execute rather than from an init function.
// AddCommand panics when a command names a group its parent does not have yet,
// and the commands register themselves from init functions spread across this
// package — whose order is the order of the filenames. Assigning groups after all
// of that has happened is the difference between a readable mapping and one that
// has to be kept in sync with an alphabet.
// Idempotent: cobra's AddGroup only appends, so calling this twice would print
// every heading twice. Tests call it directly as well as Execute.
func applyCommandGroups() {
	for _, group := range commandGroups {
		parent, ok := commandGroupParent(group.parent)
		if !ok {
			continue
		}
		if !hasCommandGroup(parent, group.id) {
			parent.AddGroup(&cobra.Group{ID: group.id, Title: group.title})
		}
		for _, name := range group.names {
			for _, child := range parent.Commands() {
				if child.Name() == name {
					child.GroupID = group.id
				}
			}
		}
	}
}

func hasCommandGroup(command *cobra.Command, id string) bool {
	for _, group := range command.Groups() {
		if group.ID == id {
			return true
		}
	}
	return false
}

// commandGroupParent resolves a group's parent path. "" is the root; anything
// else is looked up in the command tree so a renamed parent shows up as a missing
// group rather than as a silent no-op on the wrong command.
func commandGroupParent(path string) (*cobra.Command, bool) {
	if path == "" {
		return rootCmd, true
	}
	command, remaining, err := rootCmd.Find(strings.Fields(path))
	if err != nil || len(remaining) > 0 {
		return nil, false
	}
	return command, true
}
