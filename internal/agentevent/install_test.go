package agentevent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const atmBinary = "/usr/local/bin/atm"

// realWorldClaudeSettings is a trimmed copy of a genuine ~/.claude/settings.json
// from a machine with three other hook-using tools installed. The installer's
// hardest requirement is not adding its own entries — it is not destroying
// these.
const realWorldClaudeSettings = `{
  "model": "opus",
  "hooks": {
    "Notification": [
      {
        "matcher": "*",
        "hooks": [
          {"type": "command", "command": "/Users/tester/.ping-island/bin/ping-island-bridge --source claude"}
        ]
      }
    ],
    "PermissionRequest": [
      {
        "matcher": "*",
        "hooks": [
          {"type": "command", "command": "/Users/tester/.ping-island/bin/ping-island-bridge --source claude", "timeout": 86400}
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "bash \"/Users/tester/.r2c/scripts/claude-cli-hook.sh\"", "timeout": 15}
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "*",
        "hooks": [
          {"type": "command", "command": "/Users/tester/.loongsuite-pilot/hooks/claude-code-loongsuite-pilot-hook.sh stop"}
        ]
      },
      {
        "hooks": [
          {"type": "command", "command": "/Users/tester/.ping-island/bin/ping-island-bridge --source claude"}
        ]
      }
    ]
  }
}`

func writeHome(t *testing.T, relativePath, contents string) string {
	t.Helper()
	home := t.TempDir()
	full := filepath.Join(home, relativePath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if contents != "" {
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return home
}

func readDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("config is not valid JSON after writing: %v", err)
	}
	return document
}

// commandsFor collects every command registered for an event, so tests can
// assert on the whole picture rather than just ATM's own entry.
func commandsFor(document map[string]any, event string) []string {
	var commands []string
	for _, entry := range groupsFor(document, event) {
		group, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for _, hookEntry := range hooksIn(group) {
			commands = append(commands, commandOf(hookEntry))
		}
	}
	return commands
}

