package store

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/config"
)

const TodoProgressMaxRunes = 400

var (
	todoReferencePattern = regexp.MustCompile(`\bt[0-9]+\b`)
	phasePattern         = regexp.MustCompile(`阶段[0-9一二三四五六七八九十①②③④⑤⑥⑦⑧⑨⑩]+`)
)

type TodoLintIssue struct {
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

type todoProgressEntry struct {
	Timestamp string
	Text      string
}

// todoDocSectionHeadingLine matches a line that is exactly one of the task
// card's own section headings.
var todoDocSectionHeadingLine = regexp.MustCompile(`(?m)^## +(需求|分析|进展|备注) *$`)

// ValidateTodoDescription rejects a description that would collide with the task
// card's skeleton. The description is embedded under 需求 verbatim, so a line
// reading exactly "## 分析" is indistinguishable from the card's own 分析 heading:
// the requirement would appear to end there, the sync would rewrite the wrong
// span, and the card would come out with the section twice.
//
// Only these four exact strings are reserved, and only at "## ". A structured
// requirement is otherwise unrestricted — "### 分析" and "## 分析方案" both pass —
// which is the point: the card check exists to encourage detailed requirements,
// so the constraint has to be narrow enough to be worth stating in one line.
func ValidateTodoDescription(description string) error {
	match := todoDocSectionHeadingLine.FindStringSubmatch(blankFencedLines(description))
	if match == nil {
		return nil
	}
	return fmt.Errorf(
		"description line %q collides with the task card's own %s section; "+
			"use a deeper level (### %s) or a different wording",
		strings.TrimSpace(match[0]), match[1], match[1])
}

// blankFencedLines replaces the contents of fenced code blocks with empty lines,
// keeping byte offsets usable while making sure a "## " quoted inside ``` is not
// read as a heading. Requirements quote markdown; that is content, not structure.
func blankFencedLines(content string) string {
	lines := strings.Split(content, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			lines[i] = ""
			continue
		}
		if inFence {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

func ValidateTodoLogMessage(message, section string) error {
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("todo log message cannot be empty")
	}
	// 需求 is regenerated from the todo's description on every metadata sync, so an
	// entry appended here lands on disk, reports success, and is gone by the next
	// command that touches the todo. Refuse instead of losing it quietly.
	if section == todoDocGeneratedSection {
		return fmt.Errorf(
			"section %s is generated from the todo description and would be overwritten; "+
				"use `atm todo edit <id> --desc` to change the requirement, "+
				"or `--section 补充` to add to it",
			todoDocGeneratedSection,
		)
	}
	if section == todoDocPlanGeneratedSection {
		return fmt.Errorf(
			"section %s is generated from the latest plan revision and would be overwritten; "+
				"use `atm todo plan set [id] --file -` to replace the structured plan",
			todoDocPlanGeneratedSection,
		)
	}
	if section != "" && section != "进展" {
		return nil
	}
	if strings.ContainsAny(message, "\r\n") {
		return fmt.Errorf("progress entries must be one paragraph; move detailed notes to `atm todo log <id> <message> --section 分析`")
	}
	if count := utf8.RuneCountInString(message); count > TodoProgressMaxRunes {
		return fmt.Errorf("progress entry is %d characters; maximum is %d; move detailed notes to `atm todo log <id> <message> --section 分析`", count, TodoProgressMaxRunes)
	}
	return nil
}

func TodoReferences(message string) []string {
	matches := todoReferencePattern.FindAllString(message, -1)
	seen := make(map[string]bool, len(matches))
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if !seen[match] {
			seen[match] = true
			result = append(result, match)
		}
	}
	return result
}

// UnknownTodoReferences reports the tN mentions in message that name no todo.
// Archived todos count as known — referring to finished work is normal — and the
// snapshot already carries their IDs, so this needs no I/O and is safe to call
// from inside a write transaction.
func UnknownTodoReferences(tf *TodoFile, message string) []string {
	known := make(map[string]bool, len(tf.Items)+len(tf.archived))
	for _, todo := range tf.Items {
		known[todo.ID] = true
	}
	for id := range tf.archived {
		known[id] = true
	}

	var unknown []string
	for _, id := range TodoReferences(message) {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func LintTodoDoc(tf *TodoFile, todo *Todo, content string) ([]TodoLintIssue, error) {
	issues := lintTodoDocMetadata(todo, content)
	entries := parseTodoProgressEntries(content)
	progressText := make([]string, 0, len(entries))
	totalRunes := 0
	for index, entry := range entries {
		progressText = append(progressText, entry.Text)
		count := utf8.RuneCountInString(entry.Text)
		totalRunes += count
		if count > TodoProgressMaxRunes {
			issues = append(issues, TodoLintIssue{
				Severity:   "warning",
				Code:       "progress_too_long",
				Detail:     fmt.Sprintf("progress entry %d has %d characters (maximum %d)", index+1, count, TodoProgressMaxRunes),
				Suggestion: "keep only result, evidence, and next action; move investigation detail to the 分析 section",
			})
		}
		if strings.ContainsAny(entry.Text, "\r\n") {
			issues = append(issues, TodoLintIssue{
				Severity:   "warning",
				Code:       "progress_multiline",
				Detail:     fmt.Sprintf("progress entry %d spans multiple lines", index+1),
				Suggestion: "keep progress as one paragraph and move lists or headings to the 分析 section",
			})
		}
	}

	joinedProgress := strings.Join(progressText, "\n")
	unknown := UnknownTodoReferences(tf, joinedProgress)
	if len(unknown) > 0 {
		issues = append(issues, TodoLintIssue{
			Severity:   "warning",
			Code:       "unknown_todo_reference",
			Detail:     "progress references missing todos: " + strings.Join(unknown, ", "),
			Suggestion: "create and verify structured todos before referring to their IDs, or correct the progress entry",
		})
	}

	analysis := strings.TrimSpace(todoDocSection(content, "分析"))
	if totalRunes > 1000 && (analysis == "" || analysis == "待补充") {
		issues = append(issues, TodoLintIssue{
			Severity:   "warning",
			Code:       "analysis_missing_with_large_progress",
			Detail:     fmt.Sprintf("progress contains %d characters while the 分析 section is empty", totalRunes),
			Suggestion: "move architecture decisions and investigation notes from progress into the 分析 section",
		})
	}
	if strings.Contains(todo.Description, "- [ ]") && progressClaimsCompletion(joinedProgress) {
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "unchecked_checklist_with_completion_log",
			Detail:     "the todo description still has unchecked items while progress claims completion",
			Suggestion: "update the structured description or complete the real child todo before logging the milestone",
		})
	}
	issues = append(issues, lintRapidPhaseLogs(entries)...)
	return issues, nil
}

func lintTodoDocMetadata(todo *Todo, content string) []TodoLintIssue {
	var issues []TodoLintIssue
	var mismatches []string
	if title := todoDocTitle(content); title != todo.Title {
		mismatches = append(mismatches, fmt.Sprintf("title=%q (want %q)", title, todo.Title))
	}
	expected := []struct {
		label string
		value string
	}{
		{"ID", todo.ID},
		{"状态", todoStatusDisplay(todo.Status)},
		{"优先级", todo.Priority},
		{"标签", strings.Join(todo.Tags, ", ")},
		{"项目", todo.Project},
		{"创建", todo.Created},
		{"创建者", TodoCreatorDocLabel(todo.Creator)},
	}
	for _, item := range expected {
		if got := todoDocField(content, item.label); got != item.value {
			mismatches = append(mismatches, fmt.Sprintf("%s=%q (want %q)", item.label, got, item.value))
		}
	}
	if len(mismatches) > 0 {
		issues = append(issues, TodoLintIssue{
			Severity:   "warning",
			Code:       "doc_metadata_mismatch",
			Detail:     strings.Join(mismatches, "; "),
			Suggestion: "run a todo edit/lifecycle command with the current ATM version to resync the derived markdown card",
		})
	}

	expectedRequirement := strings.TrimSpace(todo.Description)
	if expectedRequirement == "" {
		expectedRequirement = "待补充"
	}
	// The section carries the "generated, do not edit" notice; compare the body.
	actual := strings.TrimSpace(strings.TrimPrefix(todoDocSection(content, "需求"), todoDocRequirementNotice))
	if actual != expectedRequirement {
		issues = append(issues, TodoLintIssue{
			Severity:   "warning",
			Code:       "doc_requirement_mismatch",
			Detail:     "the markdown 需求 section differs from the structured todo description",
			Suggestion: "resync the derived markdown card; edit the todo description instead of maintaining a second requirement source",
		})
	}
	return issues
}

func todoDocTitle(content string) string {
	line := content
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "# "))
}

