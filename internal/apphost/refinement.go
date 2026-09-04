package apphost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/work"
)

// RefineOptions supplies the same controlled document/outbox adapter as other
// Work mutations. The background service cannot choose or execute a shell
// command; only typed effects produced by Work reach this adapter.
func (h *Host) RefineOptions() background.TodoRefineOptions {
	options := background.TodoRefineOptions{Service: h.work}
	if h.effects != nil {
		options.Effects = refineEffects{host: h}
	}
	return options
}

type refineEffects struct{ host *Host }

func (effects refineEffects) ApplyWorkEffect(effect work.Effect) error {
	effects.host.effectsMu.Lock()
	defer effects.host.effectsMu.Unlock()
	return effects.host.effects.ApplyWorkEffect(effect)
}

// The Todo is already durably created when this runs. An enqueue failure is a
// warning on that success, never an HTTP failure inviting another creation.
// A retried create sees the original Todo snapshot and the same private job key.
func (h *Host) enqueueCreatedRefinement(ctx context.Context, call application.Call, key string, result *MutationResult) {
	if !config.TodoRefineOnAdd {
		return
	}
	jobs, _ := h.attachedRuntime()
	if jobs == nil {
		result.Warnings = append(result.Warnings, MutationWarning{Code: "refinement_unavailable", Message: "任务已创建；后台整理暂不可用，可稍后在任务详情中手动整理"})
		return
	}
	hash := sha256.Sum256([]byte(key))
	job, err := jobs.Run(ctx, call, background.Request{Kind: background.TodoRefine, TodoID: result.Todo.ID, ExpectedETag: result.ETag}, "todo-refine:"+hex.EncodeToString(hash[:]))
	if err != nil {
		result.Warnings = append(result.Warnings, MutationWarning{Code: "refinement_not_queued", Message: "任务已创建；自动整理未能排队，可稍后在任务详情中手动整理"})
		return
	}
	result.RefinementJob = &job
}
