package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/session"
)

// Session reads are four explicit desktop read models. They share the Session
// application service with the public CLI, while keeping CLI argv and rendering
// concerns outside the desktop protocol.
func registerSession(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "session.list", func(
		ctx context.Context,
		_ application.Call,
		input session.ListInput,
	) (session.ListResult, error) {
		return dependencies.Session.List(ctx, input)
	})
	bind(registry, "session.search", func(
		ctx context.Context,
		_ application.Call,
		input session.SearchInput,
	) (session.SearchResult, error) {
		return dependencies.Session.Search(ctx, input)
	})
	bind(registry, "session.show", func(
		ctx context.Context,
		_ application.Call,
		input session.ShowInput,
	) (session.ShowResult, error) {
		return dependencies.Session.Show(ctx, input)
	})
	bind(registry, "session.timeline", func(
		ctx context.Context,
		_ application.Call,
		input session.TimelineInput,
	) (session.TimelineResult, error) {
		return dependencies.Session.Timeline(ctx, input)
	})
}
