package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

var (
	todoListPriorityFlag   string
	todoStatusFlag         string
	todoProjectFlag        string
	todoListQueryFlag      string
	todoListCreatorFlag    string
	todoListLimitFlag      int
	todoListOffsetFlag     int
	todoAddPriorityFlag    string
	todoAddProjectFlag     string
	todoAddStatusFlag      string
	todoAddWakeFlag        string
	todoAddReviewAtFlag    string
	todoAddCreatorFlag     string
	todoSourceFlag         string
	todoDescFlag           string
	todoDescFileFlag       string
	todoBatchFlag          bool
	todoReasonFlag         string
	todoEditTitleFlag      string
	todoEditDescFlag       string
	todoEditDescFileFlag   string
	todoLogMessageFileFlag string
	todoEditPriorityFlag   string
	todoEditProjectFlag    string
	todoEditSourceFlag     string
	todoEditStatusFlag     string
	todoEditWakeFlag       string
	todoEditReviewAtFlag   string
	todoMoveProjectFlag    string
	todoLogSectionFlag     string
	todoDocInitFlag        bool
	todoDeleteProjectFlag  string
	todoDeleteYesFlag      bool
	todoOnDoneFlag         string
	todoCaptureProjectFlag string
	todoPromptCopyFlag     bool
	todoWaitWakeFlag       string
	todoWaitReviewAtFlag   string
	todoMaintainLimitFlag  int
	todoContextCWD         string
	todoSubmitReasonFlag   string
	todoRunPolicyFlag      string
	todoRunCWDFlag         string
	todoRunTailFollowFlag  bool
	todoRunTailBytesFlag   int64
)

