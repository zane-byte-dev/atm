package agentevent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HookSpec is one hook entry ATM wants registered with an agent.
type HookSpec struct {
	// Event is the agent's hook event name.
	Event string
	// Matcher is the agent's selector for the event, empty when the event takes
	// none. For Notification it doubles as the reason, since Claude Code does
	// not repeat the matched matcher in the payload.
	Matcher string
	// Reason is passed to `atm agent hook --reason`.
	Reason string
}

// DesiredHooks lists what a source needs installed.
//
// Still narrow, but on one criterion only: does the notch use what the event
// says. PermissionRequest is left out because a decision-capable hook is a
// separate feature — reporting the block is enough to light the notch up, and
// staying non-blocking means installing ATM cannot change how the agent
// behaves. An unmatched PreToolUse is left out because PostToolUse already
// covers the same ground.
//
// PostToolUse *is* installed, for sources that can raise an attention signal.
// It fires on every tool call, which this file used to rule out on cost. That
// was measured and is not the real number: the hook is one process that writes
// one line to a unix socket, ~11ms wall and well under a millisecond of CPU,
// against an agent turn measured in seconds. The cost that does matter is on
// the app side — an event nudges the app to re-run `atm session status` — and
// that is handled where it belongs, by not refreshing on `resumed`.
//
// Without it there is no way to know the user dealt with a permission prompt:
// answering one is not a new prompt and does not end the turn, so `started` and
// `completed` both stay silent while the notch keeps saying "waiting for you".
func DesiredHooks(source string) []HookSpec {
	switch source {
	case SourceClaude:
		return []HookSpec{
			{Event: "SessionStart"},
			{Event: "UserPromptSubmit"},
			{Event: "Stop"},
			{Event: "SessionEnd"},
			// One entry per matcher: the payload does not say which fired.
			// idle_prompt is not among them — it fires a minute after the turn
			// ends and means "you have not come back", not "the agent is
			// blocked", so `notificationKinds` drops every event it produces.
			// Installing it would be a process per idle timer for nothing.
			{Event: "Notification", Matcher: "permission_prompt", Reason: "permission_prompt"},
			{Event: "Notification", Matcher: "agent_needs_input", Reason: "agent_needs_input"},
			{Event: "Notification", Matcher: "elicitation_dialog", Reason: "elicitation_dialog"},
			// Tool-scoped on purpose: AskUserQuestion's dialog is the only
			// blocking prompt Claude Code raises without notifying anyone.
			{Event: "PreToolUse", Matcher: "AskUserQuestion", Reason: "ask_user_question"},
			// The other half of the pair: the tool running again is how we learn
			// the permission prompt or the question was answered.
			{Event: "PostToolUse"},
		}
	case SourceCodex:
		return []HookSpec{
			{Event: "SessionStart", Matcher: "startup|resume"},
			{Event: "UserPromptSubmit"},
			{Event: "Stop"},
		}
	case SourceGrokbuild:
		// Grok discovers ~/.grok/hooks/*.json and merges every file. ATM owns a
		// single dedicated file so install/uninstall never rewrites the user's
		// other hook files or config.toml. Notification is unscoped because
		// Grok's hook file selects no matcher — but unlike Claude it repeats the
		// matcher in the payload as `notificationType`, so `classify` recovers
		// it there and the same `notificationKinds` table applies.
		//
		// PostToolUse matters more here than anywhere else: Grok's lifecycle
		// hooks are not reliably observed even when the file is installed, while
		// its tool hooks do fire. It is often the only event that arrives at all.
		return []HookSpec{
			{Event: "SessionStart"},
			{Event: "UserPromptSubmit"},
			{Event: "Stop"},
			{Event: "SessionEnd"},
			{Event: "Notification"},
			{Event: "PostToolUse"},
		}
	}
	return nil
}