func TestInstallLeavesEveryOtherToolsHooksIntact(t *testing.T) {
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)

	result, err := Install(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !result.Changed() {
		t.Fatal("expected Install to change the config")
	}

	document := readDocument(t, result.Path)

	// Every pre-existing hook must still be there, byte for byte.
	preserved := map[string][]string{
		"Notification":      {"/Users/tester/.ping-island/bin/ping-island-bridge --source claude"},
		"PermissionRequest": {"/Users/tester/.ping-island/bin/ping-island-bridge --source claude"},
		"PreToolUse":        {`bash "/Users/tester/.r2c/scripts/claude-cli-hook.sh"`},
		"Stop": {
			"/Users/tester/.loongsuite-pilot/hooks/claude-code-loongsuite-pilot-hook.sh stop",
			"/Users/tester/.ping-island/bin/ping-island-bridge --source claude",
		},
	}
	for event, expected := range preserved {
		commands := commandsFor(document, event)
		for _, want := range expected {
			if !containsString(commands, want) {
				t.Errorf("%s lost a pre-existing hook %q; have %v", event, want, commands)
			}
		}
	}

	// Unrelated top-level settings survive too.
	if document["model"] != "opus" {
		t.Errorf("unrelated settings were dropped: model = %v", document["model"])
	}

	// And ATM's own hooks are present.
	for _, spec := range DesiredHooks(SourceClaude) {
		if !hasHook(document, spec, hookCommand("", atmBinary, SourceClaude, spec.Reason)) {
			t.Errorf("ATM hook for %s was not installed", describe(spec))
		}
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)

	if _, err := Install(SourceClaude, home, atmBinary); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	path, _ := ConfigPath(SourceClaude, home)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	second, err := Install(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if second.Changed() {
		t.Errorf("second Install reported changes: added=%v removed=%v", second.Added, second.Removed)
	}
	if len(second.Kept) != len(DesiredHooks(SourceClaude)) {
		t.Errorf("expected every hook to be reported as already present, got %v", second.Kept)
	}
	after, _ := os.ReadFile(path)
	if string(first) != string(after) {
		t.Error("a second Install rewrote the file")
	}
}

func TestUninstallRemovesOnlyATMEntries(t *testing.T) {
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)
	if _, err := Install(SourceClaude, home, atmBinary); err != nil {
		t.Fatalf("Install: %v", err)
	}

	result, err := Uninstall(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(result.Removed) != len(DesiredHooks(SourceClaude)) {
		t.Errorf("removed %v, expected all of ATM's hooks", result.Removed)
	}

	document := readDocument(t, result.Path)
	for _, spec := range DesiredHooks(SourceClaude) {
		if hasHook(document, spec, hookCommand("", atmBinary, SourceClaude, spec.Reason)) {
			t.Errorf("ATM hook for %s survived uninstall", describe(spec))
		}
	}
	// The other tools are still wired up.
	if got := commandsFor(document, "Stop"); len(got) != 2 {
		t.Errorf("Stop should still hold the two foreign hooks, got %v", got)
	}
	if got := commandsFor(document, "Notification"); len(got) != 1 {
		t.Errorf("Notification should still hold ping-island's hook, got %v", got)
	}
	if document["model"] != "opus" {
		t.Errorf("unrelated settings were dropped: %v", document["model"])
	}
}

func TestUninstallOnACleanConfigChangesNothing(t *testing.T) {
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)
	result, err := Uninstall(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if result.Changed() {
		t.Errorf("expected no changes, got removed=%v", result.Removed)
	}
}

func TestInstallCreatesAConfigThatDoesNotExistYet(t *testing.T) {
	home := t.TempDir()
	result, err := Install(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	document := readDocument(t, result.Path)
	for _, spec := range DesiredHooks(SourceClaude) {
		if !hasHook(document, spec, hookCommand("", atmBinary, SourceClaude, spec.Reason)) {
			t.Errorf("hook for %s missing from a fresh config", describe(spec))
		}
	}
}

func TestInstallRefusesToRewriteAMalformedConfig(t *testing.T) {
	// Overwriting an unparseable settings.json would silently delete whatever
	// the user had in it, so bail out and let them fix the file.
	home := writeHome(t, ".claude/settings.json", `{"hooks": {`)
	if _, err := Install(SourceClaude, home, atmBinary); err == nil {
		t.Fatal("expected Install to refuse a malformed config")
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != `{"hooks": {` {
		t.Errorf("the malformed config was modified: %q", raw)
	}
}

func TestInstallHandlesAnEmptyConfigFile(t *testing.T) {
	home := writeHome(t, ".claude/settings.json", "\n")
	if _, err := Install(SourceClaude, home, atmBinary); err != nil {
		t.Fatalf("Install: %v", err)
	}
}

func TestInstallJoinsAnExistingGroupWithTheSameMatcher(t *testing.T) {
	// ping-island already owns Notification(*) here. ATM installs
	// Notification(permission_prompt), a different matcher, so it must create its
	// own group rather than merge into the wildcard one.
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)
	result, err := Install(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	document := readDocument(t, result.Path)

	var wildcardCommands, permissionCommands []string
	for _, entry := range groupsFor(document, "Notification") {
		group := entry.(map[string]any)
		for _, hookEntry := range hooksIn(group) {
			switch matcherOf(group) {
			case "*":
				wildcardCommands = append(wildcardCommands, commandOf(hookEntry))
			case "permission_prompt":
				permissionCommands = append(permissionCommands, commandOf(hookEntry))
			}
		}
	}
	if len(wildcardCommands) != 1 || !strings.Contains(wildcardCommands[0], "ping-island") {
		t.Errorf("the wildcard group was disturbed: %v", wildcardCommands)
	}
	if len(permissionCommands) != 1 || !strings.Contains(permissionCommands[0], "--reason permission_prompt") {
		t.Errorf("ATM's matcher-specific group is wrong: %v", permissionCommands)
	}
}

func TestNotificationHooksCarryTheirMatcherAsAReason(t *testing.T) {
	// The reason flag is not decoration: Claude Code does not tell the hook which
	// matcher fired, so the only way to distinguish "waiting for permission" from
	// "went idle" is the command line the installer wrote.
	for _, spec := range DesiredHooks(SourceClaude) {
		if spec.Event != "Notification" {
			continue
		}
		if spec.Reason == "" {
			t.Errorf("Notification(%s) has no reason", spec.Matcher)
		}
		if spec.Reason != spec.Matcher {
			t.Errorf("Notification(%s) reason = %q, want the matcher", spec.Matcher, spec.Reason)
		}
	}
}

func TestDesiredHooksStayOutOfTheAgentsHotPath(t *testing.T) {
	// A decision-capable PermissionRequest hook is a separate feature: installing
	// one by accident would be a behavioural change, not a UI one. An unmatched
	// PreToolUse is redundant now that PostToolUse is installed, and it cannot
	// observe what PostToolUse is there for — it runs *before* the permission
	// prompt, not after the user answers it.
	for _, source := range []string{SourceClaude, SourceCodex, SourceGrokbuild, SourceQoder} {
		for _, spec := range DesiredHooks(source) {
			switch spec.Event {
			case "PermissionRequest":
				t.Errorf("%s should not install a %s hook in this phase", source, spec.Event)
			case "PreToolUse":
				if spec.Matcher == "" {
					t.Errorf("%s installs PreToolUse with no matcher, which fires for every tool call", source)
				}
			}
		}
	}
}

func TestPostToolUseOnlyWhereAttentionCanBeRaised(t *testing.T) {
	// PostToolUse earns its per-tool-call process by retiring attention signals.
	// A source that never raises one has nothing for it to retire, so installing
	// it there would be pure cost — which is exactly the trade this file used to
	// get wrong in the other direction.
	for _, source := range SupportedSources() {
		var raisesAttention, installsPostToolUse bool
		for _, spec := range DesiredHooks(source) {
			switch spec.Event {
			case "Notification", "PreToolUse":
				raisesAttention = true
			case "PostToolUse":
				installsPostToolUse = true
				if spec.Matcher != "" {
					t.Errorf("%s scopes PostToolUse to %q; a permission prompt can block any tool",
						source, spec.Matcher)
				}
			}
		}
		if raisesAttention != installsPostToolUse {
			t.Errorf("%s raises attention = %v but installs PostToolUse = %v; the two go together",
				source, raisesAttention, installsPostToolUse)
		}
	}
}

func TestInstallRetiresATMEntriesItNoLongerWants(t *testing.T) {
	// A settings.json from a machine that installed while idle_prompt was still
	// desired, next to another tool on the same event and an unrelated ATM-owned
	// entry that is still wanted.
	const stale = `{
  "hooks": {
    "Notification": [
      {
        "matcher": "*",
        "hooks": [
          {"type": "command", "command": "/Users/tester/.ping-island/bin/ping-island-bridge --source claude"}
        ]
      },
      {
        "matcher": "idle_prompt",
        "hooks": [
          {"type": "command", "command": "/usr/local/bin/atm agent hook --source claude --reason idle_prompt"}
        ]
      },
      {
        "matcher": "permission_prompt",
        "hooks": [
          {"type": "command", "command": "/usr/local/bin/atm agent hook --source claude --reason permission_prompt"}
        ]
      }
    ]
  }
}`
	home := writeHome(t, ".claude/settings.json", stale)

	result, err := Install(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := strings.Join(result.Removed, ","); got != "Notification(idle_prompt)" {
		t.Errorf("Removed = %q, want the retired idle_prompt entry and nothing else", got)
	}

	commands := strings.Join(commandsFor(readDocument(t, result.Path), "Notification"), "\n")
	if strings.Contains(commands, "--reason idle_prompt") {
		t.Error("retired Notification(idle_prompt) survived install; it would keep firing forever")
	}
	// Ownership is decided per command, so the neighbours must be untouched.
	for _, want := range []string{
		"/Users/tester/.ping-island/bin/ping-island-bridge --source claude",
		"--reason permission_prompt",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("pruning removed %q, which it does not own", want)
		}
	}
}

func TestInstallIsStillIdempotent(t *testing.T) {
	// Pruning walks every event, so a second install must find nothing to do —
	// otherwise it would be quietly fighting the first one.
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)
	if _, err := Install(SourceClaude, home, atmBinary); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	result, err := Install(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if result.Changed() {
		t.Errorf("second Install added %v and removed %v, want no change",
			result.Added, result.Removed)
	}
}

func TestInstalledNotificationMatchersAreNotDropped(t *testing.T) {
	// Every installed matcher costs a process each time it fires, so installing
	// one whose events `classify` always drops is pure cost. idle_prompt was
	// exactly that once it stopped counting as attention — the pair has to move
	// together, in both directions.
	for _, source := range SupportedSources() {
		for _, spec := range DesiredHooks(source) {
			if spec.Event != "Notification" || spec.Matcher == "" {
				continue
			}
			kind, known := notificationKinds[spec.Matcher]
			if known && kind == "" {
				t.Errorf("%s installs Notification(%s) but classify drops it; stop installing it",
					source, spec.Matcher)
			}
		}
	}
}

func TestStatusReportsMissingHooksWithoutWriting(t *testing.T) {
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)
	before, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))

	result, err := Status(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Added) != len(DesiredHooks(SourceClaude)) {
		t.Errorf("expected every hook to be reported missing, got missing=%v kept=%v", result.Added, result.Kept)
	}
	after, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if string(before) != string(after) {
		t.Error("Status modified the config")
	}
}

