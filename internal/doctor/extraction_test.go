package doctor

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// The extraction checks exist to catch an upstream format change, which does not
// announce itself: discovery still finds files, sessions are still created, and
// the fields the parser reads are simply gone. These tests pin the four states
// that decision has to distinguish.
func TestExtractionIssuesReportsAParserThatGotNothing(t *testing.T) {
	issues := extractionIssues("claude",
		Source{Files: 12},
		store.ExtractionCounts{Agent: "claude", Sessions: 12, Messages: 0, UsageRows: 0})

	codes := issueCodes(issues)
	if !codes["parser_extracts_no_messages"] {
		t.Errorf("no message-extraction issue: %v", codes)
	}
	if !codes["parser_extracts_no_usage"] {
		t.Errorf("no usage-extraction issue: %v", codes)
	}
	for _, issue := range issues {
		if issue.Severity != "warning" {
			t.Errorf("%s reported as %s, want warning", issue.Code, issue.Severity)
		}
		// The suggestion is the whole value of the check: it has to say "your
		// version may be behind" and give the reporting path.
		if !strings.Contains(issue.Suggestion, "atm diagnose --bundle") {
			t.Errorf("%s does not tell the user how to report it: %s", issue.Code, issue.Suggestion)
		}
	}
}

// An agent nobody has used has zero of everything. Reporting that as a parser
// regression is how a diagnostic becomes noise.
func TestExtractionIssuesStaysQuietForAnUnusedAgent(t *testing.T) {
	if issues := extractionIssues("claude", Source{Files: 0}, store.ExtractionCounts{}); len(issues) != 0 {
		t.Errorf("issues for an agent with no files: %v", issues)
	}
	if issues := extractionIssues("claude", Source{Files: 5}, store.ExtractionCounts{Sessions: 0}); len(issues) != 0 {
		t.Errorf("issues for an agent with no indexed sessions: %v", issues)
	}
}

// Copilot's upstream has never recorded tokens. Flagging that would be flagging
// the upstream's design, and it would fire on every single run.
func TestExtractionIssuesRespectsDeclaredCapabilities(t *testing.T) {
	issues := extractionIssues("copilot",
		Source{Files: 76},
		store.ExtractionCounts{Agent: "copilot", Sessions: 76, Messages: 853, UsageRows: 0})
	if len(issues) != 0 {
		t.Fatalf("copilot's documented lack of token detail was reported as a problem: %v", issues)
	}
}

// The half-broken case is the realistic one: messages still parse, token
// accounting stops. Only the affected half may be reported.
//
// Uses claude because the agent has to be one that claims usage — qoderwork was
// the original example until it was confirmed that its upstream never fills the
// token fields, at which point it stopped claiming it.
func TestExtractionIssuesReportsOnlyTheMissingHalf(t *testing.T) {
	issues := extractionIssues("claude",
		Source{Files: 34},
		store.ExtractionCounts{Agent: "claude", Sessions: 34, Messages: 318, UsageRows: 0})

	codes := issueCodes(issues)
	if codes["parser_extracts_no_messages"] {
		t.Error("reported missing messages when 318 were extracted")
	}
	if !codes["parser_extracts_no_usage"] {
		t.Errorf("missing token accounting was not reported: %v", codes)
	}
}

func issueCodes(issues []Issue) map[string]bool {
	codes := map[string]bool{}
	for _, issue := range issues {
		codes[issue.Code] = true
	}
	return codes
}