// ConfigPath returns the file an agent reads its hooks from.
//
// Grok Build loads every JSON file under ~/.grok/hooks/, so ATM writes a
// dedicated atm-notch.json there instead of merging into a shared settings
// document. That keeps uninstall a pure file delete when nothing else lives
// in the same document.
func ConfigPath(source, home string) (string, error) {
	switch source {
	case SourceClaude:
		return filepath.Join(home, ".claude", "settings.json"), nil
	case SourceCodex:
		return filepath.Join(home, ".codex", "hooks.json"), nil
	case SourceGrokbuild:
		return filepath.Join(home, ".grok", "hooks", "atm-notch.json"), nil
	}
	return "", fmt.Errorf("no hook config known for source %q", source)
}

// InstallResult describes what changed, so the CLI can report honestly instead
// of always claiming success.
type InstallResult struct {
	Path    string
	Added   []string
	Removed []string
	Kept    []string
	// Conflicts names other tools already registered for a decision-capable
	// event, where two responders would fight over the outcome.
	Conflicts []string
}

func (r InstallResult) Changed() bool { return len(r.Added) > 0 || len(r.Removed) > 0 }

// conflictingEvents are the events where a second decision-capable hook would
// actually collide. Ping Island registers PermissionRequest with a 24-hour
// timeout to hold a tool call open while the user decides; a second responder on
// the same event would mean two approval prompts racing. ATM does not install
// here yet, but the installer reports the situation so the ground truth is known
// before that feature lands.
var conflictingEvents = []string{"PermissionRequest"}

// Install adds ATM's hooks to an agent config, leaving every other entry alone.
//
// Merge, never replace: a real ~/.claude/settings.json has other tools in it
// (Ping Island, telemetry wrappers, repo-specific shell hooks), and clobbering
// them would break tools the user depends on. Entries are matched by the exact
// command string ATM writes, which makes install idempotent and uninstall
// precise.
func Install(source, home, executable string) (InstallResult, error) {
	return mutate(source, home, executable, true)
}

// Uninstall removes only the entries whose command is ATM's.
func Uninstall(source, home, executable string) (InstallResult, error) {
	// Grok's config is a dedicated ATM-owned file. Drop the whole file (and the
	// runner) rather than surgically pruning DesiredHooks — hand-edited probe
	// entries or older event lists would otherwise survive.
	if source == SourceGrokbuild {
		path, err := ConfigPath(source, home)
		if err != nil {
			return InstallResult{}, err
		}
		document, err := readConfig(path)
		if err != nil {
			return InstallResult{}, err
		}
		result := InstallResult{Path: path}
		for _, spec := range DesiredHooks(source) {
			command := hookCommand(home, executable, source, spec.Reason)
			if hasHook(document, spec, command) {
				result.Removed = append(result.Removed, describe(spec))
			}
		}
		// Also count any leftover events that still point at our runner.
		runner := grokRunnerPath(home)
		if extra := countCommands(document, runner) - len(result.Removed); extra > 0 {
			result.Removed = append(result.Removed, fmt.Sprintf("+%d other", extra))
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return result, err
		}
		_ = os.Remove(runner)
		if len(result.Removed) == 0 {
			// File may have been absent; still report success with no change.
			return result, nil
		}
		return result, nil
	}
	return mutate(source, home, executable, false)
}

func countCommands(document map[string]any, command string) int {
	hooks := hooksRoot(document, false)
	if hooks == nil {
		return 0
	}
	count := 0
	for _, groups := range hooks {
		list, _ := groups.([]any)
		for _, entry := range list {
			group, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, hookEntry := range hooksIn(group) {
				if commandOf(hookEntry) == command {
					count++
				}
			}
		}
	}
	return count
}

// Status reports what is currently registered without writing anything.
func Status(source, home, executable string) (InstallResult, error) {
	path, err := ConfigPath(source, home)
	if err != nil {
		return InstallResult{}, err
	}
	document, err := readConfig(path)
	if err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{Path: path, Conflicts: findConflicts(document, executable)}
	for _, spec := range DesiredHooks(source) {
		command := hookCommand(home, executable, source, spec.Reason)
		if hasHook(document, spec, command) {
			result.Kept = append(result.Kept, describe(spec))
		} else {
			result.Added = append(result.Added, describe(spec))
		}
	}
	// In status, Added means "missing"; surface it under that name at the call
	// site rather than pretending anything was written.
	return result, nil
}

