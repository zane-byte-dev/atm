package guard

import (
	"sort"
	"strings"
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

// mergeRules applies user rules over the built-ins.
//
// A user rule that states a matcher replaces the built-in of that id outright. One
// that does not is a *patch*: it carries only an id plus what to change, which is
// how switching a built-in off works without restating its matcher — restating it
// would let the copy drift from the real one and silently stop gating.
func mergeRules(base, override []config.GuardRule) []config.GuardRule {
	merged := make([]config.GuardRule, len(base))
	copy(merged, base)
	for _, rule := range override {
		index := indexOfRule(merged, rule.ID)
		if index < 0 {
			merged = append(merged, rule)
			continue
		}
		if rule.HasMatcher() {
			merged[index] = rule
			continue
		}
		if rule.Enabled != nil {
			merged[index].Enabled = rule.Enabled
		}
		if strings.TrimSpace(rule.Label) != "" {
			merged[index].Label = rule.Label
		}
	}
	return merged
}

func indexOfRule(rules []config.GuardRule, id string) int {
	for index := range rules {
		if rules[index].ID == id {
			return index
		}
	}
	return -1
}

// Rules returns the rules that actually gate, for one tool: the effective set
// minus anything switched off. An unknown tool has none, and therefore gates
// nothing — installing a shim for a tool with no rules turns every one of its
// invocations into a plain pass-through.
//
// Disabled rules are filtered here rather than skipped during matching, so a rule
// that is off is never evaluated at all and cannot report a matcher problem.
func Rules(tool string) []config.GuardRule {
	all := Tools()[tool].Rules
	active := make([]config.GuardRule, 0, len(all))
	for _, rule := range all {
		if rule.IsEnabled() {
			active = append(active, rule)
		}
	}
	return active
}

// RuleView is one rule as the settings UI needs to see it: what it is, whether it
// is on, and where it came from. Provenance matters because switching a built-in
// off and deleting a rule you wrote are different actions with different undo.
type RuleView struct {
	Tool        string   `json:"tool"`
	ID          string   `json:"id"`
	Label       string   `json:"label,omitempty"`
	Path        []string `json:"path,omitempty"`
	ArgvPattern string   `json:"argv_pattern,omitempty"`
	TargetFlags []string `json:"target_flags,omitempty"`
	BodyFlags   []string `json:"body_flags,omitempty"`
	Enabled     bool     `json:"enabled"`
	// Builtin means ATM ships this rule; it can be switched off but not deleted.
	Builtin bool `json:"builtin"`
	// Overridden means the config replaced or patched it.
	Overridden bool `json:"overridden"`
}

// RuleViews lists every rule for a tool, including the ones switched off.
func RuleViews(tool string) []RuleView {
	builtin := DefaultTools()[tool].Rules
	overrides := config.Guard.Tools[tool].Rules
	views := []RuleView{}
	for _, rule := range Tools()[tool].Rules {
		views = append(views, RuleView{
			Tool:        tool,
			ID:          rule.ID,
			Label:       rule.Label,
			Path:        rule.Path,
			ArgvPattern: rule.ArgvPattern,
			TargetFlags: rule.Target.Flags,
			BodyFlags:   rule.Body.Flags,
			Enabled:     rule.IsEnabled(),
			Builtin:     indexOfRule(builtin, rule.ID) >= 0,
			Overridden:  indexOfRule(overrides, rule.ID) >= 0,
		})
	}
	return views
}

// ToolNames lists every tool ATM knows about, built-in or registered.
func ToolNames() []string {
	names := make([]string, 0, len(Tools()))
	for tool := range Tools() {
		names = append(names, tool)
	}
	sort.Strings(names)
	return names
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
