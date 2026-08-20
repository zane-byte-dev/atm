package taskrun

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type ListInput struct {
	TodoID string `json:"todo_id"`
}

type ListResult struct {
	Runs []Run `json:"runs"`
}

func (Service) List(ctx context.Context, call application.Call, input ListInput) (ListResult, error) {
	if err := validateCall(ctx, call); err != nil {
		return ListResult{}, err
	}
	todoID, err := requireTodo(input.TodoID)
	if err != nil {
		return ListResult{}, err
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return ListResult{}, unavailable("open task run history", err)
	}
	defer db.Close()
	runs, err := store.ListTaskRuns(db, todoID)
	if err != nil {
		return ListResult{}, unavailable("list task runs", err)
	}
	if runs == nil {
		runs = []Run{}
	}
	return ListResult{Runs: runs}, nil
}