func init() {
	todoListCmd.Flags().StringVar(&todoListPriorityFlag, "priority", "", "filter by priority: P0, P1, P2")
	todoListCmd.Flags().StringVar(&todoStatusFlag, "status", "", "filter by status: open, in_progress, waiting, review, blocked, done, dropped, archived, trashed, all (default: active)")
	todoListCmd.Flags().StringVar(&todoProjectFlag, "project", "", "filter by project name")
	todoListCmd.Flags().StringVar(&todoListQueryFlag, "query", "", "filter by id, title, description, project, source, or todo document")
	todoListCmd.Flags().StringVar(&todoListCreatorFlag, "creator", "", "filter by creator: "+strings.Join(store.TodoCreatorVocabulary, ", "))
	todoListCmd.Flags().IntVar(&todoListLimitFlag, "limit", 0, "maximum number of todos (0 means all)")
	todoListCmd.Flags().IntVar(&todoListOffsetFlag, "offset", 0, "number of todos to skip")

	todoAddCmd.Flags().StringVar(&todoAddPriorityFlag, "priority", "P1", "priority: P0, P1, P2")
	todoAddCmd.Flags().StringVar(&todoAddProjectFlag, "project", "", "project name")
	todoAddCmd.Flags().StringVar(&todoAddStatusFlag, "status", store.TodoStatusOpen, "initial status: open, in_progress, waiting, review, blocked")
	todoAddCmd.Flags().StringVar(&todoAddWakeFlag, "wake", "", "condition that should wake a waiting todo")
	todoAddCmd.Flags().StringVar(&todoAddReviewAtFlag, "review-at", "", "next review date (YYYY-MM-DD)")
	todoAddCmd.Flags().StringVar(&todoSourceFlag, "source", "", "source of the task")
	todoAddCmd.Flags().StringVar(&todoAddCreatorFlag, "creator", "", "who filed it: "+strings.Join(store.TodoCreatorVocabulary, ", ")+" (default: the agent in the environment, otherwise me)")
	todoAddCmd.Flags().StringVar(&todoDescFlag, "desc", "", "description")
	todoAddCmd.Flags().StringVar(&todoDescFileFlag, "desc-file", "", "read description from a file (use - for stdin)")
	todoAddCmd.Flags().BoolVar(&todoBatchFlag, "batch", false, "read YAML/JSON batch input from stdin")
	todoAddCmd.MarkFlagsMutuallyExclusive("desc", "desc-file")
	todoAddCmd.MarkFlagsMutuallyExclusive("batch", "desc-file")

	todoDoneCmd.Flags().StringVar(&todoReasonFlag, "reason", "", "closing reason")
	todoDropCmd.Flags().StringVar(&todoReasonFlag, "reason", "", "dropping reason")
	todoSubmitCmd.Flags().StringVar(&todoSubmitReasonFlag, "reason", "", "submission summary or evidence")

	todoEditCmd.Flags().StringVar(&todoEditTitleFlag, "title", "", "new title")
	todoEditCmd.Flags().StringVar(&todoEditDescFlag, "desc", "", "new description")
	todoEditCmd.Flags().StringVar(&todoEditDescFileFlag, "desc-file", "", "read new description from a file (use - for stdin)")
	todoEditCmd.MarkFlagsMutuallyExclusive("desc", "desc-file")
	todoEditCmd.Flags().StringVar(&todoEditPriorityFlag, "priority", "", "new priority: P0, P1, P2")
	todoEditCmd.Flags().StringVar(&todoEditProjectFlag, "project", "", "new project name")
	todoEditCmd.Flags().StringVar(&todoEditSourceFlag, "source", "", "new source")
	todoEditCmd.Flags().StringVar(&todoEditStatusFlag, "status", "", "new status: open, in_progress, waiting, review, blocked")
	todoEditCmd.Flags().StringVar(&todoEditWakeFlag, "wake", "", "new wake condition (empty clears it)")
	todoEditCmd.Flags().StringVar(&todoEditReviewAtFlag, "review-at", "", "new review date YYYY-MM-DD (empty clears it)")

	todoMoveCmd.Flags().StringVar(&todoMoveProjectFlag, "project", "", "target project name")
	todoMoveCmd.MarkFlagRequired("project")

	todoLogCmd.Flags().StringVar(&todoLogSectionFlag, "section", "", "target section name (default: 进展)")
	todoLogCmd.Flags().StringVar(&todoLogMessageFileFlag, "message-file", "", "read the entry from a file (use - for stdin)")
	todoDocCmd.Flags().BoolVar(&todoDocInitFlag, "init", false, "create doc from template")

	todoDeleteCmd.Flags().StringVar(&todoDeleteProjectFlag, "project", "", "delete all todos in a project")
	todoDeleteCmd.Flags().BoolVarP(&todoDeleteYesFlag, "yes", "y", false, "skip the confirmation prompt")

	todoAddCmd.Flags().StringVar(&todoOnDoneFlag, "on-done", "", "command to execute when todo is done")

	todoCaptureCmd.Flags().StringVar(&todoCaptureProjectFlag, "project", "", "project name (default: cwd basename)")

	todoPromptCmd.Flags().BoolVar(&todoPromptCopyFlag, "copy", false, "copy the prompt to the clipboard")
	todoRunCmd.Flags().StringVar(&todoRunPolicyFlag, "policy", "guarded", "permission policy: guarded or trusted")
	todoRunCmd.Flags().StringVar(&todoRunCWDFlag, "cwd", "", "working directory (defaults from Todo bindings or current directory)")
	todoRunTailCmd.Flags().BoolVarP(&todoRunTailFollowFlag, "follow", "f", false, "keep following while the run is active")
	todoRunTailCmd.Flags().Int64Var(&todoRunTailBytesFlag, "bytes", 0, "show only the latest N bytes (0 means the full log)")

	todoWaitCmd.Flags().StringVar(&todoWaitWakeFlag, "wake", "", "condition that should wake the todo")
	todoWaitCmd.Flags().StringVar(&todoWaitReviewAtFlag, "review-at", "", "next review date (YYYY-MM-DD)")
	todoMaintainCmd.Flags().IntVar(&todoMaintainLimitFlag, "limit", 3, "maximum items in this maintenance batch")
	for _, contextCmd := range []*cobra.Command{todoContextCmd, todoReviewContextCmd} {
		contextCmd.Flags().StringVar(&todoContextCWD, "cwd", "", "Git worktree to inspect (required when active todo bindings use multiple worktrees)")
	}

	todoCmd.AddCommand(todoArchiveCmd, todoUnarchiveCmd, todoTrashCmd, todoRestoreCmd, todoListCmd, todoAddCmd, todoStartCmd, todoSubmitCmd, todoDoneCmd, todoDropCmd, todoShowCmd, todoContextCmd, todoReviewContextCmd, todoPromptCmd, todoRunCmd, todoRunsCmd, todoRunTailCmd, todoRunControllerCmd, todoEditCmd, todoMoveCmd, todoLogCmd, todoDocCmd, todoDeleteCmd, todoCaptureCmd, todoFocusCmd, todoWaitCmd, todoMaintainCmd)
	rootCmd.AddCommand(todoCmd)
}

