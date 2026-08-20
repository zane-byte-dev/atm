package cmd

import "github.com/spf13/cobra"

// Root help is navigation for a person at a terminal; it is not the Agent's
// capability catalog. App-only actions use typed IPC and do not need public Cobra
// mirrors, while commands retained here have an Agent, human repair, or diagnostic
// consumer. Grouping the remaining roots makes that smaller surface scannable.
var commandGroups = []struct {
	id    string
	title string
	names []string
}{
	{"work", "Work:", []string{"todo", "collect", "guard"}},
	{"observe", "Observe:", []string{"now", "session", "stats", "quota", "report"}},
	{"brain", "Second brain:", []string{"knowledge", "memory", "artifact"}},
	{"setup", "Setup and maintenance:", []string{
		"agent", "config", "day", "sync", "doctor", "diagnose", "backup", "restore", "version",
	}},
}

// applyCommandGroups runs from Execute rather than from an init function.
// AddCommand panics when a command names a group its parent does not have yet,
// and the commands register themselves from init functions spread across this
// package — whose order is the order of the filenames. Assigning groups after all
// of that has happened is the difference between a readable mapping and one that
// has to be kept in sync with an alphabet.
func applyCommandGroups() {
	byName := map[string]*cobra.Command{}
	for _, command := range rootCmd.Commands() {
		byName[command.Name()] = command
	}
	for _, group := range commandGroups {
		rootCmd.AddGroup(&cobra.Group{ID: group.id, Title: group.title})
		for _, name := range group.names {
			if command, ok := byName[name]; ok {
				command.GroupID = group.id
			}
		}
	}
}
