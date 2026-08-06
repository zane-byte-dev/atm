package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	sessionBindAgentFlag   string
	sessionBindProjectFlag string
	sessionBindCWDFlag     string
	sessionUnbindReason    string
	todoMatchProjectFlag   string
	todoMatchLimitFlag     int
	todoMatchPromptFlag    bool
	todoMatchDedupFlag     bool

	todoMatchMinQueryScoreFlag int
)

type compactTodoContext struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project,omitempty"`
	Status  string `json:"status"`
}

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
	sessionUnbindCmd.Flags().StringVar(&sessionUnbindReason, "reason", "manual", "unbind reason")
	todoMatchCmd.Flags().StringVar(&todoMatchProjectFlag, "project", "", "project to prioritize (defaults from cwd)")
	todoMatchCmd.Flags().IntVar(&todoMatchLimitFlag, "limit", 3, "maximum compact candidates")
	todoMatchCmd.Flags().BoolVar(&todoMatchPromptFlag, "prompt", false, "emit a minimal agent startup prompt")
	todoMatchCmd.Flags().BoolVar(&todoMatchDedupFlag, "dedup", false, "answer whether an existing todo already covers the goal: require real query relevance, search every project, and say so when nothing matches")
	todoMatchCmd.Flags().IntVar(&todoMatchMinQueryScoreFlag, "min-query-score", store.TodoDedupMinQueryScore, "relevance floor used by --dedup")
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
	agent := normalizeBindingAgent(sessionBindAgentFlag)
	if sessionBindAgentFlag != "" && agent == "" {
		return fmt.Errorf("unknown binding agent: %s (use claude, codex, pi, copilot, qoder, qodercli, qoderwork, or grokbuild)", sessionBindAgentFlag)
	}

	var todo store.Todo
	var binding *store.TodoSessionBinding
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		current, err := transaction.Todo(args[0])
		if err != nil {
			return err
		}
		if !store.TodoIsActive(*current) {
			return fmt.Errorf("cannot bind completed todo %s with status %s", current.ID, current.Status)
		}
		current.Status = store.TodoStatusInProgress
		current.WakeCondition = ""
		current.ReviewAt = ""
		if current.StartTS == nil {
			now := time.Now().In(config.Loc).Unix()
			current.StartTS = &now
		}
		binding, err = transaction.BindSession(store.TodoSessionBinding{
			SessionID: sessionID, TodoID: current.ID, Agent: agent, Project: project, CWD: cwd,
		})
		todo = *current
		return err
	})
	if err != nil {
		return err
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	// Binding succeeds without a markdown card, but the prompt points agents at
	// `todo doc` first. Ensure the card exists so GUI-created todos hand off.
	if err := ensureTodoDocs(tf, todo.ID); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"binding": binding, "todo": compactTodo(todo)})
		return nil
	}
	fmt.Printf("Bound session %s to %s: %s\n", shortSessionID(sessionID), todo.ID, todo.Title)
	return nil
}

func runSessionCurrent(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return err
	}
	context, err := currentSessionBindingContext(sessionID)
	if err != nil {
		return err
	}
	if jsonOutput {
		if context == nil {
			output.JSON(map[string]any{"bound": false, "state": sessionBindingStateUnbound, "session_id": sessionID})
		} else {
			output.JSON(map[string]any{
				"bound":      context.State == sessionBindingStateBound,
				"state":      context.State,
				"session_id": sessionID,
				"binding":    context.Binding,
				"todo":       context.Todo,
			})
		}
		return nil
	}
	if context == nil {
		fmt.Printf("No todo bound to session %s.\n", shortSessionID(sessionID))
		return nil
	}
	if context.State != sessionBindingStateBound {
		fmt.Printf("Stale binding for session %s: %s -> %s.\n", shortSessionID(sessionID), context.Binding.TodoID, context.State)
		return nil
	}
	fmt.Printf("%s  %-12s %s\n", context.Todo.ID, context.Todo.Project, context.Todo.Title)
	return nil
}

