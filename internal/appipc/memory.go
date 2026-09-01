package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/knowledge"
)

// Memory is part of the Knowledge application boundary, but has its own
// desktop vocabulary: recall returns the effective fact set and supersede
// appends one complete replacement event. Neither method mirrors CLI flags.
func registerMemory(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "memory.recall", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.RecallMemoryInput,
	) (knowledge.RecallMemoryResult, error) {
		return dependencies.Knowledge.RecallMemory(ctx, input)
	})
	bind(registry, "memory.supersede", func(
		ctx context.Context,
		_ application.Call,
		input knowledge.SupersedeMemoryInput,
	) (knowledge.SupersedeMemoryResult, error) {
		return dependencies.Knowledge.SupersedeMemory(ctx, input)
	})
}
