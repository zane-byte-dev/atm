package work

import (
	"context"
	"errors"
	"os"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type TodoLintIssue = store.TodoLintIssue

type LintInput struct {
	TodoID string `json:"todo_id,omitempty"`
}

type LintSummary struct {
	Issues int `json:"issues"`
}

type LintResult struct {
	TodoID  string          `json:"todo_id"`
	Issues  []TodoLintIssue `json:"issues"`
	Summary LintSummary     `json:"summary"`
}

// Lint reads one live Todo and its optional Markdown projection, then applies
// the consistency rules owned by the Work domain. A missing card is a useful
// diagnostic result rather than an infrastructure failure.
func (service Service) Lint(ctx context.Context, call application.Call, input LintInput) (LintResult, error) {
	todos, todo, err := loadTodoForRead(ctx, call, input.TodoID)
	if err != nil {
		return LintResult{}, err
	}

	issues := []TodoLintIssue{}
	content, err := store.ReadTodoDoc(todo.ID)
	switch {
	case os.IsNotExist(err):
		issues = append(issues, TodoLintIssue{
			Severity:   "info",
			Code:       "doc_missing",
			Detail:     "the todo has no markdown card",
			Suggestion: "run `atm todo doc " + todo.ID + " --init` or record the first milestone to create it",
		})
	case err != nil:
		return LintResult{}, lintUnavailable("read todo document", err)
	default:
		issues, err = store.LintTodoDoc(todos, todo, content)
		if err != nil {
			return LintResult{}, lintApplicationError("lint todo document", err)
		}
		if issues == nil {
			issues = []TodoLintIssue{}
		}
	}

	return LintResult{
		TodoID: todo.ID,
		Issues: issues,
		Summary: LintSummary{
			Issues: len(issues),
		},
	}, nil
}

func lintUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}

func lintApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return lintUnavailable(operation, err)
}
