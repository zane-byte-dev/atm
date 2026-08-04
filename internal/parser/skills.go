package parser

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

var skillPathPattern = regexp.MustCompile(`(?i)(?:^|[/\\])skills[/\\]([^/\\\s'";]+)[/\\]SKILL\.md`)
var skillCommandPattern = regexp.MustCompile(`(?i)(?:^|\s)/skill:([a-z0-9][a-z0-9:._-]*)`)
var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]*$`)

func skillFromToolCall(toolName string, input map[string]any) string {
	if strings.EqualFold(toolName, "skill") {
		return normalizeSkillName(config.GetStr(input, "skill"))
	}
	for _, key := range []string{"path", "file_path", "filePath", "target_file", "command", "cmd"} {
		if name := skillFromText(config.GetStr(input, key)); name != "" {
			return name
		}
	}
	return ""
}

func skillFromJSONArguments(raw string) string {
	var input map[string]any
	if json.Unmarshal([]byte(raw), &input) != nil {
		return skillFromText(raw)
	}
	return skillFromToolCall("", input)
}

func skillFromText(value string) string {
	match := skillPathPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	return normalizeSkillName(match[1])
}

func skillFromCommand(value string) string {
	match := skillCommandPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	return normalizeSkillName(match[1])
}

func normalizeSkillName(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "/skill:"))
	if !skillNamePattern.MatchString(value) {
		return ""
	}
	return value
}

// IsValidSkillName also protects statistics built from events indexed by older
// parser versions, which could mistake shell variables or globs for skills.
func IsValidSkillName(value string) bool {
	return skillNamePattern.MatchString(value)
}

func appendSkillEvent(events []SkillEvent, name string, ts int64) []SkillEvent {
	name = normalizeSkillName(name)
	if name == "" {
		return events
	}
	return append(events, SkillEvent{Name: name, TS: ts})
}

func compactSkillEvents(events []SkillEvent) []SkillEvent {
	lastByName := make(map[string]int64)
	result := make([]SkillEvent, 0, len(events))
	for _, event := range events {
		if previous, ok := lastByName[event.Name]; ok && event.TS >= previous && event.TS-previous <= 60 {
			continue
		}
		result = append(result, event)
		lastByName[event.Name] = event.TS
	}
	return result
}
