package guard

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// extract pulls one preview value out of a command. Flag lookup scans the whole
// argv — unlike matching, where scanning everything would be unsafe, reading a
// named flag's value cannot change *whether* the command is gated, only what the
// user is shown about it.
func extract(argv, tokens []string, extractor config.GuardExtractor) string {
	for _, flag := range extractor.Flags {
		if value, ok := flagValue(argv, flag); ok {
			return RedactSecrets(value)
		}
	}
	if extractor.Positional > 0 {
		if value, ok := positional(tokens, extractor.Positional); ok {
			return RedactSecrets(value)
		}
	}
	if extractor.JSONArg != nil {
		if value, ok := positional(tokens, extractor.JSONArg.Index); ok {
			if resolved, ok := jsonPath(value, extractor.JSONArg.Path); ok {
				return RedactSecrets(resolved)
			}
		}
	}
	return ""
}

// secretQueryParam matches the credential-bearing query parameters that turn up
// in the values a gated command targets.
// The leading \b keeps it off words that merely end in one of these, so a
// harmless `monkey=` or `tokens=` is left alone.
var secretQueryParam = regexp.MustCompile(`(?i)\b((?:access_)?token|secret|sign|key)=[^&\s"']+`)

// RedactSecrets masks credentials inside a preview value.
//
// This exists because of a real case rather than as a precaution: the only thing
// identifying which group an ATA push reaches is its webhook URL, and that URL
// embeds an access token. The user needs to see which group; nothing needs the
// token, least of all a durable table that rides into `atm backup` archives and a
// notification banner rendered by the window server.
func RedactSecrets(value string) string {
	return secretQueryParam.ReplaceAllStringFunc(value, func(match string) string {
		name, _, found := strings.Cut(match, "=")
		if !found {
			return match
		}
		return name + "=…"
	})
}

func flagValue(argv []string, flag string) (string, bool) {
	for index, token := range argv {
		if token == flag {
			if index+1 < len(argv) {
				return argv[index+1], true
			}
			return "", false
		}
		if value, ok := strings.CutPrefix(token, flag+"="); ok {
			return value, true
		}
	}
	return "", false
}

// positional indexes the leading non-flag tokens, 1-based so a rule reads the
// way the command does.
func positional(tokens []string, index int) (string, bool) {
	if index <= 0 || index > len(tokens) {
		return "", false
	}
	return tokens[index-1], true
}

// jsonPath walks a dotted path through a JSON argument. Anything unparseable
// yields no value rather than an error: a missing preview must never be able to
// stop a command from being gated.
func jsonPath(raw, path string) (string, bool) {
	var current any
	if err := json.Unmarshal([]byte(raw), &current); err != nil {
		return "", false
	}
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[segment]
		if !ok {
			return "", false
		}
	}
	switch value := current.(type) {
	case string:
		return value, true
	case nil:
		return "", false
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", false
		}
		return string(encoded), true
	}
}

// RedactedCommand renders a command for a user who has to decide about it when no
// rule extractor produced a preview. Flag values are kept — the user is being
// asked to approve this exact command and needs to see it — but the rendering is
// length-capped by the caller before it is stored or sent.
func RedactedCommand(tool string, argv []string) string {
	parts := make([]string, 0, len(argv)+1)
	parts = append(parts, tool)
	for _, token := range argv {
		if strings.ContainsAny(token, " \t\n\"'") {
			parts = append(parts, "'"+strings.ReplaceAll(token, "'", `'\''`)+"'")
			continue
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, " ")
}
