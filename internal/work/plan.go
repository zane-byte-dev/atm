package work

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

const (
	maxPlanItems       = 100
	maxPlanStepRunes   = 2000
	maxPlanExplanation = 8000
)

const (
	PlanPending    = "pending"
	PlanInProgress = "in_progress"
	PlanCompleted  = "completed"
)

type SetPlanInput struct {
	TodoID       string     `json:"todo_id,omitempty"`
	BaseRevision int64      `json:"base_revision"`
	Explanation  string     `json:"explanation,omitempty"`
	Items        []PlanItem `json:"items"`
}

type SetPlanResult struct {
	Plan    PlanSnapshot `json:"plan"`
	Changed bool         `json:"changed"`
}

type persistedPlanSnapshot struct {
	Explanation string     `json:"explanation,omitempty"`
	Items       []PlanItem `json:"items"`
}

// SetPlan replaces the latest execution plan atomically with one immutable
// snapshot. It deliberately does not mutate Todo lifecycle: even a plan whose
// items are all completed still needs an explicit Submit (Agent) or Done
// (human) transition.
func (service Service) SetPlan(ctx context.Context, call application.Call, input SetPlanInput) (SetPlanResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SetPlanResult{}, err
	}
	if err := call.Validate(); err != nil {
		return SetPlanResult{}, err
	}
	if input.BaseRevision < 0 {
		return SetPlanResult{}, readInvalidArgument("base revision must not be negative", "base_revision", input.BaseRevision)
	}

	snapshot, snapshotJSON, snapshotHash, err := normalizePlanSnapshot(input.Explanation, input.Items)
	if err != nil {
		return SetPlanResult{}, err
	}

	var result SetPlanResult
	var todo store.Todo
	err = service.Mutate(func(transaction *Transaction) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		todoID, binding, err := transaction.resolvePlanTodo(input.TodoID, call.Actor.SessionID)
		if err != nil {
			return err
		}
		currentTodo, err := transaction.Todo(todoID)
		if err != nil {
			return planNotFound(todoID, err)
		}
		todo = *currentTodo

		latest, err := transaction.state.LatestTodoPlanRevision(todo.ID)
		if err != nil {
			return readApplicationError("load latest todo plan", err)
		}
		if latest != nil && latest.SnapshotHash == snapshotHash {
			plan, err := decodePlanSnapshot(latest)
			if err != nil {
				return readApplicationError("decode latest todo plan", err)
			}
			result = SetPlanResult{Plan: plan, Changed: false}
			return nil
		}
		currentRevision := int64(0)
		if latest != nil {
			currentRevision = latest.Revision
		}
		if input.BaseRevision != currentRevision {
			return planRevisionConflict(input.BaseRevision, currentRevision)
		}

		bindingID := call.Actor.BindingID
		if bindingID == 0 && binding != nil {
			bindingID = binding.ID
		}
		var storedBindingID *int64
		if bindingID > 0 {
			storedBindingID = &bindingID
		}
		revision := store.TodoPlanRevision{
			TodoID:       todo.ID,
			Revision:     currentRevision + 1,
			BaseRevision: input.BaseRevision,
			SnapshotJSON: snapshotJSON,
			SnapshotHash: snapshotHash,
			RequestID:    call.RequestID,
			ActorKind:    string(call.Actor.Kind),
			Origin:       string(call.Actor.Origin),
			SessionID:    strings.TrimSpace(call.Actor.SessionID),
			BindingID:    storedBindingID,
			Agent:        strings.TrimSpace(call.Actor.Agent),
			CreatedAt:    time.Now().UTC().UnixNano(),
		}
		if err := transaction.state.AppendTodoPlanRevision(revision); err != nil {
			return readApplicationError("append todo plan revision", err)
		}
		result = SetPlanResult{
			Plan:    planSnapshotFromParts(revision, snapshot),
			Changed: true,
		}
		return nil
	})
	if err != nil {
		var appErr *application.Error
		if errors.As(err, &appErr) {
			return SetPlanResult{}, appErr
		}
		return SetPlanResult{}, readApplicationError("set todo plan", err)
	}

	// The Markdown card is a repairable projection. Retrying the same snapshot
	// takes the idempotent path above and still reaches this sync, so a process
	// crash after the database commit cannot leave it permanently stale.
	if _, err := syncTodoDocumentWithLatestPlan(&todo); err != nil {
		return SetPlanResult{}, readApplicationError("sync todo plan document", err)
	}
	return result, nil
}

