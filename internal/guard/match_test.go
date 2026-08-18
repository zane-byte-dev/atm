package guard

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func find(t *testing.T, tool string, argv ...string) *Match {
	t.Helper()
	match, err := Find(argv, Rules(tool))
	if err != nil {
		t.Fatalf("%s %v: %v", tool, argv, err)
	}
	return match
}

// The commands below are taken verbatim from the skills that drive these CLIs:
// ~/.agents/skills/{dingtalk,mr,a1,ata-all}. Every one of them is something an
// agent runs routinely, and gating any of them is the failure that would get the
// whole gate removed.
func TestEveryDocumentedReadCommandIsNotGated(t *testing.T) {
	reads := []struct {
		tool string
		argv []string
	}{
		{"dws", []string{"chat", "message", "list", "--group", "g1", "--limit", "50", "-f", "json"}},
		{"dws", []string{"chat", "message", "list-all", "--start", "2026-07-01T00:00:00+08:00",
			"--end", "2026-07-01T23:59:59+08:00", "--limit", "50", "-f", "json"}},
		{"dws", []string{"chat", "search", "--keyword", "发布", "-f", "json"}},
		{"dws", []string{"aisearch", "person", "--keyword", "张三", "--dimension", "name", "-f", "json"}},
		{"a1", []string{"repo", "mr", "list", "--mine", "review"}},
		{"a1", []string{"repo", "mr", "view", "123"}},
		{"a1", []string{"repo", "mr", "status", "123"}},
		{"a1", []string{"repo", "mr", "comment", "list", "--mr", "123"}},
		{"a1", []string{"repo", "mr", "reviewers"}},
		{"a1", []string{"workitem", "list", "--mine"}},
		{"aone-kit", []string{"call-tool", "ata::article-list-query", `{"fieldName_0":{"page":1}}`,
			"--provider", "zetta"}},
		{"aone-kit", []string{"call-tool", "ata::message-ding-talk-send-to-me",
			`{"fieldName_0":{"title":"t","markdownContent":"c"}}`, "--provider", "zetta"}},
	}
	for _, read := range reads {
		if match := find(t, read.tool, read.argv...); match != nil {
			t.Errorf("%s %s gated by rule %q; a read must never be gated",
				read.tool, strings.Join(read.argv, " "), match.Rule.ID)
		}
	}
}

func TestDocumentedSendCommandsAreGated(t *testing.T) {
	sends := []struct {
		tool string
		rule string
		argv []string
	}{
		// dingtalk/SKILL.md:61-65 — group message, with -y already suppressing the
		// tool's own confirmation.
		{"dws", "chat-send", []string{"chat", "message", "send", "--group", "cid123",
			"--title", "发布完成", "--text", "已发布到预发", "-y"}},
		// dingtalk/SKILL.md:77-81 — direct message.
		{"dws", "chat-send", []string{"chat", "message", "send", "--user", "u456",
			"--title", "催一下", "--text", "看下 MR", "-y"}},
		// A global flag in front of the subcommand must not hide the command.
		{"dws", "chat-send", []string{"-f", "json", "chat", "message", "send",
			"--group", "cid123", "--text", "hi", "-y"}},
		// a1/references/repo-commands.md:99 — the real path, with the `repo` prefix.
		{"a1", "mr-remind", []string{"repo", "mr", "remind", "123"}},
		{"aone-kit", "ata-webhook-push", []string{"call-tool",
			"ata::message-ding-talk-send-to-webhook",
			`{"fieldName_0":{"webhook":"https://oapi.dingtalk.com/robot/send?access_token=abc","title":"周报","markdownContent":"## 本周"}}`,
			"--provider", "zetta"}},
	}
	for _, send := range sends {
		match := find(t, send.tool, send.argv...)
		if match == nil {
			t.Errorf("%s %s not gated", send.tool, strings.Join(send.argv, " "))
			continue
		}
		if match.Rule.ID != send.rule {
			t.Errorf("%s matched rule %q, want %q", send.tool, match.Rule.ID, send.rule)
		}
	}
}

// A message body is attacker-adjacent text: it is written by whatever the agent
// was reading. It must not be able to choose a rule.
func TestBodyContentCannotTriggerAMatch(t *testing.T) {
	cases := []struct {
		name string
		tool string
		argv []string
	}{
		{"tool id quoted in a read's keyword", "aone-kit", []string{"call-tool",
			"ata::article-list-query",
			`{"fieldName_0":{"q":"ata::message-ding-talk-send-to-webhook 这个工具怎么用"}}`,
			"--provider", "zetta"}},
		{"subcommand words in a flag value", "dws", []string{"chat", "search",
			"--keyword", "chat message send", "-f", "json"}},
		{"subcommand words spread across flag values", "dws", []string{"chat", "message",
			"list", "--group", "chat", "--title", "message", "--keyword", "send"}},
		{"send appearing only in a title", "dws", []string{"chat", "search",
			"--title", "send", "-f", "json"}},
	}
	for _, test := range cases {
		if match := find(t, test.tool, test.argv...); match != nil {
			t.Errorf("%s: gated by rule %q on content alone", test.name, match.Rule.ID)
		}
	}
}

// The skill's summary table writes `mr remind`; the command is `repo mr remind`.
// Matching the shorthand would gate nothing at all.
func TestMrRemindNeedsTheRepoPrefix(t *testing.T) {
	if match := find(t, "a1", "mr", "remind", "123"); match != nil {
		t.Fatalf("bare `a1 mr remind` matched %q, but that is not a real command", match.Rule.ID)
	}
	if match := find(t, "a1", "repo", "mr", "remind", "123"); match == nil {
		t.Fatal("`a1 repo mr remind` not gated")
	}
}

