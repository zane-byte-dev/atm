package cmd

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

var (
	agentFlag      string
	jsonOutput     bool
	syncBeforeRead bool
	sessionIDFlag  string
)

var rootCmd = &cobra.Command{
	Use:   "atm",
	Short: "ATM — AI Team Manager",
	Long: `ATM monitors and manages AI coding assistants in a unified view.

Supported agents: claude, codex, pi, copilot, qoder, qodercli, qoderwork, grokbuild, antigravity

Quick start:
  atm now                        Show current work by lifecycle status
  atm todo list                  List work items
  atm session status             Show what AI tools are currently doing
  atm session search <keyword>   Search all AI session history
  atm knowledge search <query>   Search the central knowledge base
  atm memory recall [query]      Recall shared memory
  atm collect status             Show connector health and recent decisions
  atm guard list                 List outbound actions waiting for a decision
  atm stats [--days N]           Show token usage and cost
  atm report [date]              Generate daily report`,
	SilenceErrors: true,
	SilenceUsage:  true,
	// Cobra validates command names, flags and positional arguments before this
	// hook. Telemetry uses the marker to distinguish invocation-contract errors
	// from failures returned by the command body; it changes no command state.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cliCommandEnteredRun.Store(true)
	},
}

func SetVersion(v string) {
	rootCmd.Version = v
}

func Execute() {
	started := time.Now()
	applyCommandGroups()
	bufferBuiltinModelCalls()
	cliCommandEnteredRun.Store(false)
	cliCommandLongRunning.Store(false)
	if err := rootCmd.Execute(); err != nil {
		// 失败路径也要落账：一次超时的收集判定同样占了一次调用。os.Exit 不跑 defer，
		// 所以这里和成功路径各显式落一次。
		flushBuiltinModelCalls()
		// A command that chose its own exit status has also already written its own
		// stderr. Neither the log line nor the default error line belongs here: the
		// guard's refusal text is read by a model, and appending to it changes what
		// that model is told to do.
		var coded exitError
		exitCode := 1
		if errors.As(err, &coded) {
			exitCode = coded.ExitCode()
		}
		recordCLIInvocation(started, err, exitCode)
		if errors.As(err, &coded) {
			os.Exit(coded.ExitCode())
		}
		// stderr is for whoever is watching; the log is for whoever is not. The
		// App, collection and hooks all invoke atm unattended, and until this
		// existed their failures vanished with the process.
		logging.Failure("command_failed", failedCommandPath(), err, nil)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flushBuiltinModelCalls()
	recordCLIInvocation(started, nil, 0)
}

// failedCommandPath names the subcommand that failed, without its arguments.
// Arguments are excluded on purpose: `atm todo add "<title>"` and
// `atm knowledge import <path>` carry exactly the content this log must not hold.
func failedCommandPath() string {
	return invocationCommandPath(os.Args[1:])
}

// showHelp is the RunE for group commands (no own action). Combined with
// noSubcommandArgs it makes a bare `atm <group>` print help (exit 0) while an
// unknown subcommand like `atm <group> bogus` errors (exit 1). Without a RunE a
// group is non-Runnable and cobra returns help before ever validating args, so
// unknown subcommands would silently exit 0.
func showHelp(cmd *cobra.Command, args []string) error {
	return cmd.Help()
}

// noSubcommandArgs rejects a stray argument on a group, the way cobra.NoArgs
// does, and adds the suggestions cobra.NoArgs leaves out.
//
// The root command gets suggestions for free — `atm statz` has always answered
// "Did you mean this? stats" — because cobra's default arg validator appends
// them. Groups opted out of that by declaring NoArgs, so `atm todo lst` said only
// that "lst" was unknown, which is the case where a suggestion is worth most:
// there are 30-odd subcommands under todo and the reader has to scan all of them.
//
// It also makes SuggestFor reachable, so a removed alias can point at what
// replaced it instead of dead-ending.
func noSubcommandArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	message := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	// Cobra defaults this to 2 inside its own unexported suggestion path, but not
	// in the exported SuggestionsFor, where a zero means "exact matches only" and
	// quietly reduces the whole feature to prefix matching. Setting it here is what
	// makes `todo lst` find `list`.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		message += "\n\nDid you mean this?\n"
		for _, suggestion := range suggestions {
			message += fmt.Sprintf("\t%s\n", suggestion)
		}
	}
	return errors.New(message)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if jsonOutput {
			output.JSON(map[string]any{
				"schema_version": 1,
				"name":           "atm",
				"version":        rootCmd.Version,
			})
			return
		}
		fmt.Printf("atm %s\n", rootCmd.Version)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&agentFlag, "agent", "", "filter by agent: claude, codex, pi, copilot, qoder, qodercli, qoderwork, grokbuild, antigravity")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&syncBeforeRead, "sync", false, "sync session sources before a read command")
	rootCmd.PersistentFlags().StringVar(&sessionIDFlag, "agent-session", "", "current agent session ID (defaults to ATM_SESSION_ID or agent environment)")
	rootCmd.AddCommand(versionCmd)

}

