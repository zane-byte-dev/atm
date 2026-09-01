package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/knowledge"
)

func registerKnowledge(registry *ipc.Registry, dependencies Dependencies) {
	bindNoRequest(registry, "knowledge.catalog", func(
		ctx context.Context,
		_ application.Call,
	) ([]knowledge.CollectionInfo, error) {
		return dependencies.Knowledge.Catalog(ctx)
	})
	bind(registry, "knowledge.query", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.QueryInput,
	) (knowledge.QueryResult, error) {
		return dependencies.Knowledge.Query(ctx, input)
	})
	bind(registry, "knowledge.document.get", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.GetInput,
	) (knowledge.Document, error) {
		return dependencies.Knowledge.Get(ctx, input)
	})
	bind(registry, "knowledge.governance", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.GovernanceInput,
	) (knowledge.GovernanceResult, error) {
		return dependencies.Knowledge.Governance(ctx, input)
	})
	bind(registry, "knowledge.document.save", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.SaveDocumentInput,
	) (knowledge.Document, error) {
		return dependencies.Knowledge.SaveDocument(ctx, input)
	})
	bind(registry, "knowledge.document.delete", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.DeleteDocumentInput,
	) (knowledge.Document, error) {
		return dependencies.Knowledge.DeleteDocument(ctx, input)
	})
	bind(registry, "knowledge.document.import", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.ImportDocumentInput,
	) ([]knowledge.Document, error) {
		return dependencies.Knowledge.ImportDocument(ctx, input)
	})
	bind(registry, "knowledge.collection.save", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.SaveCollectionInput,
	) (knowledge.CollectionInfo, error) {
		return dependencies.Knowledge.SaveCollection(ctx, input)
	})
	bind(registry, "knowledge.collection.delete", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.DeleteCollectionInput,
	) (knowledge.DeleteCollectionResult, error) {
		return dependencies.Knowledge.DeleteCollection(ctx, input)
	})
	bind(registry, "knowledge.feedback", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.FeedbackInput,
	) (knowledge.FeedbackEvent, error) {
		return dependencies.Knowledge.Feedback(ctx, input)
	})
}
