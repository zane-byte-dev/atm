package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

// The corpus in testdata/ is one complete session per agent, in that agent's real
// transcript shape, with every human-authored value replaced by a PLACEHOLDER_
// token. It exists for a failure the per-behaviour tests in this package cannot
// catch: each of those asserts one narrow thing against a minimal fixture written
// to satisfy it, so an upstream format change that breaks extraction wholesale
// still leaves them green. These assert the opposite direction — a whole session
// goes in, and every field ATM's features depend on has to come out non-empty.
//
// They deliberately assert presence rather than exact values. Pinning numbers
// here would duplicate the behaviour tests and make the corpus expensive to
// refresh, which is how a corpus stops being refreshed.
//
// Samples carry no real content. That is not only a privacy rule for a public
// repository: a corpus containing someone's actual prompts cannot be committed,
// and one that cannot be committed does not run in CI.

// corpusExpectation is what a session must yield for ATM's features to work at
// all: sessions list, search, transcripts, tool stats, spend.
type corpusExpectation struct {
	agent string
	// parse loads the sample and returns what the parser made of it.
	parse func(t *testing.T) *ParsedFile
	// wantUsage is false only for upstreams that do not report tokens; it mirrors
	// CapabilitiesFor, and a mismatch between the two is itself a bug.
	wantTools  bool
	wantSkills bool
}

func corpusPath(name string) string {
	return filepath.Join("testdata", name)
}

// stageCorpusFile copies a sample into the directory layout its parser derives
// project and session identity from. The layout is part of the format, so the
// corpus has to be read through it rather than from testdata directly.
func stageCorpusFile(t *testing.T, sample, relative string) string {
	t.Helper()
	data, err := os.ReadFile(corpusPath(sample))
	if err != nil {
		t.Fatalf("read corpus %s: %v", sample, err)
	}
	target := filepath.Join(t.TempDir(), relative)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", relative, err)
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
	return target
}

func corpusExpectations() []corpusExpectation {
	return []corpusExpectation{
		{
			agent:      "claude",
			wantTools:  true,
			wantSkills: true,
			parse: func(t *testing.T) *ParsedFile {
				return ClaudeParseFile(stageCorpusFile(t, "claude.jsonl",
					filepath.Join("corpus-project", "0123456789abcdef.jsonl")))
			},
		},
		{
			agent:     "codex",
			wantTools: true,
			// Codex records skill use as a shell command, which this sample includes.
			wantSkills: true,
			parse: func(t *testing.T) *ParsedFile {
				return CodexParseFile(stageCorpusFile(t, "codex.jsonl",
					filepath.Join("2026", "08", "01", "rollout-corpus000001.jsonl")))
			},
		},
		{
			agent:      "pi",
			wantTools:  true,
			wantSkills: true,
			parse: func(t *testing.T) *ParsedFile {
				return PiParseFile(stageCorpusFile(t, "pi.jsonl",
					filepath.Join("--PLACEHOLDER-corpus-project--", "2026-08-01_corpus01.jsonl")))
			},
		},
		{
			agent:      "qodercli",
			wantTools:  true,
			wantSkills: true,
			parse: func(t *testing.T) *ParsedFile {
				path := stageCorpusFile(t, "qodercli.jsonl",
					filepath.Join("-PLACEHOLDER-corpus-project", "transcript", "corpus01-qcli.jsonl"))
				old := config.QoderCLIProjects
				config.QoderCLIProjects = filepath.Dir(filepath.Dir(path))
				t.Cleanup(func() { config.QoderCLIProjects = old })
				return QoderCLIParseFile(path)
			},
		},
		{
			agent:      "grokbuild",
			wantTools:  true,
			wantSkills: true,
			parse: func(t *testing.T) *ParsedFile {
				// Grok spreads one session over three sibling files, so the whole
				// directory is staged rather than a single file.
				root := t.TempDir()
				source := filepath.Join("testdata", "grok")
				if err := copyTree(source, root); err != nil {
					t.Fatalf("stage grok corpus: %v", err)
				}
				matches, err := filepath.Glob(filepath.Join(root, "*", "*", "chat_history.jsonl"))
				if err != nil || len(matches) != 1 {
					t.Fatalf("expected one staged grok session, got %v (err=%v)", matches, err)
				}
				return GrokParseFile(matches[0])
			},
		},
	}
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0644)
	})
}

// TestCorpusExtractsEveryFieldATMDependsOn is the regression that fires when an
// upstream format changes: not "this one field is wrong" but "this parser has
// stopped producing anything usable".
func TestCorpusExtractsEveryFieldATMDependsOn(t *testing.T) {
	for _, expectation := range corpusExpectations() {
		t.Run(expectation.agent, func(t *testing.T) {
			parsed := expectation.parse(t)
			if parsed == nil {
				t.Fatalf("%s returned nil for its own corpus sample — the parser no longer reads this format",
					expectation.agent)
			}
			if parsed.SessionID == "" {
				t.Error("session id is empty: the session cannot be indexed or referenced")
			}
			if parsed.Project == "" {
				t.Error("project is empty: the session cannot be grouped or filtered")
			}
			if parsed.CreatedTS == 0 || parsed.LastTS == 0 {
				t.Errorf("timestamps missing (created=%d last=%d): the session cannot be ordered or windowed",
					parsed.CreatedTS, parsed.LastTS)
			}
			if len(parsed.Inputs) == 0 {
				t.Error("no user messages: search and transcripts would be empty")
			}
			if len(parsed.Outputs) == 0 {
				t.Error("no assistant messages: search and transcripts would be empty")
			}
			if expectation.wantTools && len(parsed.Tools) == 0 {
				t.Error("no tool calls: `atm stats --by skill` and tool counts would read as zero")
			}
			if expectation.wantSkills && len(parsed.Skills) == 0 {
				t.Error("no skill events: skill statistics would read as zero")
			}

			claims := CapabilitiesFor(expectation.agent)
			if claims.Usage {
				total := parsed.Usage.InputTokens + parsed.Usage.OutputTokens +
					parsed.Usage.CacheReadTokens + parsed.Usage.CacheCreateTokens
				if total == 0 && len(parsed.UsageEvents) == 0 {
					t.Error("no token accounting: this agent's spend would read as zero while it claims to report usage")
				}
			}
		})
	}
}

// The corpus is only committable, and therefore only runnable in CI, if it holds
// no real content. This asserts the property rather than trusting review.
func TestCorpusHoldsNoRealContent(t *testing.T) {
	err := filepath.Walk("testdata", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		// Absolute home-style paths carry a username; the corpus uses /PLACEHOLDER.
		for _, forbidden := range []string{"/Users/", "/home/", "/root/"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains a real-looking home path %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
}

// A capability declared in capabilities.go but absent from the corpus would make
// the usage assertion above vacuous for that agent.
func TestCorpusCoversEveryRegisteredAgentThatCanBeSampled(t *testing.T) {
	sampled := map[string]bool{}
	for _, expectation := range corpusExpectations() {
		sampled[expectation.agent] = true
	}
	// copilot, qoder and qoderwork read SQLite databases rather than transcript
	// files. A committed binary .db cannot be reviewed in a diff, so their
	// fixtures are built with SQL in qoder_test.go and parser_test.go instead.
	sqliteBacked := map[string]bool{"copilot": true, "qoder": true, "qoderwork": true}
	for _, agent := range All() {
		name := agent.Name()
		if sampled[name] || sqliteBacked[name] {
			continue
		}
		t.Errorf("%s has neither a corpus sample nor a documented reason to lack one", name)
	}
}
