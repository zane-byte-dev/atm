package collector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// ReprocessInput identifies an audit item whose classification should be run
// again. The item ID is part of the typed use-case contract rather than an
// implicit positional argument owned by Cobra.
type ReprocessInput struct {
	ItemID string `json:"item_id"`
}

type PromoteInput struct {
	ItemID     string         `json:"item_id"`
	Correction ItemCorrection `json:"correction,omitempty"`
}

type CorrectInput struct {
	ItemID     string         `json:"item_id"`
	Correction ItemCorrection `json:"correction"`
}

type RevertInput struct {
	ItemID    string `json:"item_id"`
	Confirmed bool   `json:"confirmed"`
}

type SaveConclusionInput struct {
	ItemID     string `json:"item_id"`
	Collection string `json:"collection,omitempty"`
}

// ItemResult is shared by CLI and future typed IPC adapters. Keeping the
// result explicit leaves room for after-commit events without changing the
// persisted CollectionItem shape or the CLI's existing JSON output.
type ItemResult struct {
	Item store.CollectionItem `json:"item"`
}

func (service Service) Reprocess(
	ctx context.Context,
	call application.Call,
	input ReprocessInput,
) (ItemResult, error) {
	ctx, itemID, err := validateItemCall(ctx, call, input.ItemID)
	if err != nil {
		return ItemResult{}, err
	}
	item, err := service.reprocessItem(ctx, itemID)
	if err != nil {
		return ItemResult{}, itemApplicationError("reprocess", itemID, err)
	}
	return ItemResult{Item: item}, nil
}

func (service Service) Promote(
	ctx context.Context,
	call application.Call,
	input PromoteInput,
) (ItemResult, error) {
	_, itemID, err := validateItemCall(ctx, call, input.ItemID)
	if err != nil {
		return ItemResult{}, err
	}
	if err := validateItemCorrection(input.Correction, false); err != nil {
		return ItemResult{}, err
	}
	item, err := service.promoteItem(itemID, input.Correction)
	if err != nil {
		return ItemResult{}, itemApplicationError("promote", itemID, err)
	}
	return ItemResult{Item: item}, nil
}

func (service Service) Correct(
	ctx context.Context,
	call application.Call,
	input CorrectInput,
) (ItemResult, error) {
	_, itemID, err := validateItemCall(ctx, call, input.ItemID)
	if err != nil {
		return ItemResult{}, err
	}
	if err := validateItemCorrection(input.Correction, true); err != nil {
		return ItemResult{}, err
	}
	item, err := service.correctItem(itemID, input.Correction)
	if err != nil {
		return ItemResult{}, itemApplicationError("correct", itemID, err)
	}
	return ItemResult{Item: item}, nil
}

func (service Service) Revert(
	ctx context.Context,
	call application.Call,
	input RevertInput,
) (ItemResult, error) {
	_, itemID, err := validateItemCall(ctx, call, input.ItemID)
	if err != nil {
		return ItemResult{}, err
	}
	if !input.Confirmed {
		return ItemResult{}, itemInvalidArgument(
			"reverting a collection item requires confirmation", "confirmed", false,
		)
	}
	item, err := service.revertItem(itemID)
	if err != nil {
		return ItemResult{}, itemApplicationError("revert", itemID, err)
	}
	return ItemResult{Item: item}, nil
}

func (service Service) SaveConclusion(
	ctx context.Context,
	call application.Call,
	input SaveConclusionInput,
) (ItemResult, error) {
	_, itemID, err := validateItemCall(ctx, call, input.ItemID)
	if err != nil {
		return ItemResult{}, err
	}
	item, err := service.saveConclusion(itemID, strings.TrimSpace(input.Collection))
	if err != nil {
		return ItemResult{}, itemApplicationError("save conclusion", itemID, err)
	}
	return ItemResult{Item: item}, nil
}

func validateItemCall(
	ctx context.Context,
	call application.Call,
	itemID string,
) (context.Context, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ctx, "", err
	}
	if err := call.Validate(); err != nil {
		return ctx, "", err
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		err := application.NewError(application.CodeInvalidArgument, "collection item ID is required")
		err.Details = map[string]any{"field": "item_id", "value": itemID}
		return ctx, "", err
	}
	return ctx, itemID, nil
}

func getItemForUseCase(db *sql.DB, itemID string) (store.CollectionItem, error) {
	item, err := store.GetCollectionItem(db, itemID)
	if !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	notFound := application.WrapError(
		application.CodeNotFound,
		fmt.Sprintf("collection item not found: %s", itemID),
		err,
	)
	notFound.Details = map[string]any{"item_id": itemID}
	return store.CollectionItem{}, notFound
}

func validateItemCorrection(correction ItemCorrection, requireAny bool) error {
	if correction.Title == nil && correction.Project == nil && correction.Priority == nil {
		if requireAny {
			return itemInvalidArgument("no collection item correction supplied", "correction", correction)
		}
		return nil
	}
	if correction.Title != nil && strings.TrimSpace(*correction.Title) == "" {
		return itemInvalidArgument("corrected title cannot be empty", "correction.title", *correction.Title)
	}
	if correction.Priority != nil {
		value := strings.ToUpper(strings.TrimSpace(*correction.Priority))
		if value != "P0" && value != "P1" && value != "P2" && value != "P3" {
			return itemInvalidArgument("invalid priority: "+value, "correction.priority", *correction.Priority)
		}
	}
	return nil
}

func itemApplicationError(operation, itemID string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	wrapped := application.WrapError(
		application.CodeUnavailable,
		fmt.Sprintf("%s collection item %s", operation, itemID),
		err,
	)
	wrapped.Details = map[string]any{"item_id": itemID, "operation": operation}
	wrapped.Retryable = true
	return wrapped
}

func itemInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func itemConflict(message, itemID string) *application.Error {
	err := application.NewError(application.CodeConflict, message)
	err.Details = map[string]any{"item_id": itemID}
	return err
}

func linkedTodoConflict(message, itemID, todoID string, cause error) *application.Error {
	err := application.WrapError(application.CodeConflict, message, cause)
	err.Details = map[string]any{"item_id": itemID, "todo_id": todoID}
	return err
}
