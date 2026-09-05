package agentevent

import (
	"context"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
)

// RegistrationAction says which use case produced a report. It travels in the
// payload because the same three lists mean different things per action:
// `installed` is a finding under status and an outcome under install, and a
// reader that has to remember which verb it asked for will eventually be wrong.
type RegistrationAction string

const (
	ActionInstall   RegistrationAction = "install"
	ActionUninstall RegistrationAction = "uninstall"
	ActionStatus    RegistrationAction = "status"
)

// ServiceOptions are the hook plane's process and filesystem ports. Home and
// the socket path are resolved per call rather than captured at construction:
// config.Home is loaded from ~/.atm/config.json after package initialization,
// so a service built at init time would otherwise hold the pre-config value.
type ServiceOptions struct {
	Home       func() string
	SocketPath func() string
	Executable func() (string, error)
	Sources    func() []string
}

// Service owns hook registration: which agents are in scope, what binary path
// gets baked into their configs, and how a per-agent failure is reported.
// Adapters render the returned report.
type Service struct {
	home       func() string
	socketPath func() string
	executable func() (string, error)
	sources    func() []string
}

// Default is shared by the CLI and browser workspace host.
var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.Home == nil {
		options.Home = func() string { return config.Home }
	}
	if options.SocketPath == nil {
		options.SocketPath = SocketPath
	}
	if options.Executable == nil {
		options.Executable = config.ExecutablePath
	}
	if options.Sources == nil {
		options.Sources = SupportedSources
	}
	return Service{
		home: options.Home, socketPath: options.SocketPath,
		executable: options.Executable, sources: options.Sources,
	}
}

// RegistrationInput narrows the work to one agent. Empty means every agent ATM
// knows how to wire up, which is what both the CLI default and the browser
// workspace ask for.
type RegistrationInput struct {
	Source string `json:"source,omitempty"`
}

// Registration is one agent's outcome. Which of the list fields are populated
// depends on the action: status reports Installed and Missing, while install and
// uninstall report what they Added and Removed.
type Registration struct {
	Source    string   `json:"source"`
	Path      string   `json:"path,omitempty"`
	Installed []string `json:"installed,omitempty"`
	Missing   []string `json:"missing,omitempty"`
	Added     []string `json:"added,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	// Manual is set for agents that load an extension instead of reading a hooks
	// config file, where there is nothing for ATM to write.
	Manual string `json:"manual,omitempty"`
	// Error is this agent's own failure, not the call's. See register.
	Error string `json:"error,omitempty"`
}

type RegistrationReport struct {
	Action     RegistrationAction `json:"action"`
	SocketPath string             `json:"socket_path"`
	Sources    []Registration     `json:"sources"`
}

// Install registers ATM's reporting hooks with the selected agents.
//
// Deliberately not gated to a human at the CLI edge the way Guard installation
// is: wiring live activity up is exactly what the browser workspace asks for,
// and every hook installed here is a reporting hook that cannot block a tool
// call or change a permission decision. There is nothing to escalate to.
func (service Service) Install(
	ctx context.Context, call application.Call, input RegistrationInput,
) (RegistrationReport, error) {
	return service.register(ctx, call, input, ActionInstall)
}

func (service Service) Uninstall(
	ctx context.Context, call application.Call, input RegistrationInput,
) (RegistrationReport, error) {
	return service.register(ctx, call, input, ActionUninstall)
}

func (service Service) Status(
	ctx context.Context, call application.Call, input RegistrationInput,
) (RegistrationReport, error) {
	return service.register(ctx, call, input, ActionStatus)
}

// register walks the selected agents and keeps going after one of them fails.
//
// A per-agent failure is a finding, not the call's failure: an unparseable
// ~/.claude/settings.json must not hide that Codex and Grok were wired up fine.
// The report carries that error per source, and the settings pane shows it
// beside the agent it belongs to. Only a failure that makes the whole answer
// meaningless — an unknown agent, or not knowing our own binary path — is
// returned as an error.
func (service Service) register(
	ctx context.Context,
	call application.Call,
	input RegistrationInput,
	action RegistrationAction,
) (RegistrationReport, error) {
	if ctx == nil {
		return RegistrationReport{}, hookInvalidArgument("hook registration context is required", "context", nil)
	}
	if err := call.Validate(); err != nil {
		return RegistrationReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return RegistrationReport{}, hookUnavailable(string(action)+" agent hooks", err)
	}
	sources, err := service.selectedSources(input.Source)
	if err != nil {
		return RegistrationReport{}, err
	}
	executable, err := service.executable()
	if err != nil {
		return RegistrationReport{}, hookUnavailable("resolve the atm binary path", err)
	}

	home := service.home()
	report := RegistrationReport{
		Action: action, SocketPath: service.socketPath(),
		Sources: make([]Registration, 0, len(sources)),
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return RegistrationReport{}, hookUnavailable(string(action)+" agent hooks", err)
		}
		report.Sources = append(report.Sources, registerSource(action, source, home, executable))
	}
	return report, nil
}

// selectedSources rejects an agent ATM has no hook knowledge of instead of
// reporting it as a per-source error. A typo in `--source` is not a finding
// about that agent; nothing was inspected at all.
func (service Service) selectedSources(source string) ([]string, error) {
	supported := service.sources()
	source = strings.TrimSpace(source)
	if source == "" {
		return supported, nil
	}
	for _, candidate := range supported {
		if candidate == source {
			return []string{source}, nil
		}
	}
	return nil, hookInvalidArgument(fmt.Sprintf(
		"unknown agent %q: use one of %s", source, strings.Join(supported, ", "),
	), "source", source)
}

func registerSource(action RegistrationAction, source, home, executable string) Registration {
	entry := Registration{Source: source}

	// Pi has no hooks config file: it loads a TypeScript extension instead, so
	// point at that rather than reporting a phantom failure.
	if source == SourcePi {
		entry.Manual = "Pi 走扩展文件：把 integrations/atm-notch.ts 复制到 ~/.pi/agent/extensions/"
		return entry
	}
	if _, err := ConfigPath(source, home); err != nil {
		entry.Error = err.Error()
		return entry
	}

	var (
		result InstallResult
		err    error
	)
	switch action {
	case ActionInstall:
		result, err = Install(source, home, executable)
	case ActionUninstall:
		result, err = Uninstall(source, home, executable)
	default:
		result, err = Status(source, home, executable)
	}
	if err != nil {
		entry.Error = err.Error()
		return entry
	}

	entry.Path = result.Path
	entry.Installed = result.Kept
	if action == ActionStatus {
		// Status borrows Added to mean "not registered yet".
		entry.Missing = result.Added
		return entry
	}
	entry.Added = result.Added
	entry.Removed = result.Removed
	return entry
}

func hookInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func hookUnavailable(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action, cause)
	err.Retryable = true
	return err
}
