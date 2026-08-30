package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	sessionBindAgentFlag        string
	sessionBindProjectFlag      string
	sessionBindCWDFlag          string
	sessionBindForceFlag        bool
	sessionBindReopenReasonFlag string
	sessionUnbindReason         string
	todoMatchProjectFlag        string
	todoMatchLimitFlag          int
	todoMatchPromptFlag         bool
	todoMatchDedupFlag          bool

	todoMatchMinQueryScoreFlag int
)

type compactTodoContext = workapp.TodoSummary

var sessionBindCmd = &cobra.Command{
	Use:   "bind <todo-id>",
	Short: "Bind the current agent session to a todo",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionBind,
}

var sessionCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the todo bound to the current agent session",
	Args:  cobra.NoArgs,
	RunE:  runSessionCurrent,
}

var sessionUnbindCmd = &cobra.Command{
	Use:   "unbind",
	Short: "Unbind the current agent session while preserving binding history",
	Args:  cobra.NoArgs,
	RunE:  runSessionUnbind,
}

var todoMatchCmd = &cobra.Command{
	Use:   "match [goal]",
	Short: "Return compact project- and goal-matched todo candidates",
	Args:  cobra.ArbitraryArgs,
	RunE:  runTodoMatch,
}

func init() {
	sessionBindCmd.Flags().StringVar(&sessionBindAgentFlag, "binding-agent", "", "agent owning the binding (defaults from environment)")
	sessionBindCmd.Flags().StringVar(&sessionBindProjectFlag, "project", "", "binding project (defaults from cwd)")
	sessionBindCmd.Flags().StringVar(&sessionBindCWDFlag, "cwd", "", "binding working directory (defaults to cwd)")
	sessionBindCmd.Flags().BoolVar(&sessionBindForceFlag, "force", false,
		"bind even when the working directory belongs to a different project than the Todo")
	sessionBindCmd.Flags().StringVar(&sessionBindReopenReasonFlag, "reopen-reason", "",
		"why submitted work must resume (required when binding a Todo in review)")
	sessionUnbindCmd.Flags().StringVar(&sessionUnbindReason, "reason", "manual", "unbind reason")
	todoMatchCmd.Flags().StringVar(&todoMatchProjectFlag, "project", "", "project to prioritize (defaults from cwd)")
	todoMatchCmd.Flags().IntVar(&todoMatchLimitFlag, "limit", 3, "maximum compact candidates")
	todoMatchCmd.Flags().BoolVar(&todoMatchPromptFlag, "prompt", false, "emit a minimal agent startup prompt")
	todoMatchCmd.Flags().BoolVar(&todoMatchDedupFlag, "dedup", false, "answer whether an existing todo already covers the goal: require real query relevance, search every project, and say so when nothing matches")
	todoMatchCmd.Flags().IntVar(&todoMatchMinQueryScoreFlag, "min-query-score", workapp.DefaultDedupMinQueryScore, "relevance floor used by --dedup")
	todoMatchCmd.MarkFlagsMutuallyExclusive("prompt", "dedup")
	sessionCmd.AddCommand(sessionBindCmd, sessionCurrentCmd, sessionUnbindCmd)
	todoCmd.AddCommand(todoMatchCmd)
}

func runSessionBind(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return err
	}
	cwd := strings.TrimSpace(sessionBindCWDFlag)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cwd = cleanBindingCWD(cwd)
	project := config.CanonicalProject(strings.TrimSpace(sessionBindProjectFlag))
	if project == "" && cwd != "" {
		project = config.ProjectFromPath(cwd)
	}
	call := cliApplicationCall("session-bind", sessionID)
	agent := strings.TrimSpace(sessionBindAgentFlag)
	if agent != "" {
		agent = config.NormalizeAgent(agent)
	}
	if sessionBindAgentFlag != "" && agent == "" {
		return fmt.Errorf("unknown binding agent: %s (use claude, codex, pi, copilot, qoder, qodercli, qoderwork, grokbuild, or antigravity)", sessionBindAgentFlag)
	}
	if agent == "" {
		agent = call.Actor.Agent
	}

	result, err := workapp.Default.Bind(cmd.Context(), call, workapp.BindInput{
		TodoID:           args[0],
		Agent:            agent,
		Project:          project,
		CWD:              cwd,
		WorkspaceProject: config.ProjectFromPath(cwd),
		Force:            sessionBindForceFlag,
		ReopenReason:     sessionBindReopenReasonFlag,
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	todo, binding := result.Todo, result.Binding
	if jsonOutput {
		output.JSON(map[string]any{"binding": &binding, "todo": workapp.CompactTodo(todo), "reopened": result.Reopened})
		return nil
	}
	verb := "Bound"
	if result.Reopened {
		verb = "Reopened and bound"
	}
	fmt.Printf("%s session %s to %s: %s\n", verb, shortSessionID(sessionID), todo.ID, todo.Title)
	return nil
}

func runSessionCurrent(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return err
	}
	result, err := workapp.Default.Current(
		cmd.Context(), cliApplicationCall("session-current", sessionID), workapp.CurrentInput{},
	)
	if err != nil {
		return err
	}
	bindingContext := result.Context
	if jsonOutput {
		if bindingContext == nil {
			output.JSON(map[string]any{"bound": false, "state": sessionBindingStateUnbound, "session_id": sessionID})
		} else {
			output.JSON(map[string]any{
				"bound":      result.Bound,
				"state":      result.State,
				"session_id": sessionID,
				"binding":    bindingContext.Binding,
				"todo":       bindingContext.Todo,
			})
		}
		return nil
	}
	if bindingContext == nil {
		fmt.Printf("No todo bound to session %s.\n", shortSessionID(sessionID))
		return nil
	}
	if bindingContext.State != sessionBindingStateBound {
		fmt.Printf("Stale binding for session %s: %s -> %s.\n", shortSessionID(sessionID), bindingContext.Binding.TodoID, bindingContext.State)
		return nil
	}
	fmt.Printf("%s  %-12s %s\n", bindingContext.Todo.ID, bindingContext.Todo.Project, bindingContext.Todo.Title)
	return nil
}

