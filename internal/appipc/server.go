// Package appipc composes the desktop application's typed IPC surface.
//
// The generic JSON transport lives in internal/ipc. This package owns which
// application use cases the desktop may call and wires each stable method to a
// domain service supplied by the executable's composition root.
package appipc

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/doctor"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/ipc"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/quota"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/session"
	"github.com/zane-byte-dev/atm/internal/textmodel"
	"github.com/zane-byte-dev/atm/internal/work"
)

type TextModelChecker func(context.Context, textmodel.ConnectionCheckInput) (textmodel.CheckResult, error)
type TodoTitleGenerator func(context.Context, string) (string, error)

// Dependencies are the application services behind the desktop bridge. The
// command package constructs these once, including its OS-facing Dashboard and
// Collector ports; appipc never reaches back into cmd or Cobra.
type Dependencies struct {
	Config    config.Service
	AgentHook agentevent.Service
	AIDay     aiday.Service
	Dashboard dashboard.Service
	Doctor    doctor.Service
	Guard     guard.Service
	Quota     quota.Service
	// QuotaLiveBilling mirrors the user's grok_live_quota setting. It is a
	// dependency rather than a request field: opting into a network billing call
	// is a configuration decision, not something a replayable transport carries.
	QuotaLiveBilling  bool
	Knowledge         knowledge.Service
	Session           session.Service
	Work              work.Service
	WorkEffects       work.EffectExecutor
	Collector         collector.Service
	CheckTextModel    TextModelChecker
	GenerateTodoTitle TodoTitleGenerator
}

// Server owns both the desktop method registry and the generic IPC transport.
// It deliberately exposes names and Serve, not the mutable registry itself.
type Server struct {
	registry  *ipc.Registry
	transport *ipc.Server
}

func New(dependencies Dependencies) *Server {
	if dependencies.CheckTextModel == nil {
		dependencies.CheckTextModel = func(
			ctx context.Context,
			input textmodel.ConnectionCheckInput,
		) (textmodel.CheckResult, error) {
			return textmodel.CheckConnection(ctx, 45*time.Second, input)
		}
	}
	if dependencies.GenerateTodoTitle == nil {
		dependencies.GenerateTodoTitle = refine.GenerateTitle
	}
	registry := ipc.NewRegistry()
	registerConfig(registry, dependencies)
	registerAgentHook(registry, dependencies)
	registerAIDay(registry, dependencies)
	registerDashboard(registry, dependencies)
	registerDoctor(registry, dependencies)
	registerGuard(registry, dependencies)
	registerKnowledge(registry, dependencies)
	registerMemory(registry, dependencies)
	registerQuota(registry, dependencies)
	registerSession(registry, dependencies)
	registerTodo(registry, dependencies)
	registerCollector(registry, dependencies)
	return &Server{
		registry:  registry,
		transport: ipc.NewServer(contract.IPCProtocolVersion, registry),
	}
}

func (server *Server) Serve(
	ctx context.Context,
	verb string,
	input io.Reader,
	output io.Writer,
) error {
	if server == nil || server.transport == nil {
		return fmt.Errorf("app ipc server is nil")
	}
	return server.transport.Serve(ctx, verb, input, output)
}

func (server *Server) Names() []string {
	if server == nil || server.registry == nil {
		return nil
	}
	return server.registry.Names()
}

func bind[Request any, Response any](
	registry *ipc.Registry,
	name string,
	handler func(context.Context, application.Call, Request) (Response, error),
) {
	ipc.MustBind(registry, name, handler)
}

func bindNoRequest[Response any](
	registry *ipc.Registry,
	name string,
	handler func(context.Context, application.Call) (Response, error),
) {
	ipc.MustBindNoRequest(registry, name, handler)
}
