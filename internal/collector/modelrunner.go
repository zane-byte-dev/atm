package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// ruleModelCommand names the local keyword classifier. It is not a CLI, so the
// runner layer skips it and the extractor handles it: on its own it means
// "classify with rules", and at the end of a chain it is the last resort when
// every model is unavailable.
const ruleModelCommand = "rule"

// Placeholders a runner template may use. A template that names no prompt
// placeholder receives the prompt on stdin.
const (
	schemaPathPlaceholder = "{{schema_path}}"
	schemaJSONPlaceholder = "{{schema_json}}"
	promptPathPlaceholder = "{{prompt_path}}"
	workdirPlaceholder    = "{{workdir}}"
)

// modelRunner is one resolved way to call a CLI for a schema-constrained answer.
type modelRunner struct {
	// Name is what the chain called this candidate; it labels failures.
	Name        string
	Command     string
	Args        []string
	OutputField string
	Timeout     time.Duration
}

// builtinModelRunners holds the CLIs ATM knows how to drive. The flags are the
// point, not decoration: this reads private chat, so each profile gives the
// model the least it can be given and still answer.
//
// codex is fully contained — ephemeral session, no user config, no rules file,
// read-only sandbox. grok has no equivalent of --ignore-user-config or
// --ignore-rules, so it still reads ~/.grok/config.toml; what can be denied
// (memory, subagents, web search, filesystem writes) is denied, and the scratch
// working directory holds no rules file to pick up. --verbatim matters more
// than it looks: without it a chat line starting with "/" or containing "@path"
// would be expanded as a command or a file reference, and these messages are
// untrusted by definition.
var builtinModelRunners = map[string]modelRunner{
	"codex": {Args: []string{"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules",
		"--skip-git-repo-check", "--sandbox", "read-only", "--output-schema", schemaPathPlaceholder, "-"}},
	"grok": {Args: []string{"--prompt-file", promptPathPlaceholder, "--verbatim",
		"--json-schema", schemaJSONPlaceholder, "--sandbox", "read-only",
		"--no-memory", "--no-subagents", "--disable-web-search"},
		OutputField: "structuredOutput"},
}

// splitModelCandidates separates the CLI candidates from the rule fallback, so
// the chain "grok,codex,rule" runs two models and only then degrades to keywords.
func splitModelCandidates(commandLine string) (models []string, ruleFallback bool) {
	for _, candidate := range config.CollectionModelCandidates(commandLine) {
		if strings.EqualFold(candidate, ruleModelCommand) {
			ruleFallback = true
			continue
		}
		models = append(models, candidate)
	}
	return models, ruleFallback
}

// CheckModelCommands splits the configured chain into what can run right now
// and what cannot. Collection fails in the background, so a chain whose every
// candidate is missing is worth saying out loud before the next run does it
// silently. "rule" always counts as runnable — it is local keyword matching.
func CheckModelCommands(commandLine string) (runnable, missing []string) {
	models, ruleFallback := splitModelCandidates(commandLine)
	for _, candidate := range models {
		runner, err := resolveModelRunner(candidate)
		if err != nil {
			missing = append(missing, candidate)
			continue
		}
		if _, err := exec.LookPath(runner.Command); err != nil {
			missing = append(missing, runner.Command)
			continue
		}
		runnable = append(runnable, runner.Command)
	}
	if ruleFallback {
		runnable = append(runnable, ruleModelCommand)
	}
	return runnable, missing
}

// resolveModelRunner turns one candidate into a runnable command. A candidate
// may carry its own flags ("grok --effort low"); they stay in front of the
// profile's flags, where a global flag has to be for both codex and grok.
func resolveModelRunner(candidate string) (modelRunner, error) {
	parts := strings.Fields(candidate)
	if len(parts) == 0 {
		return modelRunner{}, fmt.Errorf("collection model command is empty")
	}
	name := parts[0]
	runner := modelRunner{Name: name, Command: name}
	if custom, ok := lookupModelRunnerConfig(name); ok {
		if strings.TrimSpace(custom.Command) != "" {
			runner.Command = strings.TrimSpace(custom.Command)
		}
		runner.Args = append([]string{}, custom.Args...)
		runner.OutputField = strings.TrimSpace(custom.OutputField)
		if custom.TimeoutSeconds > 0 {
			runner.Timeout = time.Duration(custom.TimeoutSeconds) * time.Second
		}
	} else if builtin, ok := builtinModelRunners[strings.ToLower(filepath.Base(name))]; ok {
		runner.Args = append([]string{}, builtin.Args...)
		runner.OutputField = builtin.OutputField
	}
	runner.Command = expandModelCommandPath(runner.Command)
	runner.Args = append(append([]string{}, parts[1:]...), runner.Args...)
	return runner, nil
}

