package cmd

import (
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

var (
	todoListPriorityFlag         string
	todoStatusFlag               string
	todoProjectFlag              string
	todoListQueryFlag            string
	todoListCreatorFlag          string
	todoListLimitFlag            int
	todoListOffsetFlag           int
	todoAddPriorityFlag          string
	todoAddProjectFlag           string
	todoAddCreatorFlag           string
	todoSourceFlag               string
	todoDescFlag                 string
	todoDescFileFlag             string
	todoAddImageFlags            []string
	todoBatchFlag                bool
	todoReasonFlag               string
	todoEditTitleFlag            string
	todoEditDescFlag             string
	todoEditDescFileFlag         string
	todoLogMessageFileFlag       string
	todoEditPriorityFlag         string
	todoEditProjectFlag          string
	todoEditSourceFlag           string
	todoEditStatusFlag           string
	todoEditWakeFlag             string
	todoEditReviewAtFlag         string
	todoEditMaintenanceLimitFlag int
	todoLogSectionFlag           string
	todoDocInitFlag              bool
	todoDeleteProjectFlag        string
	todoDeleteYesFlag            bool
	todoOnDoneFlag               string
	todoHandoffCWDFlag           string
	todoHandoffPrintFlag         bool
	todoHandoffCopyFlag          bool
	todoContextCWD               string
	todoSubmitReasonFlag         string
)

func init() {
	todoListCmd.Flags().StringVar(&todoListPriorityFlag, "priority", "", "filter by priority: P0, P1, P2")
	todoListCmd.Flags().StringVar(&todoStatusFlag, "status", "", "filter by status: open, in_progress, review, done, archived, all (default: active)")
	todoListCmd.Flags().StringVar(&todoProjectFlag, "project", "", "filter by project name (case-insensitive substring)")
	todoListCmd.Flags().StringVar(&todoListQueryFlag, "query", "", "filter by id, title, description, project, source, or todo document")
	todoListCmd.Flags().StringVar(&todoListCreatorFlag, "creator", "", "filter by creator: "+strings.Join(store.TodoCreatorVocabulary, ", "))
	todoListCmd.Flags().IntVar(&todoListLimitFlag, "limit", 0, "maximum number of todos (0 means all)")
	todoListCmd.Flags().IntVar(&todoListOffsetFlag, "offset", 0, "number of todos to skip")

	todoAddCmd.Flags().StringVar(&todoAddPriorityFlag, "priority", "P1", "priority: P0, P1, P2")
	todoAddCmd.Flags().StringVar(&todoAddProjectFlag, "project", "", "project name")
	todoAddCmd.Flags().StringVar(&todoSourceFlag, "source", "", "source of the task")
	todoAddCmd.Flags().StringVar(&todoAddCreatorFlag, "creator", "", "who filed it: "+strings.Join(store.TodoCreatorVocabulary, ", ")+" (default: the agent in the environment, otherwise me)")
	todoAddCmd.Flags().StringVar(&todoDescFlag, "desc", "", "single-line description (use --desc-file for multiline text)")
	todoAddCmd.Flags().StringVar(&todoDescFileFlag, "desc-file", "", "read description from a file (use - for stdin)")
	todoAddCmd.Flags().StringArrayVar(&todoAddImageFlags, "image", nil, "attach a local image (repeatable; PNG, JPEG, WebP, GIF, or HEIC; max 10 MB each, 10 total)")
	todoAddCmd.Flags().BoolVar(&todoBatchFlag, "batch", false, "read YAML/JSON batch input from stdin")
	todoAddCmd.MarkFlagsMutuallyExclusive("desc", "desc-file")
	todoAddCmd.MarkFlagsMutuallyExclusive("batch", "desc-file")
	todoAddCmd.MarkFlagsMutuallyExclusive("batch", "image")

	todoDoneCmd.Flags().StringVar(&todoReasonFlag, "reason", "", "closing reason")
	todoSubmitCmd.Flags().StringVar(&todoSubmitReasonFlag, "reason", "", "submission summary or evidence")

	todoEditCmd.Flags().StringVar(&todoEditTitleFlag, "title", "", "new title")
	todoEditCmd.Flags().StringVar(&todoEditDescFlag, "desc", "", "new single-line description (use --desc-file for multiline text)")
	todoEditCmd.Flags().StringVar(&todoEditDescFileFlag, "desc-file", "", "read new description from a file (use - for stdin)")
	todoEditCmd.MarkFlagsMutuallyExclusive("desc", "desc-file")
	todoEditCmd.Flags().StringVar(&todoEditPriorityFlag, "priority", "", "new priority: P0, P1, P2")
	todoEditCmd.Flags().StringVar(&todoEditProjectFlag, "project", "", "new project name")
	todoEditCmd.Flags().StringVar(&todoEditSourceFlag, "source", "", "new source")
	todoEditCmd.Flags().StringVar(&todoEditStatusFlag, "status", "", "return lifecycle status to open")
	todoEditCmd.Flags().StringVar(&todoEditWakeFlag, "wake", "", "waiting-style condition for in_progress (empty clears it)")
	todoEditCmd.Flags().StringVar(&todoEditReviewAtFlag, "review-at", "", "new review date YYYY-MM-DD (empty clears it)")
	todoEditCmd.Flags().IntVar(&todoEditMaintenanceLimitFlag, "maintenance-limit", 0, "bounded maintenance batch size (0 clears maintenance)")

	todoLogCmd.Flags().StringVar(&todoLogSectionFlag, "section", "", "target section name (default: 进展)")
	todoLogCmd.Flags().StringVar(&todoLogMessageFileFlag, "message-file", "", "read the entry from a file (use - for stdin)")
	todoDocCmd.Flags().BoolVar(&todoDocInitFlag, "init", false, "create doc from template")

	todoDeleteCmd.Flags().StringVar(&todoDeleteProjectFlag, "project", "", "delete all todos in a project (exact name; unlike the list filters this is not a substring match)")
	todoDeleteCmd.Flags().BoolVarP(&todoDeleteYesFlag, "yes", "y", false, "skip the confirmation prompt")

	todoAddCmd.Flags().StringVar(&todoOnDoneFlag, "on-done", "", "command to execute when todo is done")

	todoHandoffCmd.Flags().StringVar(&todoHandoffCWDFlag, "cwd", "", "working directory to open in Codex (defaults from Todo bindings or project)")
	todoHandoffCmd.Flags().BoolVar(&todoHandoffPrintFlag, "print", false, "print the deep link instead of opening Codex")
	todoHandoffCmd.Flags().BoolVar(&todoHandoffCopyFlag, "copy", false, "copy the agent pointer instead of opening Codex")
	todoHandoffCmd.MarkFlagsMutuallyExclusive("print", "copy")
	for _, contextCmd := range []*cobra.Command{todoContextCmd} {
		contextCmd.Flags().StringVar(&todoContextCWD, "cwd", "", "Git worktree to inspect (required when active todo bindings use multiple worktrees)")
	}

	todoCmd.AddCommand(todoArchiveCmd, todoRestoreCmd, todoListCmd, todoAddCmd, todoStartCmd, todoSubmitCmd, todoDoneCmd, todoShowCmd, todoContextCmd, todoHandoffCmd, todoEditCmd, todoLogCmd, todoDocCmd, todoDeleteCmd)
	rootCmd.AddCommand(todoCmd)
}

var todoCmd = &cobra.Command{
	Use:   "todo",
	Short: "Manage work items",
	// The flow is spelled out because the groups below cannot show it: cobra sorts
	// each group alphabetically, so Lifecycle reads add, archive, done, restore,
	// start, submit — the right six names in the wrong order. Turning sorting off
	// is a package-wide switch that would leave every other group printing in
	// init() order, which is filename order and means nothing to a reader.
	Long: `Manage work items.

The everyday path is four steps, with archival beside it rather than inside it:

  add → start → submit → done
            ↘ archive ↔ restore

An Agent binds its session with ` + "`atm session bind <id>`" + ` instead of ` + "`start`" + `, and
submits for review rather than marking work done. Waiting is not a state: keep a
wake condition or review date on in-progress work with ` + "`edit --wake`" + ` or
` + "`edit --review-at`" + `. Archiving is reversible and needs no confirmation; only
` + "`delete`" + ` is permanent.`,
	// Rejecting args so an unknown subcommand (e.g. `atm todo add-progress ...`)
	// errors loudly instead of silently falling through to the default list action.
	// `atm todo` with no args still lists. noSubcommandArgs rather than
	// cobra.NoArgs because this group has thirty-odd subcommands, which is exactly
	// where a "did you mean" is worth having.
	Args: noSubcommandArgs,
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
  atm todo add "Review screenshots" --image before.png --image after.webp
  atm todo add "把发布检查修一下" --project atm --refine
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
	Short: "Accept a todo as done (human only; agents use submit)",
	Long:  "Accept completed work as done. This is the human review decision; Agent work must use `atm todo submit` instead.",
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
	Use: "start <id>",
	// `todo focus` was a deprecated alias for this, removed once the --lane flag
	// that was its only difference went away. Named here so the old spelling gets
	// pointed at the new one instead of a bare "unknown command" — a stale skill
	// file or a copied note is the likeliest source of it.
	SuggestFor: []string{"focus"},
	Short:      "Start or reopen a todo (records start time for session linking)",
	Args:       cobra.ExactArgs(1),
	RunE:       runTodoStart,
}

var todoShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show todo details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoShow,
}

var todoContextCmd = &cobra.Command{
	Use: "context [id]",
	// `todo review-context` was a compatibility alias, removed because the name
	// implied it advanced review state when it only ever took a read-only snapshot.
	SuggestFor: []string{"review-context"},
	Short:      "Build a read-only Todo, session, and Git context",
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

var todoEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a todo's metadata and work state",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoEdit,
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
	Short: "Move todos out of the working set",
	Long: `Move todos of any lifecycle state out of the working set.

Archived todos keep their row, their ID, and their markdown card: dependencies
and progress notes may still refer to them, and the ID is never reused. They no
longer appear in listings, the dashboard, or matching. Use
` + "`atm todo list --status archived`" + ` to read them and ` + "`atm todo restore`" + ` to bring
one back.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTodoArchive,
}

var todoRestoreCmd = &cobra.Command{
	Use:   "restore <id>...",
	Short: "Restore archived todos to the working set",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTodoRestore,
}
