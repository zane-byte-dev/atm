package collector

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func withModelRunners(t *testing.T, runners map[string]config.ModelRunnerConfig) {
	t.Helper()
	old := config.CollectionModelRunners
	t.Cleanup(func() { config.CollectionModelRunners = old })
	config.CollectionModelRunners = runners
}

// writeFakeModel writes an executable that stands in for a CLI. body is shell.
func writeFakeModel(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake model %s: %v", name, err)
	}
	return path
}

func TestSplitModelCandidatesSeparatesRuleFallback(t *testing.T) {
	models, ruleFallback := splitModelCandidates("grok, codex ,rule,")
	if !slices.Equal(models, []string{"grok", "codex"}) || !ruleFallback {
		t.Fatalf("models=%#v ruleFallback=%v", models, ruleFallback)
	}
	models, ruleFallback = splitModelCandidates("RULE")
	if len(models) != 0 || !ruleFallback {
		t.Fatalf("rule-only chain: models=%#v ruleFallback=%v", models, ruleFallback)
	}
}

func TestResolveModelRunnerUsesBuiltinProfiles(t *testing.T) {
	withModelRunners(t, nil)
	codex, err := resolveModelRunner("codex")
	if err != nil {
		t.Fatalf("resolve codex: %v", err)
	}
	if !slices.Contains(codex.Args, "--ephemeral") || !slices.Contains(codex.Args, schemaPathPlaceholder) ||
		codex.OutputField != "" {
		t.Fatalf("codex runner = %#v", codex)
	}
	grok, err := resolveModelRunner("grok --effort low")
	if err != nil {
		t.Fatalf("resolve grok: %v", err)
	}
	// A candidate's own flags stay in front, where both CLIs expect globals.
	if !slices.Equal(grok.Args[:2], []string{"--effort", "low"}) {
		t.Fatalf("grok args = %#v", grok.Args)
	}
	if !slices.Contains(grok.Args, "--verbatim") || !slices.Contains(grok.Args, promptPathPlaceholder) ||
		grok.OutputField != "structuredOutput" {
		t.Fatalf("grok runner = %#v", grok)
	}
	// An unknown CLI gets no flags invented for it: prompt on stdin, raw stdout.
	other, err := resolveModelRunner("my-agent-cli")
	if err != nil {
		t.Fatalf("resolve unknown: %v", err)
	}
	if len(other.Args) != 0 || other.OutputField != "" {
		t.Fatalf("unknown runner = %#v", other)
	}
}

func TestResolveModelRunnerAppliesConfiguredRunner(t *testing.T) {
	t.Setenv("HOME", "/home/example")
	withModelRunners(t, map[string]config.ModelRunnerConfig{
		"grok-fast": {Command: "~/.grok/bin/grok", Args: []string{"-m", "grok-4-fast", "--json-schema", schemaJSONPlaceholder},
			OutputField: "structuredOutput", TimeoutSeconds: 45},
		// Same key as a built-in: the user's profile wins.
		"codex": {Args: []string{"--custom"}},
	})
	alias, err := resolveModelRunner("grok-fast")
	if err != nil {
		t.Fatalf("resolve alias: %v", err)
	}
	if alias.Command != "/home/example/.grok/bin/grok" || alias.Timeout != 45*time.Second ||
		!slices.Equal(alias.Args, []string{"-m", "grok-4-fast", "--json-schema", schemaJSONPlaceholder}) {
		t.Fatalf("alias runner = %#v", alias)
	}
	codex, err := resolveModelRunner("codex")
	if err != nil {
		t.Fatalf("resolve overridden codex: %v", err)
	}
	if !slices.Equal(codex.Args, []string{"--custom"}) {
		t.Fatalf("configured runner did not override the built-in: %#v", codex.Args)
	}
}