// lookupModelRunnerConfig matches a candidate against collection_model_runners
// by exact key first, then by base name, so both "grok" and an absolute path to
// the same binary pick up a user-defined profile.
func lookupModelRunnerConfig(name string) (config.ModelRunnerConfig, bool) {
	if len(config.CollectionModelRunners) == 0 {
		return config.ModelRunnerConfig{}, false
	}
	base := strings.ToLower(filepath.Base(name))
	var fallback config.ModelRunnerConfig
	found := false
	for key, runner := range config.CollectionModelRunners {
		key = strings.TrimSpace(key)
		if strings.EqualFold(key, name) {
			return runner, true
		}
		if !found && strings.EqualFold(filepath.Base(key), base) {
			fallback, found = runner, true
		}
	}
	return fallback, found
}

func expandModelCommandPath(command string) string {
	if strings.HasPrefix(command, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, command[2:])
		}
	}
	return command
}

// RunSchemaModel walks the configured candidate chain (or the one the caller
// passed) and returns the first schema-constrained JSON answer. Collection
// classification, daily digests and todo refine all use this so a new CLI is
// taught to ATM once. `rule` is not a model — callers that want a keyword
// fallback implement it themselves; refine has none, because a keyword cannot
// polish a title.
func RunSchemaModel(ctx context.Context, commandLine string, timeout time.Duration,
	schemaName, schema, prompt string) ([]byte, error) {
	if strings.TrimSpace(commandLine) == "" {
		commandLine = config.CollectionModelCommand
	}
	models, _ := splitModelCandidates(commandLine)
	if len(models) == 0 {
		return nil, fmt.Errorf("no model CLI configured (collection_model_command is %q)", strings.TrimSpace(commandLine))
	}
	return runCollectionModel(ctx, models, timeout, schemaName, schema, prompt)
}

// runCollectionModel walks the candidate chain and returns the first answer it
// gets. Falling through covers exactly the ways a CLI can be unusable through
// no fault of the prompt: not installed, rate limited, crashed, timed out, or
// answering in a shape ATM cannot read. Decoding the decision itself stays with
// the caller, where a bad answer is a hard failure — retrying that on another
// model just spends a second quota on the same mistake.
func runCollectionModel(ctx context.Context, candidates []string, timeout time.Duration,
	schemaName, schema, prompt string) ([]byte, error) {
	var failures []string
	for _, candidate := range candidates {
		runner, err := resolveModelRunner(candidate)
		if err != nil {
			failures = append(failures, shortModelFailure(candidate, err))
			continue
		}
		data, err := runner.run(ctx, timeout, schemaName, schema, prompt)
		if err == nil {
			return data, nil
		}
		failures = append(failures, shortModelFailure(runner.Name, err))
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("collection model command is empty")
	}
	return nil, fmt.Errorf("collection model failed: %s", strings.Join(failures, " | "))
}

// shortModelFailure keeps each attempt to one short line. A whole chain has to
// survive compactError's 160-rune budget, or the last candidate's reason — the
// one that actually matters — is the part that gets cut.
func shortModelFailure(name string, err error) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		name = filepath.Base(fields[0])
	}
	// Collapsed rather than cut at the first newline: a CLI that fails with a
	// pretty-printed JSON error puts the reason on the second line.
	message := strings.Join(strings.Fields(err.Error()), " ")
	if runes := []rune(message); len(runes) > 60 {
		message = string(runes[:60]) + "…"
	}
	return name + ": " + message
}

