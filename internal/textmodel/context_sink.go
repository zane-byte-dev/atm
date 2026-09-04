package textmodel

import "context"

type sinkContextKey struct{}

// WithSink records calls for one operation, overriding the CLI process sink.
// Concurrent server jobs can flush independently without either stealing each
// other's calls or buffering until a long-running process exits.
func WithSink(ctx context.Context, sink func(Call)) context.Context {
	return context.WithValue(ctx, sinkContextKey{}, sink)
}

func sinkFor(ctx context.Context) func(Call) {
	if ctx != nil {
		if sink, ok := ctx.Value(sinkContextKey{}).(func(Call)); ok {
			return sink
		}
	}
	return Sink
}
