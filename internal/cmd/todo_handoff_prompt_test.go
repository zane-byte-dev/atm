package cmd

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// The pointer has to survive being pasted into an agent that knows nothing else,
// so every part of it is load-bearing: the canonical ID because todo lookups are
// exact matches, and `todo doc` because `todo show` prints only the description.
func TestTodoPromptNamesCanonicalIDAndTheCommandsThatLoadTheTask(t *testing.T) {
	todo := store.Todo{ID: "t89", Title: "Replace dispatch with a handoff", Project: "atm"}

	prompt := buildTodoPrompt(&todo)

	for _, want := range []string{
		"t89",
		"Replace dispatch with a handoff",
		"atm todo doc t89",
		"atm session bind t89",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "#t89") || strings.Contains(prompt, "#89") {
		t.Fatalf("prompt uses a shorthand the CLI cannot resolve:\n%s", prompt)
	}
}