func runSessionUnbind(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return err
	}
	var changed bool
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		var err error
		changed, err = transaction.UnbindSession(sessionID, sessionUnbindReason)
		return err
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"session_id": sessionID, "unbound": changed})
		return nil
	}
	if !changed {
		fmt.Printf("No active todo binding for session %s.\n", shortSessionID(sessionID))
		return nil
	}
	fmt.Printf("Unbound session %s.\n", shortSessionID(sessionID))
	return nil
}

func runTodoMatch(cmd *cobra.Command, args []string) error {
	if todoMatchLimitFlag < 1 || todoMatchLimitFlag > 10 {
		return fmt.Errorf("--limit must be between 1 and 10")
	}
	project := config.CanonicalProject(strings.TrimSpace(todoMatchProjectFlag))
	if project == "" {
		if cwd, err := os.Getwd(); err == nil {
			project = config.ProjectFromPath(cwd)
		}
	}
	query := strings.TrimSpace(strings.Join(args, " "))

	if todoMatchDedupFlag {
		return runTodoMatchDedup(project, query)
	}

	var binding *store.TodoSessionBinding
	var boundTodo *store.Todo
	if sessionID, _ := resolveSessionID(false); sessionID != "" {
		binding, boundTodo, _ = currentBindingAndTodo(sessionID)
	}
	if binding != nil && boundTodo != nil {
		if todoMatchPromptFlag {
			fmt.Printf("ATM current: %s | %s | %s | %s. Use `atm todo log \"...\"`; done/wait auto-unbind.\n", boundTodo.ID, boundTodo.Project, boundTodo.Status, boundTodo.Title)
			return nil
		}
		outputMatchResult(project, binding, boundTodo, nil)
		return nil
	}

	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	matches := store.MatchTodos(tf, project, query, todoMatchLimitFlag)
	if todoMatchPromptFlag {
		printTodoMatchPrompt(project, matches)
		return nil
	}
	outputMatchResult(project, nil, nil, matches)
	return nil
}

// runTodoMatchDedup answers a different question from the rest of match: not
// "which todo should this session bind to" but "does one already cover this, or
// should I create one". That needs "nothing matches" to be a possible answer,
// which the ranking alone cannot express — it returns --limit rows whatever was
// searched for, so an unrelated query and a direct hit produce the same shape.
//
// It ignores any current session binding on purpose: whether this session is
// already working on something says nothing about whether the goal is a duplicate.
func runTodoMatchDedup(project, query string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("--dedup needs a goal to search for")
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	matches := store.MatchTodosWithOptions(tf, store.TodoMatchOptions{
		Project:       project,
		Query:         query,
		Limit:         todoMatchLimitFlag,
		MinQueryScore: todoMatchMinQueryScoreFlag,
		AllProjects:   true,
	})
	if jsonOutput {
		output.JSON(map[string]any{
			"project":         project,
			"query":           query,
			"min_query_score": todoMatchMinQueryScoreFlag,
			"duplicate":       len(matches) > 0,
			"candidates":      matches,
		})
		return nil
	}
	if len(matches) == 0 {
		fmt.Printf("ATM: no active todo matches %q at or above query score %d; creating a new todo is appropriate.\n",
			query, todoMatchMinQueryScoreFlag)
		return nil
	}
	fmt.Printf("ATM: %d existing todo(s) may already cover %q; reuse one instead of creating a duplicate.\n", len(matches), query)
	for _, match := range matches {
		fmt.Printf("  %-5s q=%-4d %-12s %-12s %s\n", match.ID, match.QueryScore, emptyAs(match.Project, "-"), match.Status, match.Title)
	}
	return nil
}

func outputMatchResult(project string, binding *store.TodoSessionBinding, todo *store.Todo, matches []store.TodoMatch) {
	result := map[string]any{"project": project, "candidates": matches}
	if binding != nil && todo != nil {
		result["bound"] = true
		result["binding"] = binding
		result["todo"] = compactTodo(*todo)
	} else {
		result["bound"] = false
	}
	if jsonOutput {
		output.JSON(result)
		return
	}
	if binding != nil && todo != nil {
		fmt.Printf("Current %s  %s\n", todo.ID, todo.Title)
		return
	}
	for _, match := range matches {
		fmt.Printf("%-5s %-12s %-12s %s\n", match.ID, match.Project, match.Status, match.Title)
	}
}

