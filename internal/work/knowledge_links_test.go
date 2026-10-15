package work

import "testing"

func TestKnowledgeLinkAllowsOnlyStableInternalDocumentRoute(t *testing.T) {
	for _, id := range []string{"document:1", "中文 & + ? #"} {
		normalized, err := NormalizeTodoLinkURL(KnowledgeLinkURL(id))
		if err != nil || KnowledgeLinkID(normalized) != id {
			t.Fatalf("roundtrip %q: %q %v", id, normalized, err)
		}
	}
	for _, raw := range []string{"//evil.example/knowledge?document=x", "/knowledge?document=x&redirect=https://evil.example", "/knowledge?document=x&document=y", "/knowledge?document=..%2Fsecret", "/knowledge?document=x#token=secret", "/tasks/t1", "javascript:alert(1)"} {
		if _, err := NormalizeTodoLinkURL(raw); err == nil {
			t.Fatalf("accepted unsafe route %q", raw)
		}
	}
}
