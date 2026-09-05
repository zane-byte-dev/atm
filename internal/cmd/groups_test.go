package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A command added without a group does not fail, it just lands under cobra's
// "Additional Commands" heading — which is where a reader looks last and an author
// never looks at all. Failing here forces the question "what is this for?" while
// the answer is still in someone's head.
func TestEveryVisibleCommandUnderAGroupedParentIsGrouped(t *testing.T) {
	applyCommandGroups()

	// Cobra owns these two and groups them itself.
	cobraOwned := map[string]bool{"help": true, "completion": true}

	for _, parentPath := range groupedParentPaths() {
		parent, ok := commandGroupParent(parentPath)
		if !ok {
			t.Errorf("commandGroups hangs groups under %q, which is not a command", parentPath)
			continue
		}
		label := parentPath
		if label == "" {
			label = rootCmd.Name()
		}

		grouped := map[string]string{}
		for _, group := range commandGroups {
			if group.parent != parentPath {
				continue
			}
			for _, name := range group.names {
				if existing, clash := grouped[name]; clash {
					t.Errorf("%s: %q is in both %q and %q", label, name, existing, group.id)
				}
				grouped[name] = group.id
			}
		}

		var ungrouped, stale []string
		seen := map[string]bool{}
		for _, command := range parent.Commands() {
			name := command.Name()
			seen[name] = true
			if cobraOwned[name] {
				continue
			}
			// Hidden commands are not in the list a person reads, so they need no
			// group. `dashboard` is a versioned payload for the browser refresh loop
			// with no human reader.
			if command.Hidden {
				continue
			}
			if command.GroupID == "" {
				ungrouped = append(ungrouped, name)
			}
		}
		for name := range grouped {
			if !seen[name] {
				stale = append(stale, name)
			}
		}
		sort.Strings(ungrouped)
		sort.Strings(stale)
		if len(ungrouped) > 0 {
			t.Errorf("%s: these commands would print under \"Additional Commands\": %s\n"+
				"add each to a group in commandGroups, or mark it Hidden if it has no human reader",
				label, strings.Join(ungrouped, ", "))
		}
		if len(stale) > 0 {
			t.Errorf("%s: commandGroups names commands that no longer exist: %s",
				label, strings.Join(stale, ", "))
		}
	}
}

// Applying the groups twice must not double the headings, because Execute and the
// tests both call it.
func TestApplyCommandGroupsIsIdempotent(t *testing.T) {
	applyCommandGroups()
	applyCommandGroups()

	for _, parentPath := range groupedParentPaths() {
		parent, ok := commandGroupParent(parentPath)
		if !ok {
			continue
		}
		seen := map[string]int{}
		for _, group := range parent.Groups() {
			seen[group.ID]++
		}
		for id, count := range seen {
			if count > 1 {
				t.Errorf("%s declares group %q %d times", parent.CommandPath(), id, count)
			}
		}
	}
}

// groupedParentPaths lists the distinct parents commandGroups describes, in the
// order they first appear.
func groupedParentPaths() []string {
	var paths []string
	seen := map[string]bool{}
	for _, group := range commandGroups {
		if !seen[group.parent] {
			seen[group.parent] = true
			paths = append(paths, group.parent)
		}
	}
	return paths
}

// The group titles are the first words a new reader sees, so an empty or
// duplicated one is worth catching.
func TestCommandGroupsAreWellFormed(t *testing.T) {
	titles, ids := map[string]bool{}, map[string]bool{}
	for _, group := range commandGroups {
		if group.id == "" || group.title == "" {
			t.Errorf("group %+v has an empty id or title", group)
		}
		if !strings.HasSuffix(group.title, ":") {
			t.Errorf("group title %q should end in a colon, to match cobra's own headings", group.title)
		}
		if ids[group.id] {
			t.Errorf("duplicate group id %q", group.id)
		}
		// Scoped per parent: two different commands may reasonably both have a
		// "Diagnostics:" heading, but one command must not print it twice.
		scopedTitle := group.parent + "\x00" + group.title
		if titles[scopedTitle] {
			t.Errorf("duplicate group title %q under %q", group.title, group.parent)
		}
		ids[group.id], titles[scopedTitle] = true, true
		if len(group.names) == 0 {
			t.Errorf("group %q has no commands", group.id)
		}
	}
}

// Every command that has subcommands must reject a stray argument *and* suggest
// what was probably meant. Cobra gives the root command suggestions for free but
// not a group that declares cobra.NoArgs, and the groups are exactly where the
// suggestion matters: `todo` alone has thirty-odd subcommands to scan by eye.
//
// Asserted behaviourally rather than by comparing function pointers, because what
// matters is the message a person reads.
func TestGroupsSuggestWhatWasProbablyMeant(t *testing.T) {
	var check func(command *cobra.Command)
	check = func(command *cobra.Command) {
		// `completion` and `help` are cobra's own, generated with cobra's own arg
		// validators. Their messages are not ours to change.
		if name := command.Name(); name == "completion" || name == "help" {
			return
		}
		for _, child := range command.Commands() {
			check(child)
		}
		if !command.HasSubCommands() || command.Args == nil {
			return
		}
		// Pick a real subcommand and drop a letter from it. Anything cobra would
		// suggest for the correct spelling it must also suggest for this.
		var target string
		for _, child := range command.Commands() {
			if child.IsAvailableCommand() && len(child.Name()) > 3 {
				target = child.Name()
				break
			}
		}
		if target == "" {
			return
		}
		typo := target[:len(target)-2] + target[len(target)-1:]

		err := command.Args(command, []string{typo})
		if err == nil {
			t.Errorf("%s accepted %q as an argument instead of rejecting an unknown subcommand",
				command.CommandPath(), typo)
			return
		}
		// Two wordings are acceptable. A group that declares ValidArgs — `config`,
		// whose `init` is a positional rather than a subcommand — is rejected by
		// cobra as an "invalid argument" instead, and still suggests.
		if !strings.Contains(err.Error(), "unknown command") &&
			!strings.Contains(err.Error(), "invalid argument") {
			t.Errorf("%s: error for %q does not say the input was rejected: %v",
				command.CommandPath(), typo, err)
		}
		if !strings.Contains(err.Error(), "Did you mean") {
			t.Errorf("%s: %q produced no suggestion — use noSubcommandArgs rather than cobra.NoArgs.\nGot: %v",
				command.CommandPath(), typo, err)
		}
	}
	check(rootCmd)
}

// A removed name should point at what replaced it rather than dead-ending, since
// the likeliest source of it is a note or a skill file written before the change.
func TestRemovedAliasesPointAtTheirReplacements(t *testing.T) {
	for _, testCase := range []struct{ group, removed, wants string }{
		{"todo", "focus", "start"},
		{"todo", "review-context", "context"},
	} {
		command, _, err := rootCmd.Find([]string{testCase.group})
		if err != nil {
			t.Fatalf("find %s: %v", testCase.group, err)
		}
		err = command.Args(command, []string{testCase.removed})
		if err == nil {
			t.Fatalf("atm %s %s did not error", testCase.group, testCase.removed)
		}
		if !strings.Contains(err.Error(), testCase.wants) {
			t.Errorf("atm %s %s does not suggest %q: %v",
				testCase.group, testCase.removed, testCase.wants, err)
		}
	}
}
