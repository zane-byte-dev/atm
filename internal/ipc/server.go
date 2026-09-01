package ipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

const EnvelopeVersion = 1

// Envelope is the single response shape for successful and failed calls. Data
// and Error are mutually exclusive. ProtocolVersion remains the app/CLI
// contract version understood by existing clients; EnvelopeVersion versions
// only this transport wrapper.
type Envelope struct {
	EnvelopeVersion int           `json:"envelope_version"`
	ProtocolVersion int           `json:"protocol_version"`
	RequestID       string        `json:"request_id"`
	Verb            string        `json:"verb"`
	Data            any           `json:"data,omitempty"`
	Error           *ErrorPayload `json:"error,omitempty"`
}

type Server struct {
	protocolVersion int
	registry        *Registry
	newRequestID    func() string
}

type ServerOption func(*Server)

// WithRequestIDGenerator is primarily a deterministic test seam. Production
// callers normally let Serve allocate one ID per process invocation.
func WithRequestIDGenerator(generator func() string) ServerOption {
	return func(server *Server) {
		if generator != nil {
			server.newRequestID = generator
		}
	}
}

func NewServer(protocolVersion int, registry *Registry, options ...ServerOption) *Server {
	server := &Server{
		protocolVersion: protocolVersion,
		registry:        registry,
		newRequestID:    NewRequestID,
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// Serve dispatches one method and always attempts to write an envelope. It
// still returns application errors so the process can exit non-zero and remain
// useful in shell scripts; an IPC-aware client decodes stdout before deciding
// how to present that status.
//
// Human@IPC records the product interaction represented by the desktop bridge;
// it is not authentication. `_ipc` is deliberately replayable in a terminal,
// so a service protecting human-only external effects must reject OriginIPC
// until that method has a separately verified local capability.
func (server *Server) Serve(ctx context.Context, verb string, input io.Reader, output io.Writer) error {
	if server == nil {
		return fmt.Errorf("ipc server is nil")
	}
	return server.ServeCall(ctx, application.Call{
		RequestID: server.newRequestID(),
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginIPC,
		},
	}, verb, input, output)
}

// ServeCall is the reusable dispatch path for an adapter that already has a
// trusted application identity. The desktop bridge uses Serve, which generates
// a request ID and fixes the actor to human@ipc instead of accepting either
// value from ordinary request parameters.
func (server *Server) ServeCall(
	ctx context.Context,
	call application.Call,
	verb string,
	input io.Reader,
	output io.Writer,
) error {
	if server == nil || server.registry == nil {
		return fmt.Errorf("ipc server has no registry")
	}
	// Cobra commands invoked through Execute have a context, while focused unit
	// tests and small embedders may call the bridge directly. Service and database
	// APIs require a non-nil context, so normalize at the transport edge.
	if ctx == nil {
		ctx = context.Background()
	}
	callErr := call.Validate()
	var data any
	if callErr == nil {
		data, callErr = server.registry.dispatch(ctx, call, verb, input)
	}
	envelope := Envelope{
		EnvelopeVersion: EnvelopeVersion,
		ProtocolVersion: server.protocolVersion,
		RequestID:       call.RequestID,
		Verb:            verb,
		Data:            data,
	}
	if callErr != nil {
		payload := faultPayload(callErr)
		envelope.Data = nil
		envelope.Error = &payload
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(envelope); err != nil {
		return fmt.Errorf("encoding ipc response: %w", err)
	}
	return callErr
}

var fallbackRequestID atomic.Uint64

func NewRequestID() string {
	var random [12]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "ipc-" + hex.EncodeToString(random[:])
	}
	// crypto/rand failure is extraordinarily rare, but losing the response is
	// worse than falling back to a process-local diagnostic identifier.
	return fmt.Sprintf("ipc-%x-%x", time.Now().UnixNano(), fallbackRequestID.Add(1))
}