func TestBuildArgsInstantiatesPlaceholders(t *testing.T) {
	workdir := t.TempDir()
	runner := modelRunner{Args: []string{"--prompt-file", promptPathPlaceholder, "--json-schema",
		schemaJSONPlaceholder, "--cwd", workdirPlaceholder}}
	args, promptOnStdin, err := runner.buildArgs(workdir, "decision", `{"type":"object"}`, "分类这段聊天")
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	if promptOnStdin {
		t.Fatal("a template naming a prompt file must not also get stdin")
	}
	if args[1] != filepath.Join(workdir, "decision.prompt.txt") || args[3] != `{"type":"object"}` ||
		args[5] != workdir {
		t.Fatalf("args = %#v", args)
	}
	prompt, err := os.ReadFile(args[1])
	if err != nil || string(prompt) != "分类这段聊天" {
		t.Fatalf("prompt file = %q err=%v", prompt, err)
	}
	// No schema placeholder means no schema file left in the working directory.
	if _, err := os.Stat(filepath.Join(workdir, "decision.schema.json")); err == nil {
		t.Fatal("schema file written for a template that never asked for it")
	}

	stdinRunner := modelRunner{Args: []string{"--output-schema", schemaPathPlaceholder, "-"}}
	args, promptOnStdin, err = stdinRunner.buildArgs(workdir, "decision", `{"type":"object"}`, "x")
	if err != nil || !promptOnStdin {
		t.Fatalf("stdin runner: args=%#v promptOnStdin=%v err=%v", args, promptOnStdin, err)
	}
	schema, err := os.ReadFile(filepath.Join(workdir, "decision.schema.json"))
	if err != nil || string(schema) != `{"type":"object"}` {
		t.Fatalf("schema file = %q err=%v", schema, err)
	}
}

func TestDecodeOutputUnwrapsEnvelopes(t *testing.T) {
	raw := modelRunner{}
	data, err := raw.decodeOutput([]byte("```json\n{\"action\":\"ignore\"}\n```"))
	if err != nil || string(data) != `{"action":"ignore"}` {
		t.Fatalf("fenced stdout = %q err=%v", data, err)
	}

	grok := modelRunner{OutputField: "structuredOutput"}
	envelope := `{"text":"{\"action\":\"create\"}","structuredOutput":{"action":"ignore"},"usage":{}}`
	data, err = grok.decodeOutput([]byte(envelope))
	if err != nil || string(data) != `{"action":"ignore"}` {
		t.Fatalf("envelope = %q err=%v", data, err)
	}
	// Structured output missing: the same answer usually arrives as text.
	data, err = grok.decodeOutput([]byte(`{"text":"{\"action\":\"create\"}","structuredOutput":null}`))
	if err != nil || string(data) != `{"action":"create"}` {
		t.Fatalf("text fallback = %q err=%v", data, err)
	}
	if _, err = grok.decodeOutput([]byte(`{"usage":{}}`)); err == nil {
		t.Fatal("envelope without any answer field should fail")
	}
	if _, err = grok.decodeOutput([]byte("not json")); err == nil {
		t.Fatal("non-JSON stdout should fail for an envelope runner")
	}
}

func TestRunCollectionModelFallsThroughToTheNextCandidate(t *testing.T) {
	withModelRunners(t, nil)
	rateLimited := writeFakeModel(t, "rate-limited", "echo 'You have hit your usage limit.' >&2\nexit 1\n")
	working := writeFakeModel(t, "working", `printf '%s' '{"action":"ignore"}'`)
	data, err := runCollectionModel(context.Background(),
		[]string{filepath.Join(t.TempDir(), "not-installed"), rateLimited, working},
		5*time.Second, "decision", `{"type":"object"}`, "prompt")
	if err != nil || string(data) != `{"action":"ignore"}` {
		t.Fatalf("chain result = %q err=%v", data, err)
	}
}

