package guard

import (
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Defaults. The wait is agent-scale and the expiry is human-scale.
//
// The wait is deliberately short. Its only job is to catch a user who is already
// at the desk and clicks; if they were not there in half a minute they were not
// there at all, and the deferred path — where ATM runs the command itself once
// the request is approved — is the designed answer rather than a degraded one. A
// longer wait actively makes things worse: agents impose their own timeouts on a
// shell command, and a gate killed by one never gets to deliver the instructions
// on its stderr, which is the single most useful thing it produces.
const (
	DefaultWait         = 25 * time.Second
	DefaultExpire       = 30 * time.Minute
	DefaultDenyCooldown = 10 * time.Minute
)

// DefaultTools are the rules that apply with no configuration at all: the
// outbound-communication commands ATM's own skills tell agents to run with the
// tool's confirmation already suppressed.
//
// Scope is outbound communication only. The same mechanism would cover
// irreversible engineering actions one token away in the same skills
// (`a1 repo mr merge`, `a1 app pipeline reenter`), and the config shape does not
// make those awkward to add — they are left out because nothing has gone wrong
// with them, and a gate is only kept if it almost never fires wrongly.
func DefaultTools() map[string]config.GuardToolConfig {
	return map[string]config.GuardToolConfig{
		"dws": {Rules: []config.GuardRule{{
			ID:     "chat-send",
			Label:  "发送钉钉消息",
			Path:   []string{"chat", "message", "send"},
			Target: config.GuardExtractor{Flags: []string{"--group", "--user"}},
			Title:  config.GuardExtractor{Flags: []string{"--title"}},
			Body:   config.GuardExtractor{Flags: []string{"--text"}},
		}}},
		"a1": {Rules: []config.GuardRule{{
			ID:    "mr-remind",
			Label: "催办 MR 评审人",
			// `a1 repo mr remind`, not `a1 mr remind`: the skill's summary table
			// drops the `repo` prefix that the actual command requires.
			Path:   []string{"repo", "mr", "remind"},
			Target: config.GuardExtractor{Positional: 4},
		}}},
		"aone-kit": {Rules: []config.GuardRule{{
			ID:    "ata-webhook-push",
			Label: "ATA 钉钉群推送",
			// Only the webhook push. ata::message-ding-talk-send-to-me delivers to
			// the caller, and gating that would be a false positive by this gate's
			// own standard of "reaches somebody else".
			ArgvPattern: `^ata::message-ding-talk-send-to-webhook$`,
			// The webhook URL is the only thing identifying which group is being
			// pushed to, and it carries an access token. Preview values are
			// secret-redacted before they are stored or sent for exactly this case.
			Target: config.GuardExtractor{JSONArg: &config.GuardJSONArg{Index: 3, Path: "fieldName_0.webhook"}},
			Title:  config.GuardExtractor{JSONArg: &config.GuardJSONArg{Index: 3, Path: "fieldName_0.title"}},
			Body:   config.GuardExtractor{JSONArg: &config.GuardJSONArg{Index: 3, Path: "fieldName_0.markdownContent"}},
		}}},
	}
}

// Tools returns the effective configuration: built-in defaults with the user's
// config.json merged over them. A user rule replaces a built-in with the same id
// and is otherwise added, so retuning one rule never silently drops the others.
func Tools() map[string]config.GuardToolConfig {
	merged := DefaultTools()
	for tool, override := range config.Guard.Tools {
		effective, known := merged[tool]
		if !known {
			effective = config.GuardToolConfig{}
		}
		if override.Bin != "" {
			effective.Bin = override.Bin
		}
		effective.Rules = mergeRules(effective.Rules, override.Rules)
		merged[tool] = effective
	}
	return merged
}

func mergeRules(base, override []config.GuardRule) []config.GuardRule {
	merged := make([]config.GuardRule, len(base))
	copy(merged, base)
	for _, rule := range override {
		replaced := false
		for index := range merged {
			if merged[index].ID == rule.ID {
				merged[index] = rule
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, rule)
		}
	}
	return merged
}

// Rules returns the effective rules for one tool. An unknown tool has none, and
// therefore gates nothing — installing a shim for a tool with no rules turns
// every one of its invocations into a plain pass-through.
func Rules(tool string) []config.GuardRule {
	return Tools()[tool].Rules
}

// Wait is how long a gate process waits for a decision before handing the
// request over and telling its caller it is still pending.
func Wait() time.Duration {
	if config.Guard.WaitSeconds > 0 {
		return time.Duration(config.Guard.WaitSeconds) * time.Second
	}
	return DefaultWait
}

// Expire is how long the request itself stays decidable, independent of whether
// any process is still waiting on it.
func Expire() time.Duration {
	if config.Guard.ExpireMinutes > 0 {
		return time.Duration(config.Guard.ExpireMinutes) * time.Minute
	}
	return DefaultExpire
}

// DenyCooldown is how long a refusal keeps answering for the same command, so a
// retrying agent is told no immediately instead of raising the banner again. It
// expires because a command refused once must not become unsendable forever.
func DenyCooldown() time.Duration {
	if config.Guard.DenyCooldownMinutes > 0 {
		return time.Duration(config.Guard.DenyCooldownMinutes) * time.Minute
	}
	return DefaultDenyCooldown
}
