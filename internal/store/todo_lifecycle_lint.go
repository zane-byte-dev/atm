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

var p1RationalePattern = regexp.MustCompile(`(?i)(deadline|blocked|blocker|urgent|截止|阻塞|紧急|\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b)`)

// These thresholds intentionally favor a missed advisory over a false alarm.
// Todo lint is run on normal work, so a batch or long-running task should not be
// branded as broken merely because it has several sessions.
const (
	TodoLintBindingSprawlThreshold       = 8
	TodoLintStaleBindingThreshold        = 24 * time.Hour
	TodoLintScopeMinimumSessions         = 8
	TodoLintScopeMinimumSummaries        = 5
	TodoLintScopeDivergentPercent        = 50
	TodoLintPriorityMinimumPortfolioSize = 20
	TodoLintPriorityP1Percent            = 70
)

type TodoLintRuntime struct {
	Bindings []TodoSessionBinding
	Sessions []TodoBoundSession
	Effects  []WorkEffectRecord
	Now      time.Time
}

// TodoGUICompletionReceipt records the human's click without inventing evidence.
const TodoGUICompletionReceipt = "通过 ATM GUI 完成"

func isTodoGUICompletionReceipt(reason string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(reason), " ")) {
	case "通过 atm gui 完成", "通过 atm 菜单栏完成", "通过 atm 菜单栏验收",
		"通过菜单栏完成", "completed via atm menu bar":
		return true
	default:
		return false
	}
}

// IsGenericTodoCompletionReason recognizes UI/action receipts that contain no
// acceptance evidence. Keep this list deliberately exact: lint must not reject
// a short but meaningful human statement merely because it lacks a keyword.
func IsGenericTodoCompletionReason(reason string) bool {
	if isTodoGUICompletionReceipt(reason) {
		return true
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(reason), " "))
	switch normalized {
	case "完成", "已完成", "done", "completed":
		return true
	default:
		return false
	}
}

func ValidateTodoCompletionReason(reason string) error {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return fmt.Errorf("completion evidence is required; describe the verified result, test, review, or delivery reference")
	}
	if IsGenericTodoCompletionReason(trimmed) {
		return fmt.Errorf("completion reason %q records the UI action rather than acceptance evidence; describe what was verified", trimmed)
	}
	return nil
}

// LintTodoLifecycle combines durable lifecycle facts that cannot be inferred
// from the Markdown card alone. Every rule is advisory and deterministic: it
// never edits a Todo, closes a binding, or rewrites legacy metadata.
func LintTodoLifecycle(tf *TodoFile, todo *Todo, runtime TodoLintRuntime) []TodoLintIssue {
	if todo == nil {
		return nil
	}
	now := runtime.Now
	if now.IsZero() {
		now = time.Now()
	}
	issues := lintTodoLegacyMetadata(tf, todo)
	issues = append(issues, lintTodoCompletionEvidence(todo)...)
	issues = append(issues, lintTodoBindingHistory(todo, runtime, now)...)
	issues = append(issues, lintTodoSubmitHistory(runtime)...)
	issues = append(issues, lintTodoScope(todo, runtime.Sessions)...)
	return issues
}

func lintTodoLegacyMetadata(tf *TodoFile, todo *Todo) []TodoLintIssue {
	var issues []TodoLintIssue
	if strings.TrimSpace(todo.Creator) == "" {
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "legacy_creator_missing",
			Detail:     "the todo predates reliable creator attribution",
			Suggestion: fmt.Sprintf("verify who filed it, then set it explicitly with `atm todo edit %s --creator <creator>`; do not guess or bulk-fill legacy rows", todo.ID),
			Details:    map[string]any{"creator": "", "automatic_fix": false},
		})
	}
	canonical := config.CanonicalProject(todo.Project)
	if strings.TrimSpace(todo.Project) != "" && canonical != todo.Project {
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "noncanonical_project",
			Detail:     fmt.Sprintf("project %q resolves to canonical project %q", todo.Project, canonical),
			Suggestion: fmt.Sprintf("confirm the alias, then run `atm todo edit %s --project %q`; lint never rewrites project history", todo.ID, canonical),
			Details:    map[string]any{"stored_project": todo.Project, "canonical_project": canonical, "automatic_fix": false},
		})
	}
	if todo.Priority == "P1" && (todo.Status == TodoStatusOpen || todo.Status == TodoStatusInProgress) &&
		strings.TrimSpace(todo.ReviewAt) == "" && strings.TrimSpace(todo.WakeCondition) == "" &&
		!p1RationalePattern.MatchString(todo.Description) {
		suggestion := "confirm P1 is intentional; record a concrete deadline/blocker in the description or consider P2"
		if todo.Status == TodoStatusInProgress {
			suggestion = "confirm P1 is intentional; add `--review-at YYYY-MM-DD` for time-critical work or `--wake \"<observable condition>\"` when blocked, otherwise consider P2"
		}
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "p1_without_deadline_or_blocker",
			Detail:     "the active P1 todo has no structured deadline, observable blocker, or explicit rationale",
			Suggestion: suggestion,
			Details: map[string]any{
				"priority": todo.Priority, "review_at": todo.ReviewAt,
				"wake_condition": todo.WakeCondition, "status": todo.Status,
				"rationale_detected": false, "automatic_fix": false,
			},
		})
	}
	if tf != nil && todo.Priority == "P1" {
		total, p1 := 0, 0
		for _, item := range tf.Items {
			// The working-set snapshot intentionally includes completed but not
			// archived rows. Priority inflation is an intake/history smell, not
			// merely a count of what happens to be active this minute.
			total++
			if item.Priority == "P1" {
				p1++
			}
		}
		if total >= TodoLintPriorityMinimumPortfolioSize && p1*100 >= total*TodoLintPriorityP1Percent {
			percent := p1 * 100 / total
			issues = append(issues, TodoLintIssue{
				Severity:   "info",
				Code:       "priority_p1_concentration",
				Detail:     fmt.Sprintf("%d of %d working-set todos are P1 (%d%%)", p1, total, percent),
				Suggestion: "review relative priority during planning; lint does not downgrade individual todos automatically",
				Details: map[string]any{
					"working_set_todos": total, "p1_todos": p1, "p1_percent": percent,
					"minimum_portfolio_size": TodoLintPriorityMinimumPortfolioSize,
					"threshold_percent":      TodoLintPriorityP1Percent,
					"automatic_fix":          false,
				},
			})
		}
	}
	return issues
}