// run executes one CLI on a prompt with a JSON output schema. Every run gets a
// fresh scratch directory: it is the process working directory, the only place
// the schema and prompt files are written, and it carries the name the session
// parsers use to recognise and skip whatever the CLI persists for these runs.
func (runner modelRunner) run(ctx context.Context, timeout time.Duration,
	schemaName, schema, prompt string) ([]byte, error) {
	if _, err := exec.LookPath(runner.Command); err != nil {
		// The failure is already labelled with the candidate name, so only an
		// alias pointing at a different binary needs to name it again.
		if filepath.Base(runner.Command) == filepath.Base(runner.Name) {
			return nil, fmt.Errorf("not installed")
		}
		return nil, fmt.Errorf("not installed: %s", filepath.Base(runner.Command))
	}
	if runner.Timeout > 0 {
		timeout = runner.Timeout
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	workdir, err := os.MkdirTemp("", config.CollectionModelWorkdirPrefix)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workdir)
	defer pruneModelSessionArtifacts(workdir)
	args, promptOnStdin, err := runner.buildArgs(workdir, schemaName, schema, prompt)
	if err != nil {
		return nil, err
	}
	modelCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(modelCtx, runner.Command, args...)
	command.Dir = workdir
	if promptOnStdin {
		command.Stdin = strings.NewReader(prompt)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if modelCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timed out after %s", timeout)
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s", message)
	}
	return runner.decodeOutput(stdout.Bytes())
}

// pruneModelSessionArtifacts removes the session a CLI files for a scratch run.
// Skipping these during parsing keeps them out of ATM, but a classification
// every few minutes would still leave thousands of directories a year in
// somebody else's data. They exist only because ATM made the working directory
// seconds earlier, so ATM cleans them up.
//
// The scan is by the scratch directory's own unique name rather than by
// re-implementing each CLI's path encoding: an entry that embeds
// "atm-collection-model-<random>" can have no other origin.
func pruneModelSessionArtifacts(workdir string) {
	marker := filepath.Base(workdir)
	if !strings.HasPrefix(marker, config.CollectionModelWorkdirPrefix) ||
		marker == config.CollectionModelWorkdirPrefix {
		return
	}
	// The session roots that key a session by working directory.
	for _, root := range []string{config.GrokSessions, config.ClaudeProjects} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.Contains(entry.Name(), marker) {
				os.RemoveAll(filepath.Join(root, entry.Name()))
			}
		}
	}
}

// buildArgs instantiates the template. Schema and prompt files are written only
// when the template asks for them, so a CLI that reads stdin never finds a
// stray file in its working directory.
func (runner modelRunner) buildArgs(workdir, schemaName, schema, prompt string) ([]string, bool, error) {
	schemaPath, promptPath := "", ""
	needsSchemaFile, needsPromptFile := false, false
	for _, arg := range runner.Args {
		if strings.Contains(arg, schemaPathPlaceholder) {
			needsSchemaFile = true
		}
		if strings.Contains(arg, promptPathPlaceholder) {
			needsPromptFile = true
		}
	}
	if needsSchemaFile {
		schemaPath = filepath.Join(workdir, schemaName+".schema.json")
		if err := os.WriteFile(schemaPath, []byte(schema), 0600); err != nil {
			return nil, false, err
		}
	}
	if needsPromptFile {
		promptPath = filepath.Join(workdir, schemaName+".prompt.txt")
		if err := os.WriteFile(promptPath, []byte(prompt), 0600); err != nil {
			return nil, false, err
		}
	}
	args := make([]string, 0, len(runner.Args))
	for _, arg := range runner.Args {
		arg = strings.ReplaceAll(arg, schemaPathPlaceholder, schemaPath)
		arg = strings.ReplaceAll(arg, schemaJSONPlaceholder, schema)
		arg = strings.ReplaceAll(arg, promptPathPlaceholder, promptPath)
		arg = strings.ReplaceAll(arg, workdirPlaceholder, workdir)
		args = append(args, arg)
	}
	return args, !needsPromptFile, nil
}

// decodeOutput narrows stdout to the JSON object the caller asked for. CLIs
// that print the object directly need no field; the ones that wrap it in a run
// envelope name the field holding it, with "text" as the common second place a
// structured answer shows up.
func (runner modelRunner) decodeOutput(stdout []byte) ([]byte, error) {
	data := trimJSONFences(stdout)
	if runner.OutputField == "" {
		return data, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("output is not a JSON envelope")
	}
	for _, field := range []string{runner.OutputField, "text"} {
		raw, ok := envelope[field]
		if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return trimJSONFences([]byte(text)), nil
		}
		return bytes.TrimSpace(raw), nil
	}
	return nil, fmt.Errorf("output has no %s", runner.OutputField)
}

func trimJSONFences(data []byte) []byte {
	data = bytes.TrimSpace(data)
	data = bytes.TrimPrefix(data, []byte("```json"))
	data = bytes.TrimPrefix(data, []byte("```"))
	data = bytes.TrimSuffix(data, []byte("```"))
	return bytes.TrimSpace(data)
}
