package store

import "testing"

func TestAgentDisplayNameIncludesQoderVariants(t *testing.T) {
	cases := map[string]string{"qoder": "Qoder", "qodercli": "Qoder CLI", "qoderwork": "QoderWork", "grokbuild": "Grok Build"}
	for agent, want := range cases {
		if got := AgentDisplayName(agent); got != want {
			t.Fatalf("AgentDisplayName(%q) = %q, want %q", agent, got, want)
		}
	}
}

// The two copies of this switch disagreed about pi and about case, so both now
// have to come out of the single one.
func TestAgentDisplayNameKnowsPiAndFoldsCase(t *testing.T) {
	for _, agent := range []string{"pi", "Pi", "PI"} {
		if got := AgentDisplayName(agent); got != "Pi" {
			t.Fatalf("AgentDisplayName(%q) = %q, want %q", agent, got, "Pi")
		}
	}
	if got := AgentDisplayName("Claude"); got != "Claude Code" {
		t.Fatalf("AgentDisplayName(\"Claude\") = %q", got)
	}
}