func lintTodoCompletionEvidence(todo *Todo) []TodoLintIssue {
	if todo.Status != TodoStatusDone {
		return nil
	}
	reason := ""
	if todo.ClosedReason != nil {
		reason = strings.TrimSpace(*todo.ClosedReason)
	}
	// GUI acceptance is intentionally one-click. Its action receipt is valid
	// audit history, though it is not evidence acceptable to the CLI validator.
	if isTodoGUICompletionReceipt(reason) {
		return nil
	}
	if reason == "" {
		return []TodoLintIssue{{
			Severity:   "warning",
			Code:       "completion_reason_missing",
			Detail:     "the accepted todo has no completion evidence",
			Suggestion: "record the verified result when accepting future work; do not invent evidence for this historical row",
			Details:    map[string]any{"automatic_fix": false},
		}}
	}
	if IsGenericTodoCompletionReason(reason) {
		return []TodoLintIssue{{
			Severity:   "warning",
			Code:       "generic_completion_reason",
			Detail:     fmt.Sprintf("completion reason %q records the UI action rather than acceptance evidence", reason),
			Suggestion: "record what was verified (result, test, review, or delivery reference); do not rewrite historical evidence automatically",
			Details:    map[string]any{"reason": reason, "automatic_fix": false},
		}}
	}
	return nil
}

func lintTodoBindingHistory(todo *Todo, runtime TodoLintRuntime, now time.Time) []TodoLintIssue {
	distinct := map[string]bool{}
	for _, binding := range runtime.Bindings {
		if binding.TodoID == todo.ID && strings.TrimSpace(binding.SessionID) != "" {
			distinct[binding.SessionID] = true
		}
	}
	var issues []TodoLintIssue
	if len(distinct) >= TodoLintBindingSprawlThreshold && !TodoHasTag(*todo, TodoTagMaintenance) && todo.MaintenanceLimit == 0 {
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "binding_sprawl",
			Detail:     fmt.Sprintf("the todo has been worked by %d distinct sessions (advisory threshold %d)", len(distinct), TodoLintBindingSprawlThreshold),
			Suggestion: "check whether the durable scope should be split into child todos; use a bounded maintenance label for intentional batches",
			Details: map[string]any{
				"distinct_sessions": len(distinct), "threshold": TodoLintBindingSprawlThreshold,
				"maintenance": false,
			},
		})
	}

	sessions := make(map[string]TodoBoundSession, len(runtime.Sessions))
	for _, session := range runtime.Sessions {
		sessions[session.SessionID] = session
	}
	var stale []string
	oldestAge := int64(0)
	for _, binding := range runtime.Bindings {
		if binding.TodoID != todo.ID || binding.UnboundAt != nil {
			continue
		}
		lastObserved := binding.BoundAt
		if session, ok := sessions[binding.SessionID]; ok && session.LastAt > lastObserved {
			lastObserved = session.LastAt
		}
		age := now.Unix() - lastObserved
		if age < int64(TodoLintStaleBindingThreshold/time.Second) {
			continue
		}
		stale = append(stale, binding.SessionID)
		if age > oldestAge {
			oldestAge = age
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		issues = append(issues, TodoLintIssue{
			Severity:   "warning",
			Code:       "unobserved_binding",
			Detail:     fmt.Sprintf("%d active binding(s) have no indexed activity within %s", len(stale), TodoLintStaleBindingThreshold),
			Suggestion: "inspect `atm session status --json`; if the sessions are truly gone, explicitly unbind or transition the Todo so ATM can reconcile the ledger",
			Details: map[string]any{
				"session_ids": stale, "count": len(stale),
				"stale_after_seconds": int64(TodoLintStaleBindingThreshold / time.Second),
				"oldest_age_seconds":  oldestAge, "automatic_fix": false,
				"reconcile_mode": "manual_confirmation_required",
			},
		})
	}
	return issues
}

