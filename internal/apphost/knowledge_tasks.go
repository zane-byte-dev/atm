package apphost

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

type TodoKnowledgeInput struct {
	TodoID     string `json:"todo_id"`
	DocumentID string `json:"document_id"`
}

type KnowledgeTasksInput struct {
	DocumentID string `json:"document_id"`
	Offset     int    `json:"offset,omitempty"`
}

type KnowledgeTasksResult struct {
	Items   []store.LinkedKnowledgeTask `json:"items"`
	HasMore bool                        `json:"has_more"`
}

func (h *Host) knowledgeForTask(ctx context.Context, id string) (knowledge.Document, error) {
	if err := knowledgeIdentity(id); err != nil {
		return knowledge.Document{}, err
	}
	if err := checkKnowledgeTree(ctx); err != nil {
		return knowledge.Document{}, err
	}
	return knowledge.NewService(knowledge.ServiceOptions{DataDir: config.AtmDir}).Get(ctx, knowledge.GetInput{DocumentID: id})
}

func (h *Host) LinkTodoKnowledge(ctx context.Context, call application.Call, input TodoKnowledgeInput, remove bool) (struct{}, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateWrite(ctx, call); err != nil {
		return struct{}{}, err
	}
	if err := validateTodo(ctx, call, input.TodoID); err != nil {
		return struct{}{}, err
	}
	if err := knowledgeIdentity(input.DocumentID); err != nil {
		return struct{}{}, err
	}
	if remove {
		// A missing source document must not prevent detaching a stale link.
		_, err := h.work.RemoveLink(ctx, call, work.RemoveLinkInput{TodoID: input.TodoID, URL: work.KnowledgeLinkURL(input.DocumentID)})
		return struct{}{}, err
	}
	document, err := h.knowledgeForTask(ctx, input.DocumentID)
	if err != nil {
		return struct{}{}, err
	}
	_, err = h.work.AddLink(ctx, call, work.AddLinkInput{TodoID: input.TodoID, URL: work.KnowledgeLinkURL(input.DocumentID), Kind: "knowledge", Title: document.Metadata.Title})
	return struct{}{}, err
}

func (h *Host) KnowledgeTasks(ctx context.Context, call application.Call, input KnowledgeTasksInput) (KnowledgeTasksResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validate(ctx, call); err != nil {
		return KnowledgeTasksResult{}, err
	}
	if err := knowledgeIdentity(input.DocumentID); err != nil {
		return KnowledgeTasksResult{}, err
	}
	if input.Offset < 0 {
		return KnowledgeTasksResult{}, invalid("offset must be non-negative")
	}
	items, err := h.work.KnowledgeTasks(ctx, call, input.DocumentID, input.Offset, 51)
	if err != nil {
		return KnowledgeTasksResult{}, err
	}
	result := KnowledgeTasksResult{Items: items, HasMore: len(items) > 50}
	if result.HasMore {
		result.Items = items[:50]
	}
	return result, nil
}
