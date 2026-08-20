package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/zane-byte-dev/atm/internal/application"
)

type handler func(context.Context, application.Call, io.Reader) (any, error)

// Registry is the typed method table behind `atm _ipc`. Registration happens
// during command construction; dispatch may happen concurrently in tests or in
// future in-process embedders, so the table protects both paths.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]handler{}}
}

// Bind registers a method with typed request and response values. Only this
// adapter sees JSON: the service function receives a fully decoded request and
// returns a normal Go value.
func Bind[Request any, Response any](
	registry *Registry,
	name string,
	fn func(context.Context, application.Call, Request) (Response, error),
) error {
	if fn == nil {
		return fmt.Errorf("ipc method %q has no handler", name)
	}
	return registry.bind(name, func(ctx context.Context, call application.Call, input io.Reader) (any, error) {
		var request Request
		if err := decodeRequest(input, &request); err != nil {
			return nil, InvalidArgument(err)
		}
		return fn(ctx, call, request)
	})
}

// BindNoRequest registers a typed response for a method with no parameters.
// Its handler deliberately never reads stdin, so pasting a read-only IPC call
// into an interactive terminal cannot block waiting for EOF.
func BindNoRequest[Response any](
	registry *Registry,
	name string,
	fn func(context.Context, application.Call) (Response, error),
) error {
	if fn == nil {
		return fmt.Errorf("ipc method %q has no handler", name)
	}
	return registry.bind(name, func(ctx context.Context, call application.Call, _ io.Reader) (any, error) {
		return fn(ctx, call)
	})
}

func MustBind[Request any, Response any](
	registry *Registry,
	name string,
	fn func(context.Context, application.Call, Request) (Response, error),
) {
	if err := Bind(registry, name, fn); err != nil {
		panic(err)
	}
}

func MustBindNoRequest[Response any](
	registry *Registry,
	name string,
	fn func(context.Context, application.Call) (Response, error),
) {
	if err := BindNoRequest(registry, name, fn); err != nil {
		panic(err)
	}
}

func (registry *Registry) bind(name string, fn handler) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("ipc method name is empty")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.handlers[name]; exists {
		return fmt.Errorf("ipc method %q is already registered", name)
	}
	registry.handlers[name] = fn
	return nil
}

func (registry *Registry) dispatch(
	ctx context.Context,
	call application.Call,
	name string,
	input io.Reader,
) (any, error) {
	registry.mu.RLock()
	fn, ok := registry.handlers[name]
	registry.mu.RUnlock()
	if !ok {
		return nil, methodNotFound(name, registry.Names())
	}
	return fn(ctx, call, input)
}

func (registry *Registry) Names() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.handlers))
	for name := range registry.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func decodeRequest(input io.Reader, target any) error {
	if input == nil {
		return fmt.Errorf("this verb reads its parameters as JSON on stdin, and stdin was empty")
	}
	raw, err := io.ReadAll(input)
	if err != nil {
		return fmt.Errorf("reading ipc parameters: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("this verb reads its parameters as JSON on stdin, and stdin was empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decoding ipc parameters: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decoding ipc parameters: more than one JSON value")
		}
		return fmt.Errorf("decoding ipc parameters: trailing data: %w", err)
	}
	return nil
}

func joinNames(names []string) string { return strings.Join(names, ", ") }