func normalizePlanSnapshot(explanation string, items []PlanItem) (persistedPlanSnapshot, string, string, error) {
	explanation = strings.TrimSpace(explanation)
	if len([]rune(explanation)) > maxPlanExplanation {
		return persistedPlanSnapshot{}, "", "", readInvalidArgument(
			fmt.Sprintf("plan explanation exceeds %d characters", maxPlanExplanation), "explanation", len([]rune(explanation)),
		)
	}
	if len(items) > maxPlanItems {
		return persistedPlanSnapshot{}, "", "", readInvalidArgument(
			fmt.Sprintf("plan has more than %d items", maxPlanItems), "items", len(items),
		)
	}
	normalized := make([]PlanItem, len(items))
	inProgress := 0
	for index, item := range items {
		step := strings.TrimSpace(item.Step)
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if step == "" {
			return persistedPlanSnapshot{}, "", "", readInvalidArgument("plan step must not be empty", fmt.Sprintf("items[%d].step", index), item.Step)
		}
		if len([]rune(step)) > maxPlanStepRunes {
			return persistedPlanSnapshot{}, "", "", readInvalidArgument(
				fmt.Sprintf("plan step exceeds %d characters", maxPlanStepRunes), fmt.Sprintf("items[%d].step", index), len([]rune(step)),
			)
		}
		switch status {
		case PlanPending, PlanCompleted:
		case PlanInProgress:
			inProgress++
		default:
			return persistedPlanSnapshot{}, "", "", readInvalidArgument(
				"plan status must be pending, in_progress, or completed", fmt.Sprintf("items[%d].status", index), item.Status,
			)
		}
		normalized[index] = PlanItem{Step: step, Status: status}
	}
	if inProgress > 1 {
		return persistedPlanSnapshot{}, "", "", readInvalidArgument("plan may contain at most one in_progress item", "items", inProgress)
	}
	snapshot := persistedPlanSnapshot{Explanation: explanation, Items: normalized}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return persistedPlanSnapshot{}, "", "", readApplicationError("encode todo plan", err)
	}
	hash := sha256.Sum256(encoded)
	return snapshot, string(encoded), hex.EncodeToString(hash[:]), nil
}

func (transaction *Transaction) resolvePlanTodo(rawTodoID, sessionID string) (string, *store.TodoSessionBinding, error) {
	todoID := strings.TrimSpace(rawTodoID)
	sessionID = strings.TrimSpace(sessionID)
	var binding *store.TodoSessionBinding
	if sessionID != "" {
		var err error
		binding, err = transaction.currentSessionBinding(sessionID)
		if err != nil {
			return "", nil, readApplicationError("resolve current todo binding", err)
		}
	}
	if todoID == "" || strings.EqualFold(todoID, "current") {
		if sessionID == "" {
			return "", nil, readInvalidArgument("session ID is required to resolve the current todo", "actor.session_id", sessionID)
		}
		if binding == nil {
			err := application.NewError(application.CodeNotFound, "no todo is bound to the current session")
			err.Details = map[string]any{"session_id": sessionID}
			return "", nil, err
		}
		todoID = binding.TodoID
	}
	todoID = store.NormalizeTodoID(todoID)
	if binding != nil && binding.TodoID != todoID {
		binding = nil
	}
	return todoID, binding, nil
}

func latestPlanSnapshot(todoID string) (*PlanSnapshot, error) {
	revision, err := store.LatestTodoPlanRevision(todoID)
	if err != nil {
		return nil, err
	}
	if revision == nil {
		return nil, nil
	}
	plan, err := decodePlanSnapshot(revision)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func decodePlanSnapshot(revision *store.TodoPlanRevision) (PlanSnapshot, error) {
	var snapshot persistedPlanSnapshot
	if err := json.Unmarshal([]byte(revision.SnapshotJSON), &snapshot); err != nil {
		return PlanSnapshot{}, fmt.Errorf("decode todo plan %s revision %d: %w", revision.TodoID, revision.Revision, err)
	}
	return planSnapshotFromParts(*revision, snapshot), nil
}

func planSnapshotFromParts(revision store.TodoPlanRevision, snapshot persistedPlanSnapshot) PlanSnapshot {
	bindingID := int64(0)
	if revision.BindingID != nil {
		bindingID = *revision.BindingID
	}
	return PlanSnapshot{
		TodoID: revision.TodoID, Revision: revision.Revision, Explanation: snapshot.Explanation, Items: snapshot.Items,
		CreatedAt: revision.CreatedAt, ActorKind: application.ActorKind(revision.ActorKind),
		Origin: application.Origin(revision.Origin), SessionID: revision.SessionID,
		BindingID: bindingID, Agent: revision.Agent,
	}
}

func planRevisionConflict(base, current int64) *application.Error {
	err := application.NewError(application.CodeConflict,
		fmt.Sprintf("todo plan revision conflict: base %d, current %d", base, current))
	err.Details = map[string]any{"base_revision": base, "current_revision": current}
	return err
}

func planNotFound(todoID string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, fmt.Sprintf("todo not found: %s", todoID), cause)
	err.Details = map[string]any{"todo_id": todoID}
	return err
}
