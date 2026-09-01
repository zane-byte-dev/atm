package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestItemUseCasesValidateCallAndTypedInputAtBoundary(t *testing.T) {
	service := Service{}

	_, err := service.Reprocess(context.Background(), application.Call{}, ReprocessInput{ItemID: "ci_1"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid call error = %v, want invalid_argument", err)
	}

	_, err = service.Reprocess(context.Background(), itemTestCall(), ReprocessInput{ItemID: "  "})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty item ID error = %v, want invalid_argument", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Details["field"] != "item_id" {
		t.Fatalf("empty item ID details = %+v", appErr)
	}

	invalidPriority := "urgent"
	_, err = service.Promote(context.Background(), itemTestCall(), PromoteInput{
		ItemID: "ci_1", Correction: ItemCorrection{Priority: &invalidPriority},
	})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid promote correction error = %v, want invalid_argument", err)
	}

	_, err = service.Revert(context.Background(), itemTestCall(), RevertInput{ItemID: "ci_1"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed revert error = %v, want invalid_argument", err)
	}
}

func TestItemUseCasesReturnStableNotFoundAndConflictErrors(t *testing.T) {
	withCollectorStore(t)
	service := Service{Extractor: &fakeExtractor{}}

	_, err := service.Reprocess(context.Background(), itemTestCall(), ReprocessInput{ItemID: "missing"})
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing item error = %v, want not_found", err)
	}

	source := addCollectorSource(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.PutCollectionItem(db, store.CollectionItem{
		SourceID: source.ID, Connector: source.Connector, Fingerprint: "typed-conflict",
		MessageIDs: []string{"m1"}, Action: "ignore", Status: "processed",
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Revert(context.Background(), itemTestCall(), RevertInput{ItemID: item.ID, Confirmed: true})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("non-reversible item error = %v, want conflict", err)
	}
	_, err = service.Correct(context.Background(), itemTestCall(), CorrectInput{ItemID: item.ID})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty correction error = %v, want invalid_argument", err)
	}
}