func TestUnknownToolGatesNothing(t *testing.T) {
	if match := find(t, "git", "push", "--force"); match != nil {
		t.Fatalf("a tool with no rules gated %q", match.Rule.ID)
	}
}

func TestRuleWithNoMatcherIsAnErrorNotASilentPass(t *testing.T) {
	_, err := Find([]string{"chat", "message", "send"}, []config.GuardRule{{ID: "broken"}})
	if err == nil {
		t.Fatal("a rule that can never match was accepted; it would silently gate nothing")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error %q does not name the offending rule", err)
	}
}

func TestInvalidPatternIsAnErrorSoTheCallerCanFailClosed(t *testing.T) {
	_, err := Find([]string{"call-tool", "ata::message-ding-talk-send-to-webhook"},
		[]config.GuardRule{{ID: "bad-regex", ArgvPattern: `^ata::(`}})
	if err == nil {
		t.Fatal("an uncompilable pattern was ignored; a send would pass on ATM's own misconfiguration")
	}
}

func TestSubcommandTokensStopAtTheFirstFlag(t *testing.T) {
	cases := []struct {
		argv []string
		want []string
	}{
		{[]string{"chat", "message", "send", "--group", "g", "--text", "t", "-y"},
			[]string{"chat", "message", "send"}},
		// A leading flag's value cannot be told apart from a subcommand without
		// knowing that flag's arity, so it stays in the head. Harmless: the rule
		// path is matched as a consecutive run, not as a prefix, so an extra
		// leading token neither hides the send nor gates anything else.
		{[]string{"-f", "json", "chat", "message", "send", "--group", "g"},
			[]string{"json", "chat", "message", "send"}},
		{[]string{"repo", "mr", "remind", "123"}, []string{"repo", "mr", "remind", "123"}},
		{[]string{"--help"}, []string{}},
		{nil, []string{}},
	}
	for _, test := range cases {
		got := subcommandTokens(test.argv)
		if len(got) != len(test.want) {
			t.Fatalf("subcommandTokens(%q) = %q, want %q", test.argv, got, test.want)
		}
		for index := range test.want {
			if got[index] != test.want[index] {
				t.Fatalf("subcommandTokens(%q) = %q, want %q", test.argv, got, test.want)
			}
		}
	}
}

func TestUserRulesReplaceByIDAndAddWithoutDroppingBuiltins(t *testing.T) {
	original := config.Guard
	t.Cleanup(func() { config.Guard = original })

	config.Guard = config.GuardConfig{Tools: map[string]config.GuardToolConfig{
		"dws": {Rules: []config.GuardRule{
			{ID: "chat-send", Label: "改过的标签", Path: []string{"chat", "message", "send"}},
			{ID: "doc-write", Label: "写钉钉文档", Path: []string{"doc", "write"}},
		}},
		"git": {Rules: []config.GuardRule{{ID: "force-push", Path: []string{"push"}}}},
	}}

	rules := Rules("dws")
	if len(rules) != 2 {
		t.Fatalf("dws rules = %d, want the replaced built-in plus the new one", len(rules))
	}
	if rules[0].ID != "chat-send" || rules[0].Label != "改过的标签" {
		t.Fatalf("built-in not replaced in place: %+v", rules[0])
	}
	if match := find(t, "dws", "doc", "write", "--id", "x"); match == nil {
		t.Fatal("added rule does not gate")
	}
	// Retuning one tool must not disturb another's built-ins.
	if match := find(t, "a1", "repo", "mr", "remind", "1"); match == nil {
		t.Fatal("a1 built-in lost when dws was overridden")
	}
	if match := find(t, "git", "push", "--force"); match == nil {
		t.Fatal("a newly registered tool does not gate")
	}
}

func TestOverridingBinDoesNotDropRules(t *testing.T) {
	original := config.Guard
	t.Cleanup(func() { config.Guard = original })

	config.Guard = config.GuardConfig{Tools: map[string]config.GuardToolConfig{
		"dws": {Bin: "/opt/dws"},
	}}
	tools := Tools()
	if tools["dws"].Bin != "/opt/dws" {
		t.Fatalf("bin = %q", tools["dws"].Bin)
	}
	if len(tools["dws"].Rules) != 1 {
		t.Fatalf("rules = %d, want the built-in preserved", len(tools["dws"].Rules))
	}
}

func TestDurationsFallBackToDefaults(t *testing.T) {
	original := config.Guard
	t.Cleanup(func() { config.Guard = original })

	config.Guard = config.GuardConfig{}
	if Wait() != DefaultWait || Expire() != DefaultExpire || DenyCooldown() != DefaultDenyCooldown {
		t.Fatalf("empty config did not fall back: %v/%v/%v", Wait(), Expire(), DenyCooldown())
	}
	config.Guard = config.GuardConfig{WaitSeconds: 5, ExpireMinutes: 2, DenyCooldownMinutes: 1}
	if Wait().Seconds() != 5 || Expire().Minutes() != 2 || DenyCooldown().Minutes() != 1 {
		t.Fatalf("overrides ignored: %v/%v/%v", Wait(), Expire(), DenyCooldown())
	}
	// The wait must stay well under the shell-command timeouts agents impose, or a
	// killed gate never delivers its instructions.
	if DefaultWait >= 60*1e9 {
		t.Fatalf("default wait %v is long enough for an agent to kill the gate first", DefaultWait)
	}
}
