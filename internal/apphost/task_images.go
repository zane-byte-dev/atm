package apphost

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/work"
)

type UploadImageInput struct {
	TodoID       string
	ExpectedETag string
	Name         string
	Data         []byte
}

func (h *Host) UploadTodoImage(ctx context.Context, call application.Call, input UploadImageInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateExtension(ctx, call, input.TodoID, input.ExpectedETag); err != nil {
		return MutationResult{}, err
	}
	result, err := h.work.AddImage(ctx, call, work.AddImageInput{
		TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, Name: input.Name, Data: input.Data,
	})
	if err != nil {
		return MutationResult{}, err
	}
	return mutationView(result.Todo), nil
}