func TestRunCollectionModelReportsEveryFailedCandidate(t *testing.T) {
	withModelRunners(t, nil)
	rateLimited := writeFakeModel(t, "rate-limited", "echo 'usage limit reached' >&2\nexit 1\n")
	missing := filepath.Join(t.TempDir(), "not-installed")
	_, err := runCollectionModel(context.Background(), []string{rateLimited, missing},
		5*time.Second, "decision", `{"type":"object"}`, "prompt")
	if err == nil {
		t.Fatal("a chain where every candidate fails must fail")
	}
	message := err.Error()
	if !strings.Contains(message, "rate-limited: usage limit reached") ||
		!strings.Contains(message, "not-installed: not installed") {
		t.Fatalf("chain error should name both failures: %s", message)
	}
	// The whole chain still has to fit compactError's budget for run records.
	if len([]rune(compactError(err))) > 160 {
		t.Fatalf("compacted chain error too long: %s", compactError(err))
	}
}

func TestRunCollectionModelUsesConfiguredRunnerArgs(t *testing.T) {
	// A CLI ATM has no profile for: config supplies argv and the output field,
	// and the prompt reaches it as a file rather than on stdin.
	echo := writeFakeModel(t, "envelope-cli", `printf '{"result":{"prompt":"%s"}}' "$(cat "$2")"`)
	withModelRunners(t, map[string]config.ModelRunnerConfig{
		"house-model": {Command: echo, Args: []string{"--prompt-file", promptPathPlaceholder,
			"--schema", schemaJSONPlaceholder}, OutputField: "result"},
	})
	data, err := runCollectionModel(context.Background(), []string{"house-model"}, 5*time.Second,
		"decision", `{"type":"object"}`, "hello")
	if err != nil || string(data) != `{"prompt":"hello"}` {
		t.Fatalf("configured runner output = %q err=%v", data, err)
	}
}

func TestPruneModelSessionArtifactsRemovesOnlyScratchSessions(t *testing.T) {
	root := t.TempDir()
	oldSessions, oldProjects := config.GrokSessions, config.ClaudeProjects
	t.Cleanup(func() { config.GrokSessions, config.ClaudeProjects = oldSessions, oldProjects })
	config.GrokSessions = root
	config.ClaudeProjects = filepath.Join(t.TempDir(), "missing")

	workdir := filepath.Join(t.TempDir(), config.CollectionModelWorkdirPrefix+"2291227821")
	scratch := filepath.Join(root, "%2Fprivate%2Fvar%2FT%2F"+filepath.Base(workdir), "session-abc")
	real := filepath.Join(root, "%2FUsers%2Fmj%2Fmox%2Fatm", "session-def")
	otherRun := filepath.Join(root, "%2Fvar%2FT%2F"+config.CollectionModelWorkdirPrefix+"999", "session-ghi")
	for _, dir := range []string{scratch, real, otherRun} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pruneModelSessionArtifacts(workdir)
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch session survived: %v", err)
	}
	// Another run's scratch directory is not this run's to delete, and a real
	// working directory is nobody's.
	for _, dir := range []string{real, otherRun} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s was removed: %v", dir, err)
		}
	}
	// A path that is not a scratch directory must never trigger a sweep.
	pruneModelSessionArtifacts(filepath.Join(t.TempDir(), "atm"))
	if _, err := os.Stat(real); err != nil {
		t.Fatalf("non-scratch workdir swept sessions: %v", err)
	}
}

func TestCheckModelCommandsSeparatesRunnableFromMissing(t *testing.T) {
	withModelRunners(t, nil)
	working := writeFakeModel(t, "working", "exit 0")
	missing := filepath.Join(t.TempDir(), "not-installed")
	runnable, unavailable := CheckModelCommands(working + "," + missing + ",rule")
	if !slices.Equal(runnable, []string{working, ruleModelCommand}) || !slices.Equal(unavailable, []string{missing}) {
		t.Fatalf("runnable=%#v missing=%#v", runnable, unavailable)
	}
}