func runSessionUnbind(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return err
	}
	result, err := workapp.Default.Unbind(
		cmd.Context(), cliApplicationCall("session-unbind", sessionID), workapp.UnbindInput{Reason: sessionUnbindReason},
	)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"session_id": sessionID, "unbound": result.Unbound})
		return nil
	}
	if !result.Unbound {
		fmt.Printf("No active todo binding for session %s.\n", shortSessionID(sessionID))
		return nil
	}
	fmt.Printf("Unbound session %s.\n", shortSessionID(sessionID))
	return nil
}

func runTodoMatch(cmd *cobra.Command, args []string) error {
	project := config.CanonicalProject(strings.TrimSpace(todoMatchProjectFlag))
	if project == "" {
		if cwd, err := os.Getwd(); err == nil {
			project = config.ProjectFromPath(cwd)
		}
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	sessionID, _ := resolveSessionID(false)
	minQueryScore := 0
	if todoMatchDedupFlag {
		minQueryScore = todoMatchMinQueryScoreFlag
	}
	result, err := workapp.Default.Match(
		cmd.Context(),
		cliApplicationCall("todo-match", sessionID),
		workapp.MatchInput{
			Project: project, Query: query, Limit: todoMatchLimitFlag,
			Deduplicate: todoMatchDedupFlag, MinQueryScore: minQueryScore,
		},
	)
	if err != nil {
		// Keep the established CLI wording for the two flag validation errors;
		// their enforcement still belongs to the use case above.
		if todoMatchLimitFlag < 1 || todoMatchLimitFlag > 10 {
			return fmt.Errorf("--limit must be between 1 and 10")
		}
		if todoMatchDedupFlag && query == "" {
			return fmt.Errorf("--dedup needs a goal to search for")
		}
		return err
	}
	if todoMatchDedupFlag {
		return renderTodoMatchDedup(result)
	}
	if todoMatchPromptFlag {
		if result.Bound && result.Todo != nil {
			fmt.Printf("ATM current: %s | %s | %s | %s. Use `atm todo log \"...\"`; done/wait auto-unbind.\n",
				result.Todo.ID, result.Todo.Project, result.Todo.Status, result.Todo.Title)
			return nil
		}
		printTodoMatchPrompt(result.Project, result.Candidates)
		return nil
	}
	outputMatchResult(result)
	return nil
}

// renderTodoMatchDedup presents the deduplication result, which answers a
// different question from the rest of match: not
// "which todo should this session bind to" but "does one already cover this, or
// should I create one". That needs "nothing matches" to be a possible answer,
// which the ranking alone cannot express — it returns --limit rows whatever was
// searched for, so an unrelated query and a direct hit produce the same shape.
//
// It ignores any current session binding on purpose: whether this session is
// already working on something says nothing about whether the goal is a duplicate.
func renderTodoMatchDedup(result workapp.MatchResult) error {
	if jsonOutput {
		output.JSON(map[string]any{
			"project":         result.Project,
			"query":           result.Query,
			"min_query_score": result.MinQueryScore,
			"duplicate":       result.Duplicate,
			"candidates":      result.Candidates,
		})
		return nil
	}
	if len(result.Candidates) == 0 {
		fmt.Printf("ATM: no active todo matches %q at or above query score %d; creating a new todo is appropriate.\n",
			result.Query, result.MinQueryScore)
		return nil
	}
	fmt.Printf("ATM: %d existing todo(s) may already cover %q; reuse one instead of creating a duplicate.\n",
		len(result.Candidates), result.Query)
	for _, match := range result.Candidates {
		fmt.Printf("  %-5s q=%-4d %-12s %-12s %s\n", match.ID, match.QueryScore, emptyAs(match.Project, "-"), match.Status, match.Title)
	}
	return nil
}

func outputMatchResult(match workapp.MatchResult) {
	result := map[string]any{"project": match.Project, "candidates": match.Candidates}
	if match.Bound && match.Binding != nil && match.Todo != nil {
		result["bound"] = true
		result["binding"] = match.Binding
		result["todo"] = match.Todo
	} else {
		result["bound"] = false
	}
	if jsonOutput {
		output.JSON(result)
		return
	}
	if match.Bound && match.Todo != nil {
		fmt.Printf("Current %s  %s\n", match.Todo.ID, match.Todo.Title)
		return
	}
	for _, candidate := range match.Candidates {
		fmt.Printf("%-5s %-12s %-12s %s\n", candidate.ID, candidate.Project, candidate.Status, candidate.Title)
	}
}

func printTodoMatchPrompt(project string, matches []workapp.MatchCandidate) {
	if len(matches) == 0 {
		fmt.Printf("ATM: no active todo matches project %s; stay unbound unless this is cross-session work.\n", emptyAs(project, "unknown"))
		return
	}
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		parts = append(parts, fmt.Sprintf("%s[%s] %s", match.ID, match.Status, match.Title))
	}
	fmt.Printf("ATM candidates(%s): %s. After reading the user request, bind the best match with `atm session bind <id>`; if none fits, stay unbound.\n", emptyAs(project, "unknown"), strings.Join(parts, "; "))
}