var todoCmd = &cobra.Command{
	Use:   "todo",
	Short: "Manage work items",
	// NoArgs so an unknown subcommand (e.g. `atm todo add-progress ...`) errors
	// loudly instead of silently falling through to the default list action.
	// `atm todo` with no args still lists (len(args)==0 passes NoArgs).
	Args: cobra.NoArgs,
	RunE: runTodoList,
}

var todoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List todos",
	RunE:  runTodoList,
}

var todoAddCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Add a new todo",
	Example: `  atm todo add "Fix release checks" --project atm --desc-file notes.md
  printf 'Multiline description\nwith details\n' | atm todo add "Fix release checks" --desc-file -
  cat <<'YAML' | atm todo add --batch
  project: atm
  priority: P1
  items:
    - title: Fix release checks
      desc: Verify the release checklist
    - title: Document rollback
  YAML`,
	Args: cobra.ArbitraryArgs,
	RunE: runTodoAdd,
}

var todoDoneCmd = &cobra.Command{
	Use:   "done [id]",
	Short: "Mark a todo as done",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoDone,
}

var todoSubmitCmd = &cobra.Command{
	Use:   "submit [id]",
	Short: "Submit completed work for confirmation",
	Long: `Move an in-progress Todo to review after work has been completed.

Submitting is an explicit lifecycle transition. It records an optional summary,
closes active session bindings, and never marks the Todo done.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTodoSubmit,
}

var todoStartCmd = &cobra.Command{
	Use:   "start <id>",
	Short: "Start or reopen a todo (records start time for session linking)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoStart,
}

var todoFocusCmd = &cobra.Command{
	Use:        "focus <id>",
	Short:      "Deprecated alias for start",
	Deprecated: "focus is derived from the current session binding; use todo start",
	Args:       cobra.ExactArgs(1),
	RunE:       runTodoFocus,
}

var todoWaitCmd = &cobra.Command{
	Use:   "wait [id]",
	Short: "Set a todo to waiting until a condition or review date",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoWait,
}

var todoMaintainCmd = &cobra.Command{
	Use:   "maintain <id>",
	Short: "Tag a todo as bounded maintenance work",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoMaintain,
}

var todoPromptCmd = &cobra.Command{
	Use:   "prompt <id>",
	Short: "Print the line to paste into a fresh agent session",
	Long: `Print a short pointer a human pastes into a new agent session.

The pointer names the todo and the commands that load it; the agent reads the
requirement from ATM itself, so it always works from the current version rather
than a copied snapshot.`,
	Example: `  atm todo prompt t89
  atm todo prompt t89 --copy`,
	Args: cobra.ExactArgs(1),
	RunE: runTodoPrompt,
}

var todoRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Dispatch a Todo to an Agent CLI",
	Long: `Start the Todo, claim one durable task run, then launch a background
controller that runs the selected Agent. A successful Agent exit submits the
Todo to review; it never marks the Todo done.`,
	Example: `  atm todo run t240
  atm todo run t240 --cwd /path/to/repo
  atm todo runs t240
  atm todo tail t240 -f`,
	Args: cobra.ExactArgs(1),
	RunE: runTodoRun,
}

var todoRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List Agent runs for a Todo",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoRuns,
}

var todoRunTailCmd = &cobra.Command{
	Use:   "tail <id>",
	Short: "Print the latest Agent run log",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoRunTail,
}

var todoRunControllerCmd = &cobra.Command{
	Use:    "run-controller <run-id>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runTodoRunController,
}

var todoDropCmd = &cobra.Command{
	Use:   "drop [id]",
	Short: "Mark a todo as dropped",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoDrop,
}

var todoShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show todo details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoShow,
}

var todoContextCmd = &cobra.Command{
	Use:   "context [id]",
	Short: "Build a read-only Todo, session, and Git context",
	Long: `Build a compact, live context snapshot without changing Todo state.

The result keeps work state, Git implementation state, reported milestones,
session bindings, and verification status separate. It lists staged, unstaged,
and untracked files, but does not include full diffs or run tests. The same
snapshot can be used to resume work, hand it off, or begin a review.`,
	Example: `  atm todo context
  atm todo context t89 --json
  atm todo context t89 --cwd /path/to/worktree`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTodoContext,
}

var todoReviewContextCmd = &cobra.Command{
	Use:        "review-context [id]",
	Short:      "Deprecated alias for context",
	Deprecated: "use todo context; context snapshots are not review state transitions",
	Args:       cobra.MaximumNArgs(1),
	RunE:       runTodoContext,
}

var todoEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a todo's metadata and work state",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoEdit,
}

var todoMoveCmd = &cobra.Command{
	Use:   "move <id> --project <name>",
	Short: "Move a todo to a different project",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoMove,
}

var todoLogCmd = &cobra.Command{
	Use:   "log [id] <message>",
	Short: "Append a progress entry to todo's doc",
	Long:  "Append one concise milestone to progress (one paragraph, maximum 400 Unicode characters). Use --section 分析 for investigation or design detail. Referenced tNN IDs must already exist.",
	Example: `  atm todo log t65 "结果：服务端固化完成；证据：端到端测试通过；下一步：适配 Wanda 原生执行"
  atm todo log t65 "完整源码调查和方案比较" --section 分析
  atm todo log t65 --section 分析 --message-file analysis.md
  atm todo lint t65`,
	// Zero arguments is valid with --message-file on a bound session; the message
	// source is checked in runTodoLog, which knows about the flag.
	Args: cobra.MaximumNArgs(2),
	RunE: runTodoLog,
}

var todoDocCmd = &cobra.Command{
	Use:   "doc [id]",
	Short: "View or init a todo's markdown doc",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoDoc,
}

var todoDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Permanently delete a todo",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoDelete,
}

var todoArchiveCmd = &cobra.Command{
	Use:   "archive <id>...",
	Short: "Move closed todos out of the working set",
	Long: `Move done or dropped todos out of the working set.

Archived todos keep their row, their ID, and their markdown card: dependencies
and progress notes may still refer to them, and the ID is never reused. They no
longer appear in listings, the dashboard, or matching. Use
` + "`atm todo list --status archived`" + ` to read them and ` + "`atm todo unarchive`" + ` to bring
one back.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTodoArchive,
}

var todoUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id>...",
	Short: "Bring archived todos back into the working set",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTodoUnarchive,
}

var todoTrashCmd = &cobra.Command{
	Use:   "trash <id>...",
	Short: "Move todos to the trash without deleting them",
	Long: `Move todos out of the working set without deleting their rows, markdown,
progress, dependencies, or history. No confirmation is required because the
operation is reversible with ` + "`atm todo restore`" + `.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTodoTrash,
}

var todoRestoreCmd = &cobra.Command{
	Use:   "restore <id>...",
	Short: "Restore todos from the trash",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTodoRestore,
}

func validateWorkStatus(value string) error {
	switch value {
	case store.TodoStatusOpen, store.TodoStatusInProgress, store.TodoStatusWaiting,
		store.TodoStatusReview, store.TodoStatusBlocked:
		return nil
	default:
		return fmt.Errorf("invalid todo status %q (use open, in_progress, waiting, review, or blocked)", value)
	}
}

func validateReviewAt(value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.ParseInLocation("2006-01-02", value, config.Loc); err != nil {
		return fmt.Errorf("invalid review date %q (use YYYY-MM-DD)", value)
	}
	return nil
}
