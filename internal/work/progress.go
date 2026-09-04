package work

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type ProgressInput struct {
	TodoID       string `json:"todo_id"`
	ExpectedETag string `json:"expected_etag"`
	Message      string `json:"message"`
}

type ProgressResult struct {
	Todo    Todo     `json:"todo"`
	Effects []Effect `json:"-"`
}

// AppendProgress commits an append intent under the Todo write lock. Its
// document effect survives a failed delivery; it does not change lifecycle.
func (service Service) AppendProgress(ctx context.Context, call application.Call, input ProgressInput) (ProgressResult, error) {
	if err := validateMetadataCall(ctx, call); err != nil {
		return ProgressResult{}, err
	}
	if !store.LooksLikeTodoID(input.TodoID) {
		return ProgressResult{}, metadataInvalidArgument("invalid todo ID", "todo_id", input.TodoID)
	}
	message := strings.TrimSpace(input.Message)
	if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
		return ProgressResult{}, logInvalidArgument(err.Error(), "message", input.Message)
	}
	var result ProgressResult
	err := service.Mutate(func(tx *Transaction) error {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		todo, err := tx.Todo(input.TodoID)
		if err != nil {
			return metadataTodoNotFound(input.TodoID, err)
		}
		if err := checkExpectedTodo(call, *todo, input.ExpectedETag); err != nil {
			return err
		}
		if unknown := store.UnknownTodoReferences(tx.Todos(), message); len(unknown) > 0 {
			return logInvalidArgument("progress references unknown todo IDs: "+strings.Join(unknown, ", "), "message", input.Message)
		}
		if err := tx.enqueueEffect(call, EffectTodoProgress, *todo, message); err != nil {
			return err
		}
		pending, err := tx.pendingEffects(todo.ID)
		if err != nil {
			return err
		}
		for _, effect := range pending {
			if effect.Kind == EffectTodoProgress {
				result.Effects = append(result.Effects, effect)
			}
		}
		result.Todo = cloneTodo(*todo)
		return nil
	})
	if err != nil {
		return ProgressResult{}, metadataApplicationError("append todo progress", err)
	}
	return result, nil
}

// ApplyProgressEffect is the filesystem projection used by local adapters.
// The entry and its effect marker are one atomic replacement, so replay after
// a crash cannot append twice. Two distinct actions with equal text remain two
// entries. The marker is separate from the user's 400-character progress text.
func ApplyProgressEffect(effect Effect) error {
	if effect.Kind != EffectTodoProgress || effect.ID == "" || strings.ContainsAny(effect.ID, "<>\r\n") || !store.LooksLikeTodoID(effect.TodoID) {
		return fmt.Errorf("invalid progress effect")
	}
	if err := store.ValidateTodoLogMessage(effect.Message, "进展"); err != nil {
		return err
	}
	return withPlanDocumentLock(effect.TodoID, func() error {
		todo := effect.Todo
		if _, err := store.EnsureTodoDoc(&todo); err != nil {
			return err
		}
		content, err := store.ReadTodoDoc(effect.TodoID)
		if err != nil {
			return err
		}
		marker := "<!-- atm-progress:" + effect.ID + " -->"
		if strings.Contains(content, marker) {
			return nil
		}
		timestamp := time.Unix(0, effect.CreatedAt).In(config.Loc).Format("2006-01-02 15:04")
		entry := fmt.Sprintf("- [%s] %s\n", timestamp, effect.Message)
		heading := "\n## 进展\n"
		start := strings.Index(content, heading)
		if start < 0 {
			content = strings.TrimRight(content, "\n") + "\n\n## 进展\n\n" + entry
		} else {
			body := start + len(heading)
			next := strings.Index(content[body:], "\n## ")
			if next < 0 {
				content = strings.TrimRight(content, "\n") + "\n" + entry
			} else {
				end := body + next
				content = strings.TrimRight(content[:end], "\n") + "\n" + entry + "\n" + content[end:]
			}
		}
		// Keep delivery metadata in the header, outside the progress section:
		// progress lint and summaries count only the user's actual paragraph.
		if headerEnd := strings.Index(content, "\n## "); headerEnd >= 0 {
			content = content[:headerEnd] + "\n" + marker + "\n" + content[headerEnd:]
		} else {
			return fmt.Errorf("todo document has no section boundary")
		}
		path := store.TodoDocPath(effect.TodoID)
		file, err := os.CreateTemp(filepath.Dir(path), ".progress-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err := file.WriteString(content); err != nil {
			file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return os.Rename(file.Name(), path)
	})
}
