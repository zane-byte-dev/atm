package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex nests each record under `payload` while Grok writes the same reasoning
// shape at the top level. Both were previously read with Claude's extractor,
// which looks for a `thinking` content block and therefore returned nothing —
// `session show --thinking` presented two of the four agents as never thinking.
func TestReasoningExtractThinkingReadsCodexAndGrokShapes(t *testing.T) {
	for _, test := range []struct {
		name  string
		lines []string
	}{
		{
			name: "codex payload wrapped",
			lines: []string{
				`{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"**Planning the fix**"}]}}`,
				`{"type":"response_item","payload":{"type":"reasoning","summary":[],"encrypted_content":"opaque"}}`,
				`{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"**Checking the tests**"}]}}`,
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"改完了"}]}}`,
			},
		},
		{
			name: "grok top level",
			lines: []string{
				`{"type":"reasoning","summary":[{"type":"summary_text","text":"**Planning the fix**"}]}`,
				`{"type":"reasoning","summary":[{"type":"summary_text","text":"**Checking the tests**"}]}`,
				`{"type":"assistant","content":"改完了","tool_calls":[]}`,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(test.lines, "\n")+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			blocks := ReasoningExtractThinking(path)
			// Everything before the visible reply is one turn's thinking, and the
			// encrypted-only item contributes nothing rather than a blank line.
			if len(blocks) != 1 {
				t.Fatalf("blocks = %#v", blocks)
			}
			if !strings.Contains(blocks[0].Thinking, "Planning the fix") ||
				!strings.Contains(blocks[0].Thinking, "Checking the tests") {
				t.Fatalf("thinking = %q", blocks[0].Thinking)
			}
			if blocks[0].Response != "改完了" {
				t.Fatalf("response = %q", blocks[0].Response)
			}
		})
	}
}

// A turn that ends on a tool call still thought, and dropping it would lose the
// reasoning behind the last action taken.
func TestReasoningExtractThinkingKeepsAnUnansweredTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	body := `{"type":"reasoning","summary":[{"type":"summary_text","text":"first"}]}` + "\n" +
		`{"type":"assistant","content":"读一下文件"}` + "\n" +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"second"}]}` + "\n" +
		`{"type":"assistant","content":"","tool_calls":[{"name":"read_file"}]}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	blocks := ReasoningExtractThinking(path)
	if len(blocks) != 2 || blocks[0].Response != "读一下文件" || blocks[1].Thinking != "second" ||
		blocks[1].Response != "" {
		t.Fatalf("blocks = %#v", blocks)
	}
}