func todoDocField(content, label string) string {
	prefix := "- **" + label + "**:"
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
	}
	return ""
}

// todoDocSection reads one card section's body. It shares its boundary rules
// with the writer in setTodoDocSection — see todoDocSectionBody — so the check
// that compares the card against the database compares the same span the sync
// writes.
func todoDocSection(content, section string) string {
	bodyStart, bodyEnd, found := todoDocSectionBody(content, section)
	if !found {
		return ""
	}
	return strings.TrimSpace(content[bodyStart:bodyEnd])
}

func parseTodoProgressEntries(content string) []todoProgressEntry {
	section := todoDocSection(content, "进展")
	var entries []todoProgressEntry
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "- [") {
			raw := strings.TrimPrefix(line, "- ")
			entry := todoProgressEntry{Text: raw}
			if close := strings.Index(raw, "]"); close > 1 {
				entry.Timestamp = raw[1:close]
				entry.Text = strings.TrimSpace(raw[close+1:])
			}
			entries = append(entries, entry)
			continue
		}
		if len(entries) > 0 && strings.TrimSpace(line) != "" {
			entries[len(entries)-1].Text += "\n" + strings.TrimSpace(line)
		}
	}
	return entries
}

func progressClaimsCompletion(progress string) bool {
	return strings.Contains(progress, "[done]") || strings.Contains(progress, "已完成") || strings.Contains(progress, "阶段①完成") || strings.Contains(progress, "阶段②完成") || strings.Contains(progress, "阶段③完成")
}

func lintRapidPhaseLogs(entries []todoProgressEntry) []TodoLintIssue {
	for index := 1; index < len(entries); index++ {
		previousPhase := phasePattern.FindString(entries[index-1].Text)
		currentPhase := phasePattern.FindString(entries[index].Text)
		if previousPhase == "" || previousPhase != currentPhase {
			continue
		}
		previousTime, previousErr := time.ParseInLocation("2006-01-02 15:04", entries[index-1].Timestamp, config.Loc)
		currentTime, currentErr := time.ParseInLocation("2006-01-02 15:04", entries[index].Timestamp, config.Loc)
		if previousErr != nil || currentErr != nil {
			continue
		}
		gap := currentTime.Sub(previousTime)
		if gap >= 0 && gap <= time.Hour {
			return []TodoLintIssue{{
				Severity:   "info",
				Code:       "rapid_repeated_phase_logs",
				Detail:     fmt.Sprintf("%s was logged more than once within %s", currentPhase, gap.Round(time.Minute)),
				Suggestion: "keep one completion entry per phase unless work pauses on an external condition",
			}}
		}
	}
	return nil
}
