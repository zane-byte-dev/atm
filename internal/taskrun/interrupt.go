package taskrun

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type InterruptInput struct {
	TodoID string `json:"todo_id"`
}

type InterruptResult struct {
	Run Run `json:"run"`
}

// Interrupt is a human control-plane action. CLI actor attribution comes from
// ambient environment and is not strong operating-system authentication, but
// enforcing the policy here still prevents an ordinary Agent path from killing
// its controller. Controller-owned cancellation must use an internal port.
func (service Service) Interrupt(
	ctx context.Context,
	call application.Call,
	input InterruptInput,
) (InterruptResult, error) {
	if err := validateCall(ctx, call); err != nil {
		return InterruptResult{}, err
	}
	if call.Actor.Kind != application.ActorHuman {
		return InterruptResult{}, forbidden("only a human actor may interrupt an agent run", map[string]any{
			"actor_kind": call.Actor.Kind, "required_actor_kind": application.ActorHuman,
		})
	}
	todoID, err := requireTodo(input.TodoID)
	if err != nil {
		return InterruptResult{}, err
	}

	readDB, err := store.OpenReadOnly()
	if err != nil {
		return InterruptResult{}, unavailable("open active task run", err)
	}
	run, queryErr := store.ActiveTaskRun(readDB, todoID)
	_ = readDB.Close()
	if queryErr != nil {
		return InterruptResult{}, unavailable("find active task run", queryErr)
	}
	if run == nil {
		return InterruptResult{}, conflict(fmt.Sprintf("todo %s has no active agent run", todoID), map[string]any{
			"todo_id": todoID,
		}, nil)
	}
	if run.PID <= 0 {
		return InterruptResult{}, busy(fmt.Sprintf("agent run %s is still starting; try again shortly", run.ID), map[string]any{
			"todo_id": todoID, "run_id": run.ID, "status": run.Status,
		})
	}
	if err := service.process.Interrupt(run.PID); err != nil {
		return InterruptResult{}, unavailable(fmt.Sprintf("interrupt agent run %s", run.ID), err)
	}

	finished := service.clock.Now()
	message := "interrupted by user"
	writeDB, err := store.Open()
	if err != nil {
		return InterruptResult{}, unavailable("open task run for interruption", err)
	}
	interruptErr := store.InterruptTaskRun(writeDB, run.ID, finished.Unix(), message)
	_ = writeDB.Close()
	if interruptErr != nil {
		if strings.Contains(interruptErr.Error(), "is not active") {
			return InterruptResult{}, conflict(
				fmt.Sprintf("agent run %s finished before interruption could be recorded", run.ID),
				map[string]any{"todo_id": todoID, "run_id": run.ID}, interruptErr,
			)
		}
		return InterruptResult{}, unavailable("record task run interruption", interruptErr)
	}

	// The status transition is authoritative. Log and desktop attention cleanup
	// are best-effort projections, matching the controller's existing behavior.
	_ = service.logs.Append(run.LogPath, []byte(fmt.Sprintf(
		"\nATM run interrupted by user at %s\n", finished.Format(time.RFC3339),
	)))
	_ = service.events.ReportEnded(*run, finished)

	readDB, err = store.OpenReadOnly()
	if err != nil {
		return InterruptResult{}, unavailable("reload interrupted task run", err)
	}
	updated, reloadErr := store.GetTaskRun(readDB, run.ID)
	_ = readDB.Close()
	if reloadErr != nil {
		return InterruptResult{}, unavailable("reload interrupted task run", reloadErr)
	}
	if updated == nil {
		return InterruptResult{}, notFound("interrupted task run disappeared", "run_id", run.ID, nil)
	}
	return InterruptResult{Run: *updated}, nil
}
