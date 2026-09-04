package collector

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type SetItemsReadInput struct {
	ItemIDs []string `json:"item_ids,omitempty"`
	All     bool     `json:"all,omitempty"`
	Read    bool     `json:"read"`
}

type SetItemsReadResult struct {
	Items []store.CollectionItem `json:"items,omitempty"`
	Count int64                  `json:"count"`
	Read  bool                   `json:"read"`
}

type SetItemsArchivedInput struct {
	ItemIDs []string `json:"item_ids"`
	// All settles every record that has already had its attention and only needs
	// clearing away — read, unsaved conclusions. It is deliberately narrower than
	// the ID path rather than "archive everything visible": see
	// store.ArchiveSettledCollectionItems for why nothing that still owes an
	// action can be swept. Reopening stays per-ID, so All only pairs with
	// Archived.
	All      bool `json:"all,omitempty"`
	Archived bool `json:"archived"`
}

type SetItemsArchivedResult struct {
	Items    []store.CollectionItem `json:"items"`
	Count    int                    `json:"count"`
	Archived bool                   `json:"archived"`
}

type DeleteItemsInput struct {
	ItemIDs   []string `json:"item_ids"`
	Confirmed bool     `json:"confirmed"`
}

type DeletedItem struct {
	ID     string `json:"id"`
	TodoID string `json:"todo_id,omitempty"`
}

type DeleteItemsResult struct {
	Deleted []DeletedItem `json:"deleted"`
	Count   int           `json:"count"`
}

func (service Service) SetItemsRead(
	ctx context.Context,
	call application.Call,
	input SetItemsReadInput,
) (SetItemsReadResult, error) {
	if _, err := validateSourceCall(ctx, call, true); err != nil {
		return SetItemsReadResult{}, err
	}
	ids, err := validateItemIDs(input.ItemIDs)
	if err != nil {
		return SetItemsReadResult{}, err
	}
	if input.All {
		if len(ids) > 0 {
			return SetItemsReadResult{}, itemInvalidArgument(
				"read state accepts item IDs or all, not both", "item_ids", input.ItemIDs,
			)
		}
		if !input.Read {
			return SetItemsReadResult{}, itemInvalidArgument(
				"all collection items can only be marked read", "read", false,
			)
		}
	} else if len(ids) == 0 {
		return SetItemsReadResult{}, itemInvalidArgument(
			"at least one collection item ID is required", "item_ids", input.ItemIDs,
		)
	}
	db, err := store.Open()
	if err != nil {
		return SetItemsReadResult{}, itemStateError("change read state", err)
	}
	defer db.Close()
	if input.All {
		count, err := store.MarkAllCollectionItemsRead(db)
		if err != nil {
			return SetItemsReadResult{}, itemStateError("mark all collection items read", err)
		}
		return SetItemsReadResult{Items: []store.CollectionItem{}, Count: count, Read: true}, nil
	}
	items, err := store.SetCollectionItemsRead(db, ids, input.Read)
	if err != nil {
		return SetItemsReadResult{}, itemStateError("change collection item read state", err)
	}
	return SetItemsReadResult{Items: items, Count: int64(len(items)), Read: input.Read}, nil
}

func (service Service) SetItemsArchived(
	ctx context.Context,
	call application.Call,
	input SetItemsArchivedInput,
) (SetItemsArchivedResult, error) {
	if _, err := validateSourceCall(ctx, call, true); err != nil {
		return SetItemsArchivedResult{}, err
	}
	ids, err := validateItemIDs(input.ItemIDs)
	if err != nil {
		return SetItemsArchivedResult{}, err
	}
	if input.All {
		if len(ids) > 0 {
			return SetItemsArchivedResult{}, itemInvalidArgument(
				"archive state accepts item IDs or all, not both", "item_ids", input.ItemIDs,
			)
		}
		if !input.Archived {
			return SetItemsArchivedResult{}, itemInvalidArgument(
				"all collection items can only be settled, not reopened", "archived", false,
			)
		}
	} else if len(ids) == 0 {
		return SetItemsArchivedResult{}, itemInvalidArgument(
			"at least one collection item ID is required", "item_ids", input.ItemIDs,
		)
	}
	db, err := store.Open()
	if err != nil {
		return SetItemsArchivedResult{}, itemStateError("change collection item archive state", err)
	}
	defer db.Close()
	if input.All {
		count, err := store.ArchiveSettledCollectionItems(db)
		if err != nil {
			return SetItemsArchivedResult{}, itemStateError("settle read collection conclusions", err)
		}
		return SetItemsArchivedResult{
			Items: []store.CollectionItem{}, Count: int(count), Archived: true,
		}, nil
	}
	items, err := store.SetCollectionItemsArchived(db, ids, input.Archived)
	if err != nil {
		return SetItemsArchivedResult{}, itemStateError("change collection item archive state", err)
	}
	return SetItemsArchivedResult{Items: items, Count: len(items), Archived: input.Archived}, nil
}

// DeleteItems requires both trusted adapter provenance and explicit
// confirmation. Human@IPC is attribution rather than strong authentication:
// the replayable desktop bridge prevents accidental deletion but cannot defend
// against another local process running the same method.
func (service Service) DeleteItems(
	ctx context.Context,
	call application.Call,
	input DeleteItemsInput,
) (DeleteItemsResult, error) {
	if _, err := validateSourceCall(ctx, call, true); err != nil {
		return DeleteItemsResult{}, err
	}
	ids, err := validateItemIDs(input.ItemIDs)
	if err != nil {
		return DeleteItemsResult{}, err
	}
	if len(ids) == 0 {
		return DeleteItemsResult{}, itemInvalidArgument(
			"at least one collection item ID is required", "item_ids", input.ItemIDs,
		)
	}
	if !input.Confirmed {
		return DeleteItemsResult{}, itemInvalidArgument(
			"deleting collection items requires confirmation", "confirmed", false,
		)
	}
	lock, err := acquireCollectionLock(ctx)
	if err != nil {
		return DeleteItemsResult{}, itemStateError("delete collection items", err)
	}
	defer lock.Close()
	db, err := store.Open()
	if err != nil {
		return DeleteItemsResult{}, itemStateError("delete collection items", err)
	}
	defer db.Close()
	items, err := store.DeleteCollectionItems(db, ids)
	if err != nil {
		return DeleteItemsResult{}, itemStateError("delete collection items", err)
	}
	deleted := make([]DeletedItem, 0, len(items))
	for _, item := range items {
		deleted = append(deleted, DeletedItem{ID: item.ID, TodoID: item.TodoID})
	}
	return DeleteItemsResult{Deleted: deleted, Count: len(deleted)}, nil
}

func validateItemIDs(values []string) ([]string, error) {
	ids := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			return nil, itemInvalidArgument("collection item ID cannot be empty", "item_ids", values)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func itemStateError(operation string, cause error) error {
	var appErr *application.Error
	if errors.As(cause, &appErr) {
		return appErr
	}
	if errors.Is(cause, sql.ErrNoRows) || strings.Contains(cause.Error(), "not found") {
		message := "collection item not found"
		if strings.HasPrefix(cause.Error(), "collection item not found:") {
			message = cause.Error()
		}
		return application.WrapError(application.CodeNotFound, message, cause)
	}
	err := application.WrapError(application.CodeUnavailable, operation, cause)
	err.Retryable = true
	return err
}
