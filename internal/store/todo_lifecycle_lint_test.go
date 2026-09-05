package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestLintTodoLifecycleReportsExplainableGuardrailCodes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	todo := Todo{
		ID: "t1", Title: "DartX film batch", Description: "Run film capability checks",
		Priority: "P1", Status: TodoStatusInProgress, Project: "atm", Created: Today(),
	}
	portfolio := &TodoFile{}
	for index := 0; index < TodoLintPriorityMinimumPortfolioSize; index++ {
		item := Todo{ID: fmt.Sprintf("t%d", index+1), Title: "Work", Priority: "P1", Status: TodoStatusOpen, Created: Today()}
		if index == 0 {
			item = todo
		}
		portfolio.Items = append(portfolio.Items, item)
	}

	runtime := TodoLintRuntime{Now: now}
	for index := 0; index < TodoLintBindingSprawlThreshold; index++ {
		id := fmt.Sprintf("session-%02d", index)
		boundAt := now.Add(-48*time.Hour + time.Duration(index)*time.Minute).Unix()
		runtime.Bindings = append(runtime.Bindings, TodoSessionBinding{
			SessionID: id, TodoID: todo.ID, BoundAt: boundAt,
		})
		runtime.Sessions = append(runtime.Sessions, TodoBoundSession{
			SessionID: id, Summary: fmt.Sprintf("Design unrelated dashboard color variant %d", index),
			Indexed: true, LastAt: boundAt,
		})
	}
	firstSubmit := now.Add(-72 * time.Hour).UnixNano()
	runtime.Effects = []WorkEffectRecord{
		{ID: "e1", TodoID: todo.ID, Kind: "todo_submitted", CreatedAt: firstSubmit},
		{ID: "e2", TodoID: todo.ID, Kind: "todo_started", CreatedAt: now.Add(-50 * time.Hour).UnixNano()},
		{ID: "e3", TodoID: todo.ID, Kind: "todo_submitted", CreatedAt: now.Add(-49 * time.Hour).UnixNano()},
	}

	issues := LintTodoLifecycle(portfolio, &todo, runtime)
	want := map[string]bool{
		"legacy_creator_missing":         false,
		"p1_without_deadline_or_blocker": false,
		"priority_p1_concentration":      false,
		"binding_sprawl":                 false,
		"unobserved_binding":             false,
		"multiple_submit":                false,
		"post_submit_work":               false,
		"scope_drift":                    false,
	}
	for _, issue := range issues {
		if _, ok := want[issue.Code]; ok {
			want[issue.Code] = true
			if len(issue.Details) == 0 {
				t.Errorf("%s has no machine-readable details: %+v", issue.Code, issue)
			}
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing %s in %+v", code, issues)
		}
	}
}

func TestLintTodoLifecycleSkipsBatchHeuristicsForBoundedMaintenance(t *testing.T) {
	todo := Todo{
		ID: "t1", Title: "DartX film batch", Priority: "P1", Status: TodoStatusInProgress,
		Tags: []string{TodoTagMaintenance}, MaintenanceLimit: 10, Creator: TodoCreatorMe, Created: Today(),
	}
	runtime := TodoLintRuntime{}
	for index := 0; index < TodoLintBindingSprawlThreshold; index++ {
		id := fmt.Sprintf("s%d", index)
		runtime.Bindings = append(runtime.Bindings, TodoSessionBinding{SessionID: id, TodoID: todo.ID})
		runtime.Sessions = append(runtime.Sessions, TodoBoundSession{SessionID: id, Summary: fmt.Sprintf("unrelated %d", index)})
	}
	for _, issue := range LintTodoLifecycle(&TodoFile{Items: []Todo{todo}}, &todo, runtime) {
		if issue.Code == "binding_sprawl" || issue.Code == "scope_drift" {
			t.Fatalf("bounded maintenance got %s: %+v", issue.Code, issue)
		}
	}
}

func TestCompletionReasonValidationAndLint(t *testing.T) {
	for _, reason := range []string{"", "done", TodoGUICompletionReceipt} {
		if err := ValidateTodoCompletionReason(reason); err == nil {
			t.Errorf("accepted generic completion reason %q", reason)
		}
	}
	if err := ValidateTodoCompletionReason("reviewed MR 42 and reran parser tests"); err != nil {
		t.Fatalf("rejected evidence: %v", err)
	}
	reason := "done"
	todo := Todo{ID: "t1", Status: TodoStatusDone, ClosedReason: &reason, Creator: TodoCreatorMe}
	issues := LintTodoLifecycle(&TodoFile{Items: []Todo{todo}}, &todo, TodoLintRuntime{})
	if len(issues) != 1 || issues[0].Code != "generic_completion_reason" {
		t.Fatalf("issues = %+v", issues)
	}
	for _, receipt := range []string{TodoGUICompletionReceipt} {
		todo.ClosedReason = &receipt
		if issues := LintTodoLifecycle(&TodoFile{Items: []Todo{todo}}, &todo, TodoLintRuntime{}); len(issues) != 0 {
			t.Errorf("GUI receipt %q should not require evidence: %+v", receipt, issues)
		}
	}
}

func TestLintTodoLifecycleOnlySuggestsCanonicalAndCreatorRepairs(t *testing.T) {
	oldAliases := config.ProjectAliases
	config.ProjectAliases = map[string]string{"ATM-WORKTREE": "atm"}
	t.Cleanup(func() { config.ProjectAliases = oldAliases })
	todo := Todo{ID: "t1", Project: "ATM-WORKTREE", Priority: "P2", Status: TodoStatusOpen}
	issues := LintTodoLifecycle(&TodoFile{Items: []Todo{todo}}, &todo, TodoLintRuntime{})
	codes := map[string]bool{}
	for _, issue := range issues {
		codes[issue.Code] = true
		if automatic, ok := issue.Details["automatic_fix"].(bool); ok && automatic {
			t.Fatalf("legacy lint promised an automatic fix: %+v", issue)
		}
	}
	if !codes["legacy_creator_missing"] || !codes["noncanonical_project"] {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestP1RationaleAvoidsDeadlineAdvisory(t *testing.T) {
	for _, description := range []string{
		"Deadline: 2026-09-03 because the release freezes next week.",
		"阻塞发布，等待上游修复。",
	} {
		todo := Todo{
			ID: "t1", Title: "Urgent work", Description: description, Priority: "P1",
			Status: TodoStatusOpen, Creator: TodoCreatorMe,
		}
		for _, issue := range LintTodoLifecycle(&TodoFile{Items: []Todo{todo}}, &todo, TodoLintRuntime{}) {
			if issue.Code == "p1_without_deadline_or_blocker" {
				t.Fatalf("explicit rationale %q got advisory: %+v", description, issue)
			}
		}
	}
}
