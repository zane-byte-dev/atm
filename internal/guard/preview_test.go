package guard

import (
	"strings"
	"testing"
)

func TestPreviewReadsTheDingTalkGroupAndBody(t *testing.T) {
	match := find(t, "dws", "chat", "message", "send", "--group", "cid123",
		"--title", "发布完成", "--text", "已发布到预发", "-y")
	if match == nil {
		t.Fatal("not gated")
	}
	if match.Target != "cid123" || match.Title != "发布完成" || match.Body != "已发布到预发" {
		t.Fatalf("preview = %q / %q / %q", match.Target, match.Title, match.Body)
	}
}

func TestPreviewReadsEqualsSeparatedFlags(t *testing.T) {
	match := find(t, "dws", "chat", "message", "send", "--group=cid123", "--text=你好", "-y")
	if match == nil {
		t.Fatal("not gated")
	}
	if match.Target != "cid123" || match.Body != "你好" {
		t.Fatalf("preview = %q / %q", match.Target, match.Body)
	}
}

// The user is deciding whether to send to a person or a group; whichever flag was
// used has to reach the card.
func TestPreviewFallsThroughFlagAlternatives(t *testing.T) {
	match := find(t, "dws", "chat", "message", "send", "--user", "u456", "--text", "hi", "-y")
	if match == nil || match.Target != "u456" {
		t.Fatalf("target = %+v", match)
	}
}

func TestPreviewReadsAPositionalArgument(t *testing.T) {
	match := find(t, "a1", "repo", "mr", "remind", "123")
	if match == nil {
		t.Fatal("not gated")
	}
	if match.Target != "123" {
		t.Fatalf("target = %q, want the MR id", match.Target)
	}
}

func TestPreviewReadsADottedPathOutOfAJSONArgument(t *testing.T) {
	match := find(t, "aone-kit", "call-tool", "ata::message-ding-talk-send-to-webhook",
		`{"fieldName_0":{"webhook":"https://oapi.dingtalk.com/robot/send?access_token=abc123","title":"周报","markdownContent":"## 本周\n做完了"}}`,
		"--provider", "zetta")
	if match == nil {
		t.Fatal("not gated")
	}
	if match.Title != "周报" || !strings.Contains(match.Body, "做完了") {
		t.Fatalf("title = %q body = %q", match.Title, match.Body)
	}
	if strings.Contains(match.Target, "abc123") {
		t.Fatalf("target %q still carries the access token", match.Target)
	}
	if !strings.Contains(match.Target, "oapi.dingtalk.com") {
		t.Fatalf("target %q no longer identifies which group is being pushed to", match.Target)
	}
}

// Extraction is presentation. A command whose preview cannot be built is still
// gated — the opposite would make an unparseable argument a way through.
func TestMissingPreviewStillGates(t *testing.T) {
	match := find(t, "dws", "chat", "message", "send", "-y")
	if match == nil {
		t.Fatal("a send with no readable arguments was let through")
	}
	if match.Target != "" || match.Body != "" {
		t.Fatalf("invented a preview: %+v", match)
	}

	match = find(t, "aone-kit", "call-tool", "ata::message-ding-talk-send-to-webhook",
		"not json at all", "--provider", "zetta")
	if match == nil {
		t.Fatal("an unparseable JSON argument was let through")
	}
	if match.Body != "" {
		t.Fatalf("body = %q, want empty rather than invented", match.Body)
	}
}

func TestRedactSecretsMasksCredentialsAndKeepsTheRest(t *testing.T) {
	cases := map[string]string{
		"https://oapi.dingtalk.com/robot/send?access_token=abc123": "https://oapi.dingtalk.com/robot/send?access_token=…",
		"https://h/p?token=xyz&id=7":                               "https://h/p?token=…&id=7",
		"https://h/p?ACCESS_TOKEN=XYZ":                             "https://h/p?ACCESS_TOKEN=…",
		"https://h/p?id=7&secret=s1&sign=s2":                       "https://h/p?id=7&secret=…&sign=…",
		"没有凭证的普通正文":                                                "没有凭证的普通正文",
		"tokens=are-not-token":                                     "tokens=are-not-token",
		"monkey=banana":                                            "monkey=banana",
	}
	for input, want := range cases {
		if got := RedactSecrets(input); got != want {
			t.Errorf("RedactSecrets(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRedactedCommandQuotesWhatNeedsIt(t *testing.T) {
	rendered := RedactedCommand("dws", []string{"chat", "message", "send",
		"--text", "有 空格\n和换行", "-y"})
	if !strings.HasPrefix(rendered, "dws chat message send --text ") {
		t.Fatalf("rendered = %q", rendered)
	}
	if !strings.Contains(rendered, "'有 空格") {
		t.Fatalf("rendered = %q, want the multi-word argument quoted", rendered)
	}
	if !strings.HasSuffix(rendered, " -y") {
		t.Fatalf("rendered = %q", rendered)
	}
}
