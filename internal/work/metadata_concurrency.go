package work

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// TodoETag identifies the persisted business content of a Todo. It deliberately
// excludes derived attachment paths, which change when ATM_HOME moves, and
// sorts set-valued fields so a transport's ordering cannot create a conflict.
// The returned token has no HTTP quotes; adapters own their wire encoding.
func TodoETag(todo Todo) string {
	payload, _ := json.Marshal(todoContentSnapshot(todo))
	return "todo-v1-" + contentHash(payload)
}

type todoImageSnapshot struct {
	Name       string `json:"name"`
	StoredName string `json:"stored_name"`
	MediaType  string `json:"media_type"`
	SizeBytes  int64  `json:"size_bytes"`
}

// Todo's public image JSON exposes a derived Path and hides StoredName. A
// durable create response needs the reverse, both for replay after relocation
// and for an ETag that includes the actual stored attachment identity.
type todoSnapshot struct {
	Todo   Todo                `json:"todo"`
	Images []todoImageSnapshot `json:"images,omitempty"`
}

func todoContentSnapshot(todo Todo) todoSnapshot {
	copy := cloneTodo(todo)
	sort.Strings(copy.Tags)
	sort.Strings(copy.DependsOn)
	snapshot := todoSnapshot{Todo: copy}
	snapshot.Todo.Images = nil
	for _, image := range todo.Images {
		snapshot.Images = append(snapshot.Images, todoImageSnapshot{
			Name: image.Name, StoredName: image.StoredName,
			MediaType: image.MediaType, SizeBytes: image.SizeBytes,
		})
	}
	return snapshot
}

func restoreCreateSnapshot(encoded string, todo *Todo) error {
	var snapshot todoSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return fmt.Errorf("decode persisted todo create response: %w", err)
	}
	if snapshot.Todo.ID == "" {
		return fmt.Errorf("persisted todo create response has no todo ID")
	}
	*todo = snapshot.Todo
	for _, image := range snapshot.Images {
		todo.Images = append(todo.Images, store.TodoImage{
			Name: image.Name, StoredName: image.StoredName,
			MediaType: image.MediaType, SizeBytes: image.SizeBytes,
			Path: filepath.Join(store.TodoAssetsDir(todo.ID), image.StoredName),
		})
	}
	return nil
}

func (transaction *Transaction) recordTodoCreate(key, payloadHash string, todo Todo) error {
	encoded, err := json.Marshal(todoContentSnapshot(todo))
	if err != nil {
		return err
	}
	return transaction.state.RecordTodoCreate(store.TodoCreateRecord{
		Key: key, PayloadHash: payloadHash, TodoID: todo.ID,
		ResultJSON: string(encoded), CreatedAt: time.Now().UnixMilli(),
	})
}

func addPayloadHash(input normalizedAdd) string {
	// Hash the normalized creation intent, never the assigned ID, the current
	// date, or request provenance. A reconnect has a new request ID but remains
	// the same user action.
	payload, _ := json.Marshal(AddInput{
		Title: input.title, Description: input.description, Priority: input.priority,
		Project: input.project, Source: input.source, Creator: input.creator,
		OnDone: input.onDone, ImagePaths: input.imagePaths,
	})
	return contentHash(payload)
}

func contentHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func normalizeCreateIdempotencyKey(call application.Call, value string) (string, error) {
	if value == "" {
		if call.Actor.Origin == application.OriginWeb {
			return "", metadataInvalidArgument("idempotency_key is required for Web todo creation", "idempotency_key", value)
		}
		return "", nil
	}
	if len(value) > 200 || strings.IndexFunc(value, func(r rune) bool { return r < '!' || r > '~' }) >= 0 {
		return "", metadataInvalidArgument("idempotency_key must contain 1 to 200 printable ASCII characters without spaces", "idempotency_key", value)
	}
	return value, nil
}

func createIdempotencyConflict(key, todoID string) *application.Error {
	err := application.NewError(application.CodeConflict, "idempotency_key has already been used for a different todo creation payload")
	err.Details = map[string]any{"idempotency_key": key, "todo_id": todoID}
	return err
}

func todoETagConflict(todo Todo, expected string) *application.Error {
	err := application.NewError(application.CodeConflict, "todo changed since it was loaded; reload it before saving")
	err.Details = map[string]any{
		"todo_id": todo.ID, "expected_etag": expected, "current_etag": TodoETag(todo),
	}
	return err
}