func lintTodoSubmitHistory(runtime TodoLintRuntime) []TodoLintIssue {
	var submits []WorkEffectRecord
	for _, effect := range runtime.Effects {
		if effect.Kind == "todo_submitted" {
			submits = append(submits, effect)
		}
	}
	if len(submits) == 0 {
		return nil
	}
	var issues []TodoLintIssue
	if len(submits) > 1 {
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "multiple_submit",
			Detail:     fmt.Sprintf("the todo has %d distinct submit transitions", len(submits)),
			Suggestion: "confirm that each reopen addressed rejected work; split unrelated follow-up work into another todo",
			Details:    map[string]any{"submit_count": len(submits), "threshold": 2},
		})
	}
	firstSubmit := submits[0].CreatedAt
	postSubmitBindings := map[string]bool{}
	for _, binding := range runtime.Bindings {
		// Effects use Unix nanoseconds while bindings use seconds. Requiring the
		// next whole second avoids classifying the binding closed by submit itself.
		if binding.BoundAt > firstSubmit/int64(time.Second) {
			postSubmitBindings[binding.SessionID] = true
		}
	}
	postSubmitStarts := 0
	for _, effect := range runtime.Effects {
		if effect.Kind == "todo_started" && effect.CreatedAt > firstSubmit {
			postSubmitStarts++
		}
	}
	if len(postSubmitBindings) > 0 || postSubmitStarts > 0 {
		ids := make([]string, 0, len(postSubmitBindings))
		for id := range postSubmitBindings {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		issues = append(issues, TodoLintIssue{
			Severity:   "warning",
			Code:       "post_submit_work",
			Detail:     fmt.Sprintf("work resumed after submit (%d later start transition(s), %d later bound session(s))", postSubmitStarts, len(ids)),
			Suggestion: "verify whether review explicitly rejected the submission; otherwise keep the submitted Todo in review and move follow-up scope to another todo",
			Details: map[string]any{
				"later_start_count": postSubmitStarts, "later_binding_count": len(ids),
				"later_session_ids": ids, "first_submit_at": firstSubmit,
			},
		})
	}
	return issues
}

func lintTodoScope(todo *Todo, sessions []TodoBoundSession) []TodoLintIssue {
	if len(sessions) < TodoLintScopeMinimumSessions || TodoHasTag(*todo, TodoTagMaintenance) || todo.MaintenanceLimit > 0 {
		return nil
	}
	targetTokens := meaningfulTodoLintTokens(todo.Title + " " + todo.Description)
	if len(targetTokens) < 2 {
		return nil
	}
	evaluated, divergent := 0, 0
	var examples []string
	seen := map[string]bool{}
	for _, session := range sessions {
		summary := strings.TrimSpace(session.Summary)
		key := strings.ToLower(summary)
		if summary == "" || seen[key] {
			continue
		}
		seen[key] = true
		evaluated++
		if summaryMatchesTodoTokens(summary, targetTokens) {
			continue
		}
		divergent++
		if len(examples) < 3 {
			examples = append(examples, truncateTodoLintText(summary, 120))
		}
	}
	if evaluated < TodoLintScopeMinimumSummaries || divergent*100 < evaluated*TodoLintScopeDivergentPercent {
		return nil
	}
	percent := divergent * 100 / evaluated
	return []TodoLintIssue{{
		Severity:   "info",
		Code:       "scope_drift",
		Detail:     fmt.Sprintf("%d of %d distinct session summaries (%d%%) share no meaningful target token with the todo", divergent, evaluated, percent),
		Suggestion: "review the sampled summaries and split only genuinely unrelated work; this lexical heuristic never edits the todo",
		Details: map[string]any{
			"session_count": len(sessions), "evaluated_summaries": evaluated,
			"divergent_summaries": divergent, "divergent_percent": percent,
			"minimum_sessions":  TodoLintScopeMinimumSessions,
			"minimum_summaries": TodoLintScopeMinimumSummaries,
			"threshold_percent": TodoLintScopeDivergentPercent,
			"examples":          examples, "automatic_fix": false,
		},
	}}
}

func meaningfulTodoLintTokens(value string) map[string]bool {
	generic := map[string]bool{
		"todo": true, "agent": true, "task": true, "work": true, "with": true,
		"任务": true, "工作": true, "分析": true, "修改": true, "优化": true,
	}
	result := map[string]bool{}
	for _, token := range todoMatchTokens(value) {
		if !generic[token] {
			result[token] = true
		}
	}
	return result
}

func summaryMatchesTodoTokens(summary string, target map[string]bool) bool {
	for _, token := range todoMatchTokens(summary) {
		if target[token] {
			return true
		}
	}
	return false
}

func truncateTodoLintText(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max-1]) + "…"
}
