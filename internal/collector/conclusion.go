package collector

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
)

// saveConclusion explicitly promotes one collected insight's conclusion into
// central knowledge. Classification only writes CollectionItem.Summary; this is
// the user-controlled boundary that creates a knowledge document.
func (service Service) saveConclusion(itemID, collection string) (store.CollectionItem, error) {
	db, err := store.Open()
	if err != nil {
		return store.CollectionItem{}, err
	}
	defer db.Close()
	item, err := getItemForUseCase(db, strings.TrimSpace(itemID))
	if err != nil {
		return store.CollectionItem{}, err
	}
	if item.Action != "insight" || item.Status != "processed" {
		return store.CollectionItem{}, itemConflict(fmt.Sprintf("collection item %s has no saveable conclusion", item.ID), item.ID)
	}
	if strings.TrimSpace(item.Summary) == "" {
		return store.CollectionItem{}, itemConflict(fmt.Sprintf("collection item %s conclusion is empty", item.ID), item.ID)
	}
	// Repeated clicks are idempotent. If the document was moved, refresh the
	// stored collection so the browser still opens the right library. If it was
	// deleted, recreate it: the conclusion remains the durable source here.
	if item.KnowledgeDocumentID != "" {
		if document, getErr := knowledge.Get(config.AtmDir, item.KnowledgeDocumentID); getErr == nil {
			if item.KnowledgeCollection != document.Collection {
				item.KnowledgeCollection = document.Collection
				if err := store.UpdateCollectionItem(db, item); err != nil {
					return store.CollectionItem{}, err
				}
			}
			return item, nil
		} else if !strings.Contains(getErr.Error(), "not found") {
			return store.CollectionItem{}, getErr
		}
	}
	source, err := store.GetCollectionSource(db, item.SourceID)
	if err != nil && err != sql.ErrNoRows {
		return store.CollectionItem{}, err
	}
	destination := strings.TrimSpace(collection)
	if destination == "" {
		destination = digestCollection(source)
	}
	title := strings.TrimSpace(item.Title)
	sourceName := conclusionSourceName(item, source)
	if title == "" {
		title = sourceName + " 收集结论"
	}
	document, err := knowledge.Add(config.AtmDir, knowledge.AddDocumentInput{
		Title: title, Content: conclusionKnowledgeBody(item, source), Collection: destination,
		Tags: []string{"收集结论", sourceName}, Projects: digestProjects(source),
		Producer: "atm-collector",
	})
	if err != nil {
		return store.CollectionItem{}, err
	}
	item.KnowledgeDocumentID = document.Metadata.ID
	item.KnowledgeCollection = document.Collection
	if err := store.UpdateCollectionItem(db, item); err != nil {
		return store.CollectionItem{}, err
	}
	return store.GetCollectionItem(db, item.ID)
}

func conclusionKnowledgeBody(item store.CollectionItem, source store.CollectionSource) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(item.Summary))
	builder.WriteString("\n\n---\n\n## 来源\n\n")
	builder.WriteString(conclusionSourceName(item, source))
	if item.OccurredAt > 0 {
		builder.WriteString(" · ")
		builder.WriteString(time.Unix(item.OccurredAt, 0).In(config.Loc).Format("2006-01-02 15:04"))
	}
	if sender := strings.TrimSpace(item.Sender); sender != "" {
		builder.WriteString(" · ")
		builder.WriteString(sender)
	}
	builder.WriteString("\n\n收集记录：`")
	builder.WriteString(item.ID)
	builder.WriteString("`")
	return builder.String()
}

func conclusionSourceName(item store.CollectionItem, source store.CollectionSource) string {
	if name := strings.TrimSpace(sourceDisplayName(source)); name != "" {
		return name
	}
	return item.SourceID
}