func mutate(source, home, executable string, adding bool) (InstallResult, error) {
	path, err := ConfigPath(source, home)
	if err != nil {
		return InstallResult{}, err
	}
	specs := DesiredHooks(source)
	if len(specs) == 0 {
		return InstallResult{}, fmt.Errorf("no hooks defined for source %q", source)
	}
	if adding && source == SourceGrokbuild {
		// Grok executes hook commands more reliably as a single absolute
		// executable path (see the r2c PreToolUse wrapper). Bake the atm
		// binary into a tiny runner so argv splitting never bites us, and so
		// the snake_case payload fix always hits the binary we just installed.
		if err := writeGrokRunner(home, executable); err != nil {
			return InstallResult{}, err
		}
	}
	document, err := readConfig(path)
	if err != nil {
		return InstallResult{}, err
	}

	result := InstallResult{Path: path, Conflicts: findConflicts(document, executable)}
	for _, spec := range specs {
		command := hookCommand(home, executable, source, spec.Reason)
		switch {
		case adding && hasHook(document, spec, command):
			result.Kept = append(result.Kept, describe(spec))
		case adding:
			addHook(document, spec, command)
			result.Added = append(result.Added, describe(spec))
		default:
			if removeHook(document, spec, command) {
				result.Removed = append(result.Removed, describe(spec))
			}
		}
	}
	if adding {
		result.Removed = append(result.Removed, pruneRetired(document, source, home, executable, specs)...)
	}

	if result.Changed() {
		// Grok owns a dedicated file. After a full uninstall leave no empty
		// leftover — a zero-byte or `{}` file still shows up in /hooks and
		// confuses users about what is installed.
		if !adding && source == SourceGrokbuild && hooksRoot(document, false) == nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return result, err
			}
			_ = os.Remove(grokRunnerPath(home))
			return result, nil
		}
		if err := writeConfig(path, document); err != nil {
			return result, err
		}
	}
	return result, nil
}

func describe(spec HookSpec) string {
	if spec.Matcher == "" {
		return spec.Event
	}
	return spec.Event + "(" + spec.Matcher + ")"
}

// hookCommand is the exact string written into the agent config. Grok gets a
// dedicated runner script path; Claude/Codex keep the multi-arg atm invocation
// their hook runners already understand.
func hookCommand(home, executable, source, reason string) string {
	if source == SourceGrokbuild {
		return grokRunnerPath(home)
	}
	command := shellQuote(executable) + " agent hook --source " + source
	if reason != "" {
		command += " --reason " + reason
	}
	return command
}

func grokRunnerPath(home string) string {
	return filepath.Join(home, ".grok", "hooks", "atm-notch-run.sh")
}

