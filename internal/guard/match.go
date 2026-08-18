// Package guard decides whether a command an agent is about to run reaches
// somebody else, and therefore needs a human decision first.
//
// The whole package is built around one asymmetry. Missing a send costs exactly
// what ATM cost before the gate existed: the message goes out unreviewed. Gating
// a read costs the feature itself — a gate that interrupts `atm`'s own routine
// queries gets uninstalled within a day, after which nothing is gated at all. So
// every ambiguous case here resolves toward *not* matching.
package guard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Match is a gated command: which rule caught it, and what to show the user.
type Match struct {
	Rule   config.GuardRule
	Target string
	Title  string
	Body   string
}

// subcommandTokens returns the leading run of non-flag tokens — the command's
// subcommand path and its positional arguments, and nothing else.
//
// Scanning stops at the first flag on purpose. Matching against every token
// instead would let flag *values* pose as subcommands: an agent running
// `dws chat message list --x chat --y message --z send` would otherwise present
// the consecutive run chat/message/send and have a read gated as a send.
//
// Leading flags are skipped rather than terminating the scan, so a global flag
// in front of the subcommand (`dws -f json chat message send`) does not hide the
// command from its rule.
func subcommandTokens(argv []string) []string {
	tokens := []string{}
	started := false
	for _, token := range argv {
		if strings.HasPrefix(token, "-") && token != "-" {
			if started {
				break
			}
			continue
		}
		started = true
		tokens = append(tokens, token)
	}
	return tokens
}

// Find reports the rule that gates this command, or nil when none does.
//
// A non-nil error means a rule for this tool could not be evaluated. Callers
// must treat that as "cannot tell" and refuse to run the command, because the
// alternative is letting a send through on the strength of ATM's own
// misconfiguration. It is scoped to one tool's rules, so a broken rule cannot
// block commands for the tools it says nothing about.
func Find(argv []string, rules []config.GuardRule) (*Match, error) {
	tokens := subcommandTokens(argv)
	for _, rule := range rules {
		matched, err := ruleMatches(rule, tokens)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		match := &Match{Rule: rule}
		match.Target = extract(argv, tokens, rule.Target)
		match.Title = extract(argv, tokens, rule.Title)
		match.Body = extract(argv, tokens, rule.Body)
		return match, nil
	}
	return nil, nil
}

func ruleMatches(rule config.GuardRule, tokens []string) (bool, error) {
	if len(rule.Path) == 0 && strings.TrimSpace(rule.ArgvPattern) == "" {
		return false, fmt.Errorf("guard rule %q matches nothing: give it a path or an argv_pattern", rule.ID)
	}
	if len(rule.Path) > 0 && !containsRun(tokens, rule.Path) {
		return false, nil
	}
	if pattern := strings.TrimSpace(rule.ArgvPattern); pattern != "" {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("guard rule %q has an invalid argv_pattern: %w", rule.ID, err)
		}
		if !matchesAnyToken(expression, tokens) {
			return false, nil
		}
	}
	return true, nil
}

// containsRun reports whether path appears as consecutive tokens.
func containsRun(tokens, path []string) bool {
	if len(path) == 0 || len(path) > len(tokens) {
		return false
	}
	for start := 0; start+len(path) <= len(tokens); start++ {
		hit := true
		for offset, want := range path {
			if tokens[start+offset] != want {
				hit = false
				break
			}
		}
		if hit {
			return true
		}
	}
	return false
}

// matchesAnyToken tests each token separately. Never run a pattern over the
// joined command line: the join makes a rule content-injectable, because a
// message body would then be able to satisfy a pattern written about a
// subcommand.
func matchesAnyToken(expression *regexp.Regexp, tokens []string) bool {
	for _, token := range tokens {
		if expression.MatchString(token) {
			return true
		}
	}
	return false
}
