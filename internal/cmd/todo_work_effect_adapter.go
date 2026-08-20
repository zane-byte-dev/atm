package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

// localWorkEffectExecutor is the controlled filesystem/desktop/shell edge for
// durable Work effects. Work application services describe the required
// projection; they never import command rendering or execute user-configured
// shell text.
type localWorkEffectExecutor struct{}

func (localWorkEffectExecutor) ApplyWorkEffect(event workapp.Effect) error {
	todo := event.Todo
	switch event.Kind {
	case workapp.EffectTodoSubmitted:
		if err := appendWorkEffectLogOnce(todo, event.Message); err != nil {
			return fmt.Errorf("append todo log after submit: %w", err)
		}
		notifyTodoEvent(&todo, notifyEventReview)
		return syncWorkEffectDocument(todo, "after submit")
	case workapp.EffectTodoWaiting:
		return syncWorkEffectDocument(todo, "")
	case workapp.EffectTodoStarted:
		return syncWorkEffectDocument(todo, "after start")
	case workapp.EffectTodoUpdated:
		return syncWorkEffectDocument(todo, "after update")
	case workapp.EffectTodoRefined:
		if _, err := store.EnsureTodoDoc(&todo); err != nil {
			return fmt.Errorf("materialize refined todo document %s: %w", todo.ID, err)
		}
		for index := range event.RelatedTodos {
			child := event.RelatedTodos[index]
			if _, err := store.EnsureTodoDoc(&child); err != nil {
				return fmt.Errorf("materialize refine child document %s: %w", child.ID, err)
			}
		}
		return appendRefinementAnalysisOnce(todo, event.Message)
	case workapp.EffectTodoClosed:
		if err := syncWorkEffectDocument(todo, "after close"); err != nil {
			return err
		}
		// Historically a close reason is recorded only when the Todo already has
		// a document; closing a lightweight card must not materialize one merely
		// to store an optional note.
		if event.Message != "" && store.TodoDocExists(todo.ID) {
			if err := appendWorkEffectLogOnce(todo, event.Message); err != nil {
				return fmt.Errorf("append todo close log: %w", err)
			}
		}
		// The close effect only ever means accepted-as-done now. It used to also
		// carry 放弃, which was a status this branch checked for; setting work
		// aside archives it instead, and archival is not a close.
		notifyTodoEvent(&todo, notifyEventDone)
		if todo.Status == "done" && strings.TrimSpace(todo.OnDone) != "" {
			// Delivery is at-least-once. A process crash after Start and before the
			// outbox acknowledgement can launch this command again; OnDone hooks
			// must therefore be written as idempotent automation.
			fmt.Fprintf(os.Stderr, "on-done: %s\n", todo.OnDone)
			command := exec.Command("sh", "-c", todo.OnDone)
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
			if err := command.Start(); err != nil {
				return fmt.Errorf("start on-done command: %w", err)
			}
		}
		return nil
	case workapp.EffectTodoAwakened, workapp.EffectTodoDependencyAwakened:
		if err := appendWorkEffectLogOnce(todo, event.Message); err != nil {
			return fmt.Errorf("append todo wake log: %w", err)
		}
		return syncWorkEffectDocument(todo, "after wake")
	default:
		return fmt.Errorf("unknown work effect %q", event.Kind)
	}
}

func appendRefinementAnalysisOnce(todo workapp.Todo, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(message))
	marker := fmt.Sprintf("<!-- atm-refine:%x -->", digest[:8])
	content, err := store.ReadTodoDoc(todo.ID)
	if err != nil {
		return err
	}
	if strings.Contains(content, marker) {
		return nil
	}
	_, err = store.AppendTodoLog(&todo, message+"\n\n"+marker, "分析")
	return err
}

func appendWorkEffectLogOnce(todo workapp.Todo, message string) error {
	if message == "" {
		return nil
	}
	if store.TodoDocExists(todo.ID) {
		content, err := store.ReadTodoDoc(todo.ID)
		if err != nil {
			return err
		}
		if strings.Contains(content, "] "+message+"\n") {
			return nil
		}
	}
	_, err := store.AppendTodoLog(&todo, message, "")
	return err
}

func syncWorkEffectDocument(todo workapp.Todo, suffix string) error {
	if !store.TodoDocExists(todo.ID) {
		return nil
	}
	if err := store.SyncTodoDocMetadata(&todo); err != nil {
		message := fmt.Sprintf("sync todo doc %s", todo.ID)
		if suffix != "" {
			message += " " + suffix
		}
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}