// writeGrokRunner installs the thin shell wrapper Grok's hook runner execs.
// stdin is captured once so we can both append a one-line debug log under
// ~/.atm/grok-hook.log and forward the payload to `atm agent hook`.
func writeGrokRunner(home, executable string) error {
	path := grokRunnerPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := "#!/bin/sh\n" +
		"# Generated by `atm agent hook install --source grokbuild`. Do not edit.\n" +
		"LOG=\"${HOME}/.atm/grok-hook.log\"\n" +
		"mkdir -p \"${HOME}/.atm\"\n" +
		"ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)\n" +
		"payload=$(cat)\n" +
		"event=$(printf '%s' \"$payload\" | sed -n 's/.*\"hookEventName\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p' | head -1)\n" +
		"sid=$(printf '%s' \"$payload\" | sed -n 's/.*\"sessionId\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p' | head -1)\n" +
		"printf '%s event=%s session=%s bytes=%s\\n' \"$ts\" \"${event:-?}\" \"${sid:-?}\" \"${#payload}\" >>\"$LOG\"\n" +
		"printf '%s' \"$payload\" | " + shellQuote(executable) + " agent hook --source grokbuild\n" +
		"exit 0\n"
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atm-notch-run-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if _, err := temporary.WriteString(body); err != nil {
		temporary.Close()
		os.Remove(name)
		return err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func shellQuote(path string) string {
	if strings.ContainsAny(path, " \t\"'") {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}

// readConfig loads the config, tolerating a missing file but never a malformed
// one — silently discarding a config we failed to parse would delete the user's
// other hooks.
func readConfig(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON, refusing to rewrite it: %w", path, err)
	}
	return document, nil
}

func writeConfig(path string, document map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	// Write via a temporary file in the same directory and rename, so an
	// interrupted write cannot leave the user with a truncated settings file.
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atm-hooks-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		os.Remove(name)
		return err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// hooksRoot returns the "hooks" object, creating it when asked.
func hooksRoot(document map[string]any, create bool) map[string]any {
	if existing, ok := document["hooks"].(map[string]any); ok {
		return existing
	}
	if !create {
		return nil
	}
	created := map[string]any{}
	document["hooks"] = created
	return created
}

// matcherOf reads a group's matcher, treating absent and "" as the same thing:
// agents write both for events that take no matcher.
func matcherOf(group map[string]any) string {
	value, _ := group["matcher"].(string)
	return value
}

func groupsFor(document map[string]any, event string) []any {
	hooks := hooksRoot(document, false)
	if hooks == nil {
		return nil
	}
	groups, _ := hooks[event].([]any)
	return groups
}

func hasHook(document map[string]any, spec HookSpec, command string) bool {
	for _, entry := range groupsFor(document, spec.Event) {
		group, ok := entry.(map[string]any)
		if !ok || matcherOf(group) != spec.Matcher {
			continue
		}
		for _, hookEntry := range hooksIn(group) {
			if commandOf(hookEntry) == command {
				return true
			}
		}
	}
	return false
}

func hooksIn(group map[string]any) []any {
	list, _ := group["hooks"].([]any)
	return list
}

func commandOf(entry any) string {
	hook, ok := entry.(map[string]any)
	if !ok {
		return ""
	}
	command, _ := hook["command"].(string)
	return command
}

func addHook(document map[string]any, spec HookSpec, command string) {
	hooks := hooksRoot(document, true)
	groups, _ := hooks[spec.Event].([]any)

	entry := map[string]any{"type": "command", "command": command}

	// Prefer an existing group with the same matcher so we add one hook beside
	// whatever is already there rather than a duplicate group.
	for _, candidate := range groups {
		group, ok := candidate.(map[string]any)
		if !ok || matcherOf(group) != spec.Matcher {
			continue
		}
		group["hooks"] = append(hooksIn(group), entry)
		hooks[spec.Event] = groups
		return
	}

	group := map[string]any{"hooks": []any{entry}}
	if spec.Matcher != "" {
		group["matcher"] = spec.Matcher
	}
	hooks[spec.Event] = append(groups, group)
}

// removeHook deletes ATM's entry and prunes containers that become empty,
// leaving other tools' entries untouched.
func removeHook(document map[string]any, spec HookSpec, command string) bool {
	hooks := hooksRoot(document, false)
	if hooks == nil {
		return false
	}
	groups, _ := hooks[spec.Event].([]any)
	removed := false
	remainingGroups := make([]any, 0, len(groups))

	for _, candidate := range groups {
		group, ok := candidate.(map[string]any)
		if !ok || matcherOf(group) != spec.Matcher {
			remainingGroups = append(remainingGroups, candidate)
			continue
		}
		entries := hooksIn(group)
		remainingEntries := make([]any, 0, len(entries))
		for _, entry := range entries {
			if commandOf(entry) == command {
				removed = true
				continue
			}
			remainingEntries = append(remainingEntries, entry)
		}
		if len(remainingEntries) == 0 {
			// The group existed only for us.
			continue
		}
		group["hooks"] = remainingEntries
		remainingGroups = append(remainingGroups, group)
	}

	if !removed {
		return false
	}
	if len(remainingGroups) == 0 {
		delete(hooks, spec.Event)
	} else {
		hooks[spec.Event] = remainingGroups
	}
	if len(hooks) == 0 {
		delete(document, "hooks")
	}
	return true
}

// pruneRetired deletes entries that point at ATM but that DesiredHooks no
// longer lists, and reports what it dropped.
//
// Without this, retiring a spec only ever reaches machines that have never
// installed: `mutate` walks the desired list, so an entry that left the list is
// no longer looked at by install, by uninstall, or by status. Claude's
// `Notification(idle_prompt)` is the case that forced this — it kept firing on
// every existing install after it stopped meaning anything, paying for a
// process per idle timer to produce an event `classify` now drops.
//
// Ownership is decided by the command string, never by the event name, so an
// event ATM shares with another tool loses only ATM's own entry. Matched by
// prefix rather than equality because the retired command's `--reason` argument
// is exactly the thing that is no longer reconstructible from the spec list.
func pruneRetired(document map[string]any, source, home, executable string, specs []HookSpec) []string {
	hooks := hooksRoot(document, false)
	if hooks == nil {
		return nil
	}
	wanted := make(map[string]bool, len(specs))
	for _, spec := range specs {
		wanted[describe(spec)] = true
	}

	var retired []string
	for event, groups := range hooks {
		list, _ := groups.([]any)
		remainingGroups := make([]any, 0, len(list))
		for _, candidate := range list {
			group, ok := candidate.(map[string]any)
			if !ok {
				remainingGroups = append(remainingGroups, candidate)
				continue
			}
			label := describe(HookSpec{Event: event, Matcher: matcherOf(group)})
			if wanted[label] {
				remainingGroups = append(remainingGroups, candidate)
				continue
			}
			entries := hooksIn(group)
			remainingEntries := make([]any, 0, len(entries))
			dropped := false
			for _, entry := range entries {
				if ownsCommand(commandOf(entry), source, home, executable) {
					dropped = true
					continue
				}
				remainingEntries = append(remainingEntries, entry)
			}
			if !dropped {
				remainingGroups = append(remainingGroups, candidate)
				continue
			}
			retired = append(retired, label)
			if len(remainingEntries) == 0 {
				// The group existed only for us.
				continue
			}
			group["hooks"] = remainingEntries
			remainingGroups = append(remainingGroups, group)
		}
		if len(remainingGroups) == 0 {
			delete(hooks, event)
			continue
		}
		hooks[event] = remainingGroups
	}
	if len(hooks) == 0 {
		delete(document, "hooks")
	}
	// Map iteration order is random and this string reaches the CLI output and
	// the tests.
	sort.Strings(retired)
	return retired
}

// ownsCommand reports whether a registered command is one ATM wrote for this
// source. Grok execs a dedicated runner script, so that path is the whole
// identity; Claude and Codex get an `atm agent hook --source <source>`
// invocation that may or may not carry a trailing `--reason`.
func ownsCommand(command, source, home, executable string) bool {
	if command == "" {
		return false
	}
	if source == SourceGrokbuild {
		return command == grokRunnerPath(home)
	}
	prefix := hookCommand(home, executable, source, "")
	if command == prefix {
		return true
	}
	// Require the separator so `--source claude` never claims `--source claudex`.
	return strings.HasPrefix(command, prefix+" ")
}

// findConflicts lists other tools registered on decision-capable events.
func findConflicts(document map[string]any, executable string) []string {
	var conflicts []string
	for _, event := range conflictingEvents {
		for _, entry := range groupsFor(document, event) {
			group, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, hookEntry := range hooksIn(group) {
				command := commandOf(hookEntry)
				if command == "" || strings.Contains(command, executable) {
					continue
				}
				conflicts = append(conflicts, event+": "+command)
			}
		}
	}
	sort.Strings(conflicts)
	return conflicts
}
