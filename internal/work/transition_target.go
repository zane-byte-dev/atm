package work

import (
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type transitionTargetSpec struct {
	effectKind    EffectKind
	currentStatus string
	unboundReason string
}

var (
	submitTransitionTarget = transitionTargetSpec{
		effectKind: EffectTodoSubmitted, currentStatus: store.TodoStatusReview,
		unboundReason: "submit:review",
	}
	doneTransitionTarget = transitionTargetSpec{
		effectKind: EffectTodoClosed, currentStatus: store.TodoStatusDone,
		unboundReason: store.TodoStatusDone,
	}
)

// resolveTransitionTodoID owns implicit target selection for bound Work
// commands. A live binding is the normal path. After Submit has committed and
// closed that binding, a retry may use the newest history row only when its
// closure reason, the Todo's current lifecycle and a pending durable effect all
// agree. Keeping this inside WorkStateTx makes the decision share the same
// serialized snapshot as the transition that follows it.
func (transaction *Transaction) resolveTransitionTodoID(
	explicitTodoID, sessionID string,
	spec transitionTargetSpec,
) (string, error) {
	if strings.TrimSpace(explicitTodoID) != "" {
		return store.NormalizeTodoID(explicitTodoID), nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", transitionTargetUnavailable("")
	}
	current, err := transaction.currentSessionBinding(sessionID)
	if err != nil {
		return "", err
	}
	if current != nil {
		return current.TodoID, nil
	}

	latest, err := transaction.latestSessionBinding(sessionID)
	if err != nil {
		return "", err
	}
	if latest == nil || latest.UnboundAt == nil || latest.Reason != spec.unboundReason {
		return "", transitionTargetUnavailable(sessionID)
	}
	todo := store.FindTodo(transaction.Todos(), latest.TodoID)
	if todo == nil || todo.Status != spec.currentStatus {
		return "", transitionTargetUnavailable(sessionID)
	}
	records, err := transaction.state.PendingWorkEffects(todo.ID)
	if err != nil {
		return "", err
	}
	for _, record := range records {
		if record.Kind == string(spec.effectKind) {
			return todo.ID, nil
		}
	}
	return "", transitionTargetUnavailable(sessionID)
}

func transitionTargetUnavailable(sessionID string) *application.Error {
	message := "todo ID is required when no Todo is bound to the current session"
	if sessionID != "" {
		message = fmt.Sprintf("no Todo is bound to session %s and no matching transition effect is pending", sessionID)
	}
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": "todo_id", "session_id": sessionID}
	return err
}