func resolveAgent() (string, error) {
	if agentFlag == "" {
		return "", nil
	}
	a := config.NormalizeAgent(agentFlag)
	if a == "" {
		return "", fmt.Errorf("unknown agent: %s (use claude, codex, pi, copilot, qoder, qodercli, qoderwork, grokbuild, or antigravity)", agentFlag)
	}
	return a, nil
}

// startOfDayWindow is midnight local, days-1 days back: every --days flag means
// "today plus the N-1 days before it", not a rolling N*24h. Clamped because a
// zero or negative window would silently report nothing.
func startOfDayWindow(now time.Time, days int) time.Time {
	if days < 1 {
		days = 1
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, config.Loc).
		AddDate(0, 0, -(days - 1))
}

// parseSessionSince remains a shared flag parser for non-session adapters such
// as collection history. Session list/search pass their raw flag values to the
// session service, which owns those use cases' time-window validation.
func parseSessionSince(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(config.Loc), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, config.Loc); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: use RFC3339 or YYYY-MM-DD", value)
}

// formatShortDuration renders an elapsed span as Ns / Nm / NhNm, collapsing an
// exact hour to Nh. Three commands had grown their own copy of this ladder and
// only one of them collapsed, so a two-hour-old session read as "2h" in one
// place and "2h0m" in another. quota's countdown stays separate on purpose: it
// counts toward a future reset, so it has a day tier and no seconds tier.
func formatShortDuration(seconds int64) string {
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	}
	hours, minutes := seconds/3600, (seconds%3600)/60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// dayRangeLabel names the window startOfDayWindow computes.
func dayRangeLabel(days int) string {
	if days > 1 {
		return fmt.Sprintf("last %d days", days)
	}
	return "today"
}

// confirmDestructive asks before something irreversible. skip is the command's
// own --yes flag, so a non-interactive caller opts out at the call site rather
// than every prompt growing its own bypass.
func confirmDestructive(cmd *cobra.Command, skip bool, prompt string) (bool, error) {
	if skip {
		return true, nil
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", prompt)
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	// Nothing to read at all: stdin is closed or not a terminal, so no human
	// ever saw the prompt. Failing loudly beats a silent "Cancelled." that a
	// GUI or script caller would read as success.
	if answer == "" && err == io.EOF {
		return false, fmt.Errorf("cannot confirm on a non-interactive stdin: pass --yes to confirm")
	}
	return answer == "y" || answer == "yes", nil
}

// withDB opens a read-only connection for query commands by default. Passing
// readCommand=false opens a writable connection for mutations. Read commands
// only update session data when the user explicitly passes --sync.
func withDB(readCommand bool, fn func(db *sql.DB) error) error {
	var db *sql.DB
	var err error
	if readCommand && !syncBeforeRead {
		db, err = store.OpenReadOnly()
	} else {
		db, err = store.Open()
	}
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()
	if readCommand && syncBeforeRead {
		n, err := store.SyncAll(db)
		if err != nil {
			return fmt.Errorf("sync before read: %w", err)
		}
		if n > 0 && !jsonOutput {
			output.Progress("Synced %d files.", n)
		}
	}
	return fn(db)
}
