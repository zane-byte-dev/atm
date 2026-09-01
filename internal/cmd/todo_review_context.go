package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

type todoContext struct {
	WorkState      workapp.Todo                  `json:"work_state"`
	TaskDocument   workapp.ContextTaskDocument   `json:"task_document"`
	Implementation workapp.ContextImplementation `json:"implementation"`
	Verification   reviewVerification            `json:"verification"`
	Trace          workapp.ContextTrace          `json:"trace"`
	LatestPlan     *workapp.PlanSnapshot         `json:"latest_plan,omitempty"`
}

type reviewVerification struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func runTodoContext(cmd *cobra.Command, args []string) error {
	contextSnapshot, err := buildTodoContextWithContext(cmd.Context(), todoReadID(args), todoContextCWD)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(contextSnapshot)
		return nil
	}
	printTodoContext(contextSnapshot)
	return nil
}

func buildTodoContext(id, cwdOverride string) (*todoContext, error) {
	return buildTodoContextWithContext(context.Background(), id, cwdOverride)
}

func buildTodoContextWithContext(ctx context.Context, id, cwdOverride string) (*todoContext, error) {
	result, err := workapp.Default.Context(
		ctx, todoReadCall("todo-context"),
		workapp.ContextInput{TodoID: id, CWD: cwdOverride},
	)
	if err != nil {
		return nil, err
	}
	return &todoContext{
		WorkState:      result.WorkState,
		TaskDocument:   result.TaskDocument,
		Implementation: result.Implementation,
		Verification: reviewVerification{
			Status: string(result.Verification.Status),
			Note:   "context does not run tests; reported milestones are trace evidence, not verified results",
		},
		Trace:      result.Trace,
		LatestPlan: result.LatestPlan,
	}, nil
}

func printTodoContext(contextSnapshot *todoContext) {
	todo := contextSnapshot.WorkState
	fmt.Printf("Todo:         %s [%s] %s\n", todo.ID, todo.Status, todo.Title)
	if todo.Project != "" {
		fmt.Printf("Project:      %s\n", todo.Project)
	}
	if todo.Description != "" {
		fmt.Printf("Goal:         %s\n", strings.ReplaceAll(todo.Description, "\n", "\n              "))
	}
	if plan := contextSnapshot.LatestPlan; plan != nil {
		fmt.Printf("Plan:         revision %d (%d item(s))\n", plan.Revision, len(plan.Items))
		for _, item := range plan.Items {
			fmt.Printf("  %-11s %s\n", item.Status, strings.ReplaceAll(item.Step, "\n", " "))
		}
	}

	trace := contextSnapshot.Trace
	fmt.Printf("Trace:        %d binding(s)", trace.BindingCount)
	if len(trace.RecentBindings) > 0 {
		latest := trace.RecentBindings[0]
		fmt.Printf("; latest %s session %s", emptyAs(latest.Agent, "unknown"), shortSessionID(latest.SessionID))
	}
	fmt.Println()
	if contextSnapshot.TaskDocument.Error != "" {
		fmt.Printf("Task doc:     unavailable: %s\n", contextSnapshot.TaskDocument.Error)
	}

	implementation := contextSnapshot.Implementation
	fmt.Printf("Git:          %s (%s)\n", implementation.CWD, implementation.WorkspaceSource)
	if !implementation.Available {
		fmt.Printf("Git status:   unavailable: %s\n", implementation.Error)
	} else {
		revision := implementation.Branch
		if revision == "" {
			revision = "(detached)"
		}
		if implementation.Head != "" {
			revision += " @ " + shortReviewRevision(implementation.Head)
		}
		fmt.Printf("Revision:     %s\n", revision)
		printReviewFileGroup("Staged", implementation.Staged)
		printReviewFileGroup("Unstaged", implementation.Unstaged)
		printReviewFileGroup("Untracked", implementation.Untracked)
		if implementation.Error != "" {
			fmt.Printf("Git warning:  %s\n", implementation.Error)
		}
	}
	fmt.Printf("Verification: %s — %s\n", contextSnapshot.Verification.Status, contextSnapshot.Verification.Note)
}

func printReviewFileGroup(label string, paths []string) {
	fmt.Printf("%-13s %d\n", label+":", len(paths))
	for _, path := range paths {
		fmt.Printf("  - %s\n", path)
	}
}

func shortReviewRevision(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

// Task-run cwd selection historically shared this canonicalization helper with
// context. Keep the command-level name as a compatibility shim while Work owns
// the rule itself.
func cleanReviewContextCWD(value string) string {
	return workapp.NormalizeContextCWD(value)
}
