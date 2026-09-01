package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// RecallMemoryInput is the transport-independent intent for reading the
// effective shared-memory set. An empty query lists recent memories; an empty
// scope includes every scope.
type RecallMemoryInput struct {
	Query string `json:"query,omitempty"`
	Scope string `json:"scope,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type RecallMemoryResult struct {
	Hits []MemoryHit `json:"hits"`
}

// SupersedeMemoryInput carries the complete replacement fact. Source is the
// only caller-controlled provenance key: adapters do not get an unbounded
// metadata map that could grow into a transport-specific escape hatch.
type SupersedeMemoryInput struct {
	TargetID string   `json:"target_id"`
	Scope    string   `json:"scope"`
	Content  string   `json:"content"`
	Tags     []string `json:"tags,omitempty"`
	Source   string   `json:"source,omitempty"`
}

type SupersedeMemoryResult struct {
	Event MemoryEvent `json:"event"`
}

// RecallMemory validates the query boundary and returns ranked effective
// memories. Ranking remains domain logic; the service owns input defaults,
// context cancellation, and application-level error classification.
func (service Service) RecallMemory(ctx context.Context, input RecallMemoryInput) (RecallMemoryResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return RecallMemoryResult{}, err
	}
	input.Query = strings.TrimSpace(input.Query)
	input.Scope = strings.TrimSpace(input.Scope)
	if input.Scope != "" {
		if err := ValidateScope(input.Scope); err != nil {
			return RecallMemoryResult{}, invalidKnowledgeArgument(err.Error(), "scope", input.Scope)
		}
	}
	if input.Limit < 0 {
		return RecallMemoryResult{}, invalidKnowledgeArgument(
			"memory recall limit must not be negative", "limit", input.Limit,
		)
	}
	if input.Limit == 0 {
		input.Limit = 10
	}

	hits, err := recallMemoryHits(input.Query, input.Scope, input.Limit)
	if err != nil {
		return RecallMemoryResult{}, unavailableKnowledge("recall memory", err)
	}
	if err := knowledgeContextError(ctx); err != nil {
		return RecallMemoryResult{}, err
	}
	return RecallMemoryResult{Hits: hits}, nil
}

// SupersedeMemory appends one replacement event. The target check and scope
// rule live at this boundary; AppendMemoryEvent atomically persists the event,
// ordered tags, and source metadata. The unique target index makes concurrent
// replacements a single-winner operation.
func (service Service) SupersedeMemory(ctx context.Context, input SupersedeMemoryInput) (SupersedeMemoryResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return SupersedeMemoryResult{}, err
	}
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.Scope = strings.TrimSpace(input.Scope)
	input.Content = strings.TrimSpace(input.Content)
	input.Source = strings.TrimSpace(input.Source)
	if input.TargetID == "" {
		return SupersedeMemoryResult{}, invalidKnowledgeArgument(
			"memory target id must not be empty", "target_id", input.TargetID,
		)
	}
	if input.Content == "" {
		return SupersedeMemoryResult{}, invalidKnowledgeArgument(
			"memory content must not be empty", "content", input.Content,
		)
	}
	if err := ValidateScope(input.Scope); err != nil {
		return SupersedeMemoryResult{}, invalidKnowledgeArgument(err.Error(), "scope", input.Scope)
	}

	target, err := store.EffectiveMemory(input.TargetID)
	if err != nil {
		if strings.Contains(err.Error(), "active memory not found:") {
			return SupersedeMemoryResult{}, memoryNotFound(input.TargetID, err)
		}
		return SupersedeMemoryResult{}, unavailableKnowledge("read superseded memory", err)
	}
	if target.Scope != input.Scope {
		message := fmt.Sprintf("memory scope mismatch: target uses %s", target.Scope)
		appErr := application.NewError(application.CodeConflict, message)
		appErr.Details = map[string]any{
			"target_id":    input.TargetID,
			"target_scope": target.Scope,
			"scope":        input.Scope,
		}
		return SupersedeMemoryResult{}, appErr
	}
	if err := knowledgeContextError(ctx); err != nil {
		return SupersedeMemoryResult{}, err
	}

	metadata := map[string]string(nil)
	if input.Source != "" {
		metadata = map[string]string{"source": input.Source}
	}
	event := MemoryEvent{
		SchemaVersion: MemorySchemaVersion,
		ID:            newID("memory"),
		Op:            store.MemoryOpSupersede,
		Scope:         input.Scope,
		Content:       input.Content,
		TargetID:      input.TargetID,
		Tags:          normalizeValues(input.Tags),
		CreatedAt:     service.now().UTC(),
		Metadata:      metadata,
	}
	if err := store.AppendMemoryEvent(memoryRow(event)); err != nil {
		if store.IsMemoryTargetTaken(err) {
			appErr := application.WrapError(
				application.CodeConflict,
				fmt.Sprintf("memory is no longer active: %s", input.TargetID),
				err,
			)
			appErr.Details = map[string]any{"target_id": input.TargetID}
			return SupersedeMemoryResult{}, appErr
		}
		return SupersedeMemoryResult{}, unavailableKnowledge("supersede memory", err)
	}
	return SupersedeMemoryResult{Event: event}, nil
}

func memoryNotFound(targetID string, cause error) *application.Error {
	err := application.WrapError(
		application.CodeNotFound,
		fmt.Sprintf("active memory not found: %s", targetID),
		cause,
	)
	err.Details = map[string]any{"target_id": targetID}
	return err
}
