package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/config"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Wire AI agents into the ATM notch",
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var agentHookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Forward one agent hook event to the ATM notch",
	Long: `Reads a hook payload on stdin and forwards it to the running ATM app.

Meant to be invoked by an agent's hook system, not by hand. It never writes to
stdout and always exits 0, so installing it cannot change how the agent behaves:
if the app is not running the event is simply dropped.

  atm agent hook --source claude < payload.json`,
	// This one both forwards an event and parents install/status/uninstall, so a
	// mistyped subcommand has to be rejected rather than forwarded as an event.
	Args: noSubcommandArgs,
	RunE: runAgentHook,
}

var (
	agentHookSource  string
	agentHookReason  string
	agentHookVerbose bool
)

func init() {
	agentHookCmd.Flags().StringVar(
		&agentHookSource, "source", "",
		"agent that produced the event: "+strings.Join(agentevent.SupportedSources(), ", "),
	)
	agentHookCmd.Flags().StringVar(
		&agentHookReason, "reason", "",
		"notification matcher that fired, e.g. permission_prompt (Claude Code does not repeat it in the payload)",
	)
	agentHookCmd.Flags().BoolVar(
		&agentHookVerbose, "verbose", false,
		"report what happened on stderr; stdout stays empty either way",
	)
	for _, command := range []*cobra.Command{
		agentHookInstallCmd, agentHookUninstallCmd, agentHookStatusCmd,
	} {
		command.Flags().StringVar(
			&agentHookSource, "source", "",
			"agent to wire up: "+strings.Join(agentevent.SupportedSources(), ", ")+" (default: all)",
		)
		agentHookCmd.AddCommand(command)
	}
	agentCmd.AddCommand(agentHookCmd)
	rootCmd.AddCommand(agentCmd)
}

var agentHookInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register ATM's notch hooks with an agent",
	Long: `Adds ATM's hooks to the agent's own config, leaving every other entry alone.

Only reporting hooks are installed: none of them can block a tool call or change
a permission decision, so the agent behaves exactly as before.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runHookConfig(cmd, hookConfigInstall)
	},
}

var agentHookUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ATM's notch hooks from an agent",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runHookConfig(cmd, hookConfigUninstall)
	},
}

var agentHookStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which notch hooks are registered",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runHookConfig(cmd, hookConfigStatus)
	},
}

type hookConfigMode int

const (
	hookConfigInstall hookConfigMode = iota
	hookConfigUninstall
	hookConfigStatus
)

// hookConfigSourceReport is the per-agent result, shaped for --json so the app's
// settings pane can render installation state without scraping text.
type hookConfigSourceReport struct {
	Source    string   `json:"source"`
	Path      string   `json:"path,omitempty"`
	Installed []string `json:"installed,omitempty"`
	Missing   []string `json:"missing,omitempty"`
	Added     []string `json:"added,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
	Manual    string   `json:"manual,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type hookConfigReport struct {
	SocketPath string                   `json:"socket_path"`
	Sources    []hookConfigSourceReport `json:"sources"`
}

func runHookConfig(cmd *cobra.Command, mode hookConfigMode) error {
	executable, err := atmExecutablePath()
	if err != nil {
		return fmt.Errorf("cannot resolve the atm binary path: %w", err)
	}
	sources := agentevent.SupportedSources()
	if agentHookSource != "" {
		sources = []string{agentHookSource}
	}

	report := hookConfigReport{SocketPath: agentevent.SocketPath()}
	for _, source := range sources {
		report.Sources = append(
			report.Sources,
			hookConfigForSource(mode, source, config.Home, executable),
		)
	}

	if jsonOutput {
		// Encoded to the command's writer rather than through output.JSON, which
		// prints straight to os.Stdout: the app reads this to render hook state,
		// so it needs to be exercisable in tests.
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}
	printHookConfigReport(cmd.OutOrStdout(), mode, report)
	return nil
}

func hookConfigForSource(
	mode hookConfigMode,
	source, home, executable string,
) hookConfigSourceReport {
	entry := hookConfigSourceReport{Source: source}

	// Pi has no hooks config file: it loads a TypeScript extension instead, so
	// point at that rather than reporting a phantom failure.
	if source == agentevent.SourcePi {
		entry.Manual = "Pi 走扩展文件：把 integrations/atm-notch.ts 复制到 ~/.pi/agent/extensions/"
		return entry
	}
	if _, err := agentevent.ConfigPath(source, home); err != nil {
		entry.Error = err.Error()
		return entry
	}

	var result agentevent.InstallResult
	var err error
	switch mode {
	case hookConfigInstall:
		result, err = agentevent.Install(source, home, executable)
	case hookConfigUninstall:
		result, err = agentevent.Uninstall(source, home, executable)
	case hookConfigStatus:
		result, err = agentevent.Status(source, home, executable)
	}
	if err != nil {
		entry.Error = err.Error()
		return entry
	}

	entry.Path = result.Path
	entry.Installed = result.Kept
	entry.Conflicts = result.Conflicts
	switch mode {
	case hookConfigStatus:
		// Status borrows Added to mean "not registered yet".
		entry.Missing = result.Added
	default:
		entry.Added = result.Added
		entry.Removed = result.Removed
	}
	return entry
}

func printHookConfigReport(writer io.Writer, mode hookConfigMode, report hookConfigReport) {
	fmt.Fprintf(writer, "notch socket: %s\n", report.SocketPath)
	for _, entry := range report.Sources {
		fmt.Fprintf(writer, "\n%s\n", entry.Source)
		if entry.Manual != "" {
			fmt.Fprintf(writer, "  %s\n", entry.Manual)
			continue
		}
		if entry.Error != "" {
			fmt.Fprintf(writer, "  error: %s\n", entry.Error)
			continue
		}
		fmt.Fprintf(writer, "  config: %s\n", entry.Path)
		switch mode {
		case hookConfigStatus:
			fmt.Fprintf(writer, "  registered: %s\n", listOrDash(entry.Installed))
			fmt.Fprintf(writer, "  missing:    %s\n", listOrDash(entry.Missing))
		default:
			if len(entry.Added) > 0 {
				fmt.Fprintf(writer, "  added:   %s\n", strings.Join(entry.Added, ", "))
			}
			if len(entry.Removed) > 0 {
				fmt.Fprintf(writer, "  removed: %s\n", strings.Join(entry.Removed, ", "))
			}
			if len(entry.Installed) > 0 {
				fmt.Fprintf(writer, "  already: %s\n", strings.Join(entry.Installed, ", "))
			}
			if len(entry.Added) == 0 && len(entry.Removed) == 0 && len(entry.Installed) == 0 {
				fmt.Fprintf(writer, "  no change\n")
			}
		}
		for _, conflict := range entry.Conflicts {
			// Worth saying out loud: another tool already answers this event, so
			// in-notch approval would mean two prompts racing for one decision.
			fmt.Fprintf(writer, "  note: another tool owns %s\n", conflict)
		}
	}
}

func listOrDash(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return strings.Join(values, ", ")
}

// runAgentHook forwards one event and swallows every failure.
//
// This runs inside the agent's turn. A non-zero exit or stray stdout would, for
// several hook events, be read as a decision — Claude Code treats exit 2 on
// PreToolUse as "block this tool call". So an ATM problem must never become the
// agent's problem: everything is reported on stderr under --verbose and the
// command still exits 0.
// hookPayloadLimit bounds what one hook invocation will buffer.
//
// PostToolUse carries the tool's entire response, so stdin stopped being
// reliably small the moment that hook was installed. Nothing this command reads
// lives beyond the payload's top-level scalars, and an unbounded ReadAll on the
// agent's hot path is a memory hazard for no gain.
const hookPayloadLimit = 8 << 20

func runAgentHook(cmd *cobra.Command, _ []string) error {
	note := func(format string, args ...any) {
		if agentHookVerbose {
			fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", args...)
		}
	}

	if agentHookSource == "" {
		note("atm agent hook: --source is required")
		return nil
	}

	raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), hookPayloadLimit))
	if err != nil {
		note("atm agent hook: cannot read stdin: %v", err)
		return nil
	}
	if int64(len(raw)) == hookPayloadLimit {
		// Drain first so the agent's write never blocks on a full pipe, then
		// give up: the JSON is cut mid-structure and will not parse. The next
		// tool call reports the same thing a moment later.
		_, _ = io.Copy(io.Discard, cmd.InOrStdin())
		note("atm agent hook: payload exceeds %d bytes, dropped", hookPayloadLimit)
		return nil
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		note("atm agent hook: empty payload")
		return nil
	}

	envelope, ok, err := agentevent.FromHook(agentevent.Input{
		Source: agentHookSource,
		Reason: agentHookReason,
		Raw:    raw,
		Now:    time.Now(),
	})
	if err != nil {
		note("atm agent hook: %v", err)
		return nil
	}
	if !ok {
		note("atm agent hook: nothing to report for this event")
		return nil
	}
	if err := agentevent.Deliver(envelope); err != nil {
		note("atm agent hook: not delivered (%v)", err)
		return nil
	}
	note("atm agent hook: delivered %s for session %s", envelope.Event, envelope.SessionID)
	return nil
}

// agentHookCommand renders the command line an agent config should invoke.
// Shared by the installer and `status` so the string they compare is the string
// they wrote.
func agentHookCommand(executable, source, reason string) string {
	command := fmt.Sprintf("%s agent hook --source %s", quoteForHookConfig(executable), source)
	if reason != "" {
		command += " --reason " + reason
	}
	return command
}

func quoteForHookConfig(path string) string {
	if strings.ContainsAny(path, " \t\"'") {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}

// atmExecutablePath resolves the absolute path to this binary for baking into
// agent configs. Hooks run with an unpredictable PATH, so a bare "atm" is not
// good enough.
func atmExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := os.Readlink(path)
	if err != nil {
		return path, nil
	}
	if strings.HasPrefix(resolved, "/") {
		return resolved, nil
	}
	return path, nil
}