func TestStatusFlagsAnotherToolOwningTheDecisionEvent(t *testing.T) {
	// Ping Island holds PermissionRequest open for 24 hours to let the user
	// decide. Knowing that is a prerequisite for ever adding in-notch approval,
	// so the installer surfaces it now rather than discovering it later.
	home := writeHome(t, ".claude/settings.json", realWorldClaudeSettings)
	result, err := Status(SourceClaude, home, atmBinary)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected one conflict, got %v", result.Conflicts)
	}
	if !strings.Contains(result.Conflicts[0], "PermissionRequest") ||
		!strings.Contains(result.Conflicts[0], "ping-island") {
		t.Errorf("unexpected conflict text: %q", result.Conflicts[0])
	}
}

func TestCodexUsesItsOwnConfigFile(t *testing.T) {
	home := t.TempDir()
	path, err := ConfigPath(SourceCodex, home)
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(home, ".codex", "hooks.json"); path != want {
		t.Errorf("codex config path = %q, want %q", path, want)
	}
	result, err := Install(SourceCodex, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	document := readDocument(t, result.Path)
	for _, spec := range DesiredHooks(SourceCodex) {
		if !hasHook(document, spec, hookCommand("", atmBinary, SourceCodex, spec.Reason)) {
			t.Errorf("codex hook for %s missing", describe(spec))
		}
	}
	// Codex commands must say codex, or events would be attributed to Claude.
	if got := commandsFor(document, "Stop"); len(got) != 1 || !strings.Contains(got[0], "--source codex") {
		t.Errorf("codex Stop hook = %v", got)
	}
}

// realWorldQoderSettings is a trimmed copy of a genuine ~/.qoder/settings.json.
// Qoder keeps unrelated top-level settings in the same document as its hooks and
// already has two other tools registered, so this is the shape the installer has
// to merge into without disturbing anything.
const realWorldQoderSettings = `{
  "enabledPlugins": {"security-scan@qoder-bundler": true},
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {"type": "command", "command": "/Users/tester/.ping-island/bin/ping-island-bridge --source claude --client-kind qoder"}
        ]
      },
      {
        "matcher": "Bash|bash|terminal",
        "hooks": [
          {"type": "command", "command": "bash /Users/tester/.r2c/scripts/qoder-cli-hook.sh", "timeout": 15}
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {"type": "command", "command": "/Users/tester/.ping-island/bin/ping-island-bridge --source claude --client-kind qoder"}
        ]
      }
    ]
  }
}`

func TestQoderInstallsIntoItsOwnSettingsDocument(t *testing.T) {
	home := writeHome(t, ".qoder/settings.json", realWorldQoderSettings)
	path, err := ConfigPath(SourceQoder, home)
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(home, ".qoder", "settings.json"); path != want {
		t.Errorf("qoder config path = %q, want %q", path, want)
	}

	result, err := Install(SourceQoder, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	document := readDocument(t, result.Path)
	for _, spec := range DesiredHooks(SourceQoder) {
		if !hasHook(document, spec, hookCommand(home, atmBinary, SourceQoder, spec.Reason)) {
			t.Errorf("qoder hook for %s missing", describe(spec))
		}
	}
	// Events must be attributed to qoder, not to the Claude payload shape they
	// happen to share.
	stop := commandsFor(document, "Stop")
	if len(stop) != 2 {
		t.Fatalf("Stop commands = %v, want ping-island's plus ATM's", stop)
	}
	if !strings.Contains(strings.Join(stop, " "), "--source qoder") {
		t.Errorf("no ATM qoder Stop hook registered: %v", stop)
	}
	// The other tools and the unrelated settings survive.
	if !strings.Contains(strings.Join(commandsFor(document, "PostToolUse"), " "), "qoder-cli-hook.sh") {
		t.Error("r2c's PostToolUse hook was lost")
	}
	if document["enabledPlugins"] == nil {
		t.Error("unrelated Qoder settings were dropped")
	}

	if _, err := Uninstall(SourceQoder, home, atmBinary); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	after := readDocument(t, result.Path)
	for _, command := range commandsFor(after, "Stop") {
		if strings.Contains(command, "--source qoder") {
			t.Errorf("ATM's qoder hook survived uninstall: %q", command)
		}
	}
	if len(commandsFor(after, "Stop")) != 1 {
		t.Errorf("uninstall did not leave ping-island's Stop hook alone: %v", commandsFor(after, "Stop"))
	}
}

func TestPiHasNoHookConfigFile(t *testing.T) {
	// Pi is wired through a TypeScript extension file, not a hooks config, so the
	// installer must not pretend otherwise.
	if _, err := ConfigPath(SourcePi, t.TempDir()); err == nil {
		t.Error("expected no hook config path for pi")
	}
	if hooks := DesiredHooks(SourcePi); len(hooks) != 0 {
		t.Errorf("expected no config-file hooks for pi, got %v", hooks)
	}
}

func TestExecutablePathsWithSpacesAreQuoted(t *testing.T) {
	home := t.TempDir()
	binary := "/Applications/My Tools/atm"
	result, err := Install(SourceClaude, home, binary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	document := readDocument(t, result.Path)
	commands := commandsFor(document, "Stop")
	if len(commands) != 1 || !strings.HasPrefix(commands[0], `"/Applications/My Tools/atm"`) {
		t.Errorf("path with a space was not quoted: %v", commands)
	}
	// And the round trip still matches, so uninstall can find it again.
	if _, err := Uninstall(SourceClaude, home, binary); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := commandsFor(readDocument(t, result.Path), "Stop"); len(got) != 0 {
		t.Errorf("quoted command was not removed: %v", got)
	}
}

func TestGrokInstallWritesDedicatedHookFile(t *testing.T) {
	// Leave a sibling hook file alone — Grok merges every JSON under hooks/.
	home := writeHome(t, ".grok/hooks/other.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo other"}]}]}}`)

	result, err := Install(SourceGrokbuild, home, atmBinary)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantPath := filepath.Join(home, ".grok", "hooks", "atm-notch.json")
	if result.Path != wantPath {
		t.Fatalf("path = %q, want %q", result.Path, wantPath)
	}
	if !result.Changed() {
		t.Fatal("expected Install to create atm-notch.json")
	}

	document := readDocument(t, result.Path)
	runner := grokRunnerPath(home)
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("Grok runner missing: %v", err)
	}
	for _, spec := range DesiredHooks(SourceGrokbuild) {
		if !hasHook(document, spec, hookCommand(home, atmBinary, SourceGrokbuild, spec.Reason)) {
			t.Errorf("ATM hook for %s missing from Grok config", describe(spec))
		}
	}
	// Runner must point at the atm binary we installed.
	body, err := os.ReadFile(runner)
	if err != nil {
		t.Fatalf("ReadFile runner: %v", err)
	}
	if !strings.Contains(string(body), atmBinary) {
		t.Errorf("runner does not exec %s:\n%s", atmBinary, body)
	}

	// Sibling file is untouched.
	other, err := os.ReadFile(filepath.Join(home, ".grok", "hooks", "other.json"))
	if err != nil {
		t.Fatalf("ReadFile other: %v", err)
	}
	if !strings.Contains(string(other), "echo other") {
		t.Errorf("sibling hook file was rewritten: %s", other)
	}

	// Idempotent.
	second, err := Install(SourceGrokbuild, home, atmBinary)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if second.Changed() {
		t.Errorf("second Grok Install rewrote hooks: %+v", second)
	}

	// Uninstall deletes the dedicated file rather than leaving `{}`.
	removed, err := Uninstall(SourceGrokbuild, home, atmBinary)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(removed.Removed) != len(DesiredHooks(SourceGrokbuild)) {
		t.Errorf("removed %v, want every Grok hook", removed.Removed)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Errorf("atm-notch.json should be gone after uninstall, stat err=%v", err)
	}
	if _, err := os.Stat(runner); !os.IsNotExist(err) {
		t.Errorf("atm-notch-run.sh should be gone after uninstall, stat err=%v", err)
	}
	// Sibling still present.
	if _, err := os.Stat(filepath.Join(home, ".grok", "hooks", "other.json")); err != nil {
		t.Errorf("sibling hook file disappeared: %v", err)
	}
}

func TestGrokStatusReportsMissingWhenFileAbsent(t *testing.T) {
	home := t.TempDir()
	result, err := Status(SourceGrokbuild, home, atmBinary)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Kept) != 0 {
		t.Errorf("expected no installed hooks, got %v", result.Kept)
	}
	if len(result.Added) != len(DesiredHooks(SourceGrokbuild)) {
		t.Errorf("missing = %v, want every Grok hook", result.Added)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