func printTodoMatchPrompt(project string, matches []store.TodoMatch) {
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

func currentBindingAndTodo(sessionID string) (*store.TodoSessionBinding, *store.Todo, error) {
	context, err := currentSessionBindingContext(sessionID)
	if err != nil {
		return nil, nil, err
	}
	if context == nil || context.State != sessionBindingStateBound || context.Todo == nil {
		return nil, nil, nil
	}
	todo := &store.Todo{
		ID:      context.Todo.ID,
		Title:   context.Todo.Title,
		Project: context.Todo.Project,
		Status:  context.Todo.Status,
	}
	binding := context.Binding
	return &binding, todo, nil
}

func resolveSessionID(required bool) (string, error) {
	for _, value := range []string{
		sessionIDFlag,
		os.Getenv("ATM_SESSION_ID"),
		os.Getenv("CODEX_THREAD_ID"),
		os.Getenv("CLAUDE_CODE_SESSION_ID"),
		os.Getenv("PI_SESSION_ID"),
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value, nil
		}
	}
	if required {
		return "", fmt.Errorf("current session ID unavailable; pass --agent-session or set ATM_SESSION_ID")
	}
	return "", nil
}

func resolveCurrentTodoID() (string, error) {
	sessionID, err := resolveSessionID(true)
	if err != nil {
		return "", err
	}
	binding, err := store.CurrentTodoBinding(sessionID)
	// No database yet means nothing has ever been bound — the same answer as an
	// empty bindings table, and a more useful one than "run atm sync".
	if err != nil && !errors.Is(err, store.ErrDatabaseMissing) {
		return "", err
	}
	if binding == nil {
		return "", fmt.Errorf("no todo bound to current session; run `atm todo match --prompt` then `atm session bind <id>`")
	}
	return binding.TodoID, nil
}

func optionalTodoID(args []string) (string, error) {
	if len(args) > 0 && args[0] != "current" {
		return args[0], nil
	}
	return resolveCurrentTodoID()
}

func normalizeBindingAgent(value string) string {
	if agent := config.NormalizeAgent(strings.TrimSpace(value)); agent != "" {
		return agent
	}
	if os.Getenv("CODEX_THREAD_ID") != "" {
		return "codex"
	}
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return "claude"
	}
	if os.Getenv("PI_SESSION_ID") != "" {
		return "pi"
	}
	return ""
}

// todoCreatorFromEnvironment answers "who is filing this todo" for the commands
// that create one. An agent session in the environment means an agent is at the
// keyboard; anything else is the human, because a plain terminal and the desktop
// app are both the single person this installation belongs to. An agent whose
// environment carries no session ID can still say so with --creator.
func todoCreatorFromEnvironment() string {
	if agent := normalizeBindingAgent(""); agent != "" {
		return agent
	}
	return store.TodoCreatorMe
}

// resolveTodoCreator settles the creator of a todo about to be created: an
// explicit --creator wins, because only the caller knows when the environment is
// misleading, and otherwise the environment is read.
func resolveTodoCreator(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return store.NormalizeTodoCreator(flag)
	}
	return todoCreatorFromEnvironment(), nil
}

func compactTodo(todo store.Todo) compactTodoContext {
	return compactTodoContext{ID: todo.ID, Title: todo.Title, Project: todo.Project, Status: todo.Status}
}

func shortSessionID(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

// todoSourceFromSession labels a todo with the agent session that filed it.
// Returns "" when no session is in the environment, so a caller can fall back to
// its own default rather than recording a bare "session:" prefix.
func todoSourceFromSession() string {
	sid := os.Getenv("CLAUDE_CODE_SESSION_ID")
	if sid == "" {
		return ""
	}
	return "session:" + shortSessionID(sid)
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func cleanBindingCWD(value string) string {
	if value == "" {
		return ""
	}
	cleaned, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return cleaned
}
