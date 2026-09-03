package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCollectionWebHumanStateActionsAndAgentDenial(t *testing.T) {
	withCollectorStore(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{Connector: "test", Kind: "group", ExternalID: "web-group", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := store.PutCollectionItem(db, store.CollectionItem{SourceID: source.ID, Connector: source.Connector,
		Fingerprint: "web-result", MessageIDs: []string{"message"}, Action: "insight", Status: "processed"})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	service := Service{}
	ctx := context.Background()
	for _, kind := range []application.ActorKind{application.ActorHuman, application.ActorAgent} {
		call := sourceApplicationCall(kind, application.OriginWeb)
		operations := []func() error{
			func() error {
				_, err := service.SetItemsRead(ctx, call, SetItemsReadInput{ItemIDs: []string{item.ID}, Read: true})
				return err
			},
			func() error {
				_, err := service.SetItemsArchived(ctx, call, SetItemsArchivedInput{ItemIDs: []string{item.ID}, Archived: true})
				return err
			},
			func() error {
				_, err := service.SetSourceEnabled(ctx, call, SetSourceEnabledInput{SourceID: source.ID, Enabled: false})
				return err
			},
			func() error {
				_, err := service.SetSourceMuted(ctx, call, SetSourceMutedInput{SourceID: source.ID, Muted: true})
				return err
			},
		}
		for i, operation := range operations {
			err := operation()
			if kind == application.ActorHuman && err != nil {
				t.Errorf("human Web action %d denied: %v", i, err)
			}
			if kind == application.ActorAgent && !errors.Is(err, application.ErrForbidden) {
				t.Errorf("agent Web action %d accepted: %v", i, err)
			}
		}
	}
}
