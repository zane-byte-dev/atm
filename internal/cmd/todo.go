package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	todoListPriorityFlag   string
	todoStatusFlag         string
	todoProjectFlag        string
	todoListLaneFlag       string
	todoListQueryFlag      string
	todoListCreatorFlag    string
	todoListLimitFlag      int
	todoListOffsetFlag     int
	todoAddPriorityFlag    string
	todoAddProjectFlag     string
	todoAddLaneFlag        string
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
	todoEditLaneFlag       string
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
	todoFocusLaneFlag      string
	todoWaitLaneFlag       string
	todoWaitWakeFlag       string
	todoWaitReviewAtFlag   string
	todoMaintainLaneFlag   string
	todoMaintainLimitFlag  int
	todoContextCWD         string
	todoSubmitReasonFlag   string
)

func init() {
	todoListCmd.Flags().StringVar(&todoListPriorityFlag, "priority", "", "filter by priority: P0, P1, P2")
	todoListCmd.Flags().StringVar(&todoStatusFlag, "status", "", "filter by status: open, in_progress, waiting, review, blocked, done, dropped, archived, all (default: active)")
	todoListCmd.Flags().StringVar(&todoProjectFlag, "project", "", "filter by project name")
	todoListCmd.Flags().StringVar(&todoListLaneFlag, "lane", "", "filter by work lane (for example: work, personal)")
	todoListCmd.Flags().StringVar(&todoListQueryFlag, "query", "", "filter by id, title, description, project, source, or todo document")
	todoListCmd.Flags().StringVar(&todoListCreatorFlag, "creator", "", "filter by creator: "+strings.Join(store.TodoCreatorVocabulary, ", "))
	todoListCmd.Flags().IntVar(&todoListLimitFlag, "limit", 0, "maximum number of todos (0 means all)")
	todoListCmd.Flags().IntVar(&todoListOffsetFlag, "offset", 0, "number of todos to skip")

	todoAddCmd.Flags().StringVar(&todoAddPriorityFlag, "priority", "P1", "priority: P0, P1, P2")
	todoAddCmd.Flags().StringVar(&todoAddProjectFlag, "project", "", "project name")
	todoAddCmd.Flags().StringVar(&todoAddLaneFlag, "lane", "", "work lane (for example: work, personal)")
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
	todoEditCmd.Flags().StringVar(&todoEditLaneFlag, "lane", "", "new work lane")
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

	todoFocusCmd.Flags().StringVar(&todoFocusLaneFlag, "lane", "", "work lane")
	todoWaitCmd.Flags().StringVar(&todoWaitLaneFlag, "lane", "", "work lane")
	todoWaitCmd.Flags().StringVar(&todoWaitWakeFlag, "wake", "", "condition that should wake the todo")
	todoWaitCmd.Flags().StringVar(&todoWaitReviewAtFlag, "review-at", "", "next review date (YYYY-MM-DD)")
	todoMaintainCmd.Flags().StringVar(&todoMaintainLaneFlag, "lane", "", "work lane")
	todoMaintainCmd.Flags().IntVar(&todoMaintainLimitFlag, "limit", 3, "maximum items in this maintenance batch")
	for _, contextCmd := range []*cobra.Command{todoContextCmd, todoReviewContextCmd} {
		contextCmd.Flags().StringVar(&todoContextCWD, "cwd", "", "Git worktree to inspect (required when active todo bindings use multiple worktrees)")
	}

	todoCmd.AddCommand(todoArchiveCmd, todoUnarchiveCmd, todoListCmd, todoAddCmd, todoStartCmd, todoSubmitCmd, todoDoneCmd, todoDropCmd, todoShowCmd, todoContextCmd, todoReviewContextCmd, todoPromptCmd, todoEditCmd, todoMoveCmd, todoLogCmd, todoDocCmd, todoDeleteCmd, todoCaptureCmd, todoFocusCmd, todoWaitCmd, todoMaintainCmd)
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

func normalizeLane(value string) (string, error) {
	lane := strings.ToLower(strings.TrimSpace(value))
	if strings.ContainsAny(lane, " \t\r\n") {
		return "", fmt.Errorf("lane must not contain whitespace: %q", value)
	}
	return lane, nil
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

func applyLane(t *store.Todo, laneFlag string, changed bool) error {
	if !changed {
		return nil
	}
	lane, err := normalizeLane(laneFlag)
	if err != nil {
		return err
	}
	t.Lane = lane
	return nil
}

func runTodoDelete(cmd *cobra.Command, args []string) error {
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}

	if todoDeleteProjectFlag != "" {
		deleted := 0
		for _, t := range tf.Items {
			if t.Project == todoDeleteProjectFlag {
				deleted++
			}
		}
		if deleted == 0 {
			return fmt.Errorf("no todos found for project: %s", todoDeleteProjectFlag)
		}
		confirmed, err := confirmDestructive(cmd, todoDeleteYesFlag, fmt.Sprintf("Permanently delete %d todos from project %s?", deleted, todoDeleteProjectFlag))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
		deletedIDs := []string{}
		err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
			keep := []store.Todo{}
			for _, todo := range transaction.Todos().Items {
				if todo.Project == todoDeleteProjectFlag {
					// Comments and session bindings go with the todo via
					// ON DELETE CASCADE.
					deletedIDs = append(deletedIDs, todo.ID)
				} else {
					keep = append(keep, todo)
				}
			}
			transaction.Todos().Items = keep
			return nil
		})
		if err != nil {
			return err
		}
		for _, id := range deletedIDs {
			if store.TodoDocExists(id) {
				_ = os.Remove(store.TodoDocPath(id))
			}
		}
		fmt.Printf("Deleted %d todos from project %s\n", deleted, todoDeleteProjectFlag)
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("provide a todo ID or use --project to batch delete")
	}

	id := args[0]
	found := false
	for _, t := range tf.Items {
		if t.ID == id {
			found = true
		}
	}
	if !found {
		return store.TodoNotFoundError(tf, id)
	}
	confirmed, err := confirmDestructive(cmd, todoDeleteYesFlag, fmt.Sprintf("Permanently delete todo %s?", id))
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
		return nil
	}
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		var updated []store.Todo
		for _, todo := range transaction.Todos().Items {
			if todo.ID != id {
				updated = append(updated, todo)
			}
		}
		// Comments and session bindings go with the todo via ON DELETE CASCADE.
		transaction.Todos().Items = updated
		return nil
	})
	if err != nil {
		return err
	}
	if store.TodoDocExists(id) {
		_ = os.Remove(store.TodoDocPath(id))
	}
	fmt.Printf("Deleted %s\n", id)
	return nil
}

func todoMatchesQuery(todo store.Todo, rawQuery string) bool {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	if query == "" {
		return true
	}
	parts := []string{todo.ID, todo.Title, todo.Description, todo.Project, todo.Source}
	if document, err := store.ReadTodoDoc(todo.ID); err == nil {
		parts = append(parts, document)
	}
	haystack := strings.ToLower(strings.Join(parts, "\n"))
	for _, term := range strings.Fields(query) {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func runTodoList(cmd *cobra.Command, args []string) error {
	if todoListLaneFlag != "" {
		lane, err := normalizeLane(todoListLaneFlag)
		if err != nil {
			return err
		}
		todoListLaneFlag = lane
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	// Normalized up front so `--creator 收集` and `--creator collection` select
	// the same rows the stored token does, and so a typo is rejected instead of
	// quietly matching nothing.
	creator, err := store.NormalizeTodoCreator(todoListCreatorFlag)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(todoStatusFlag)
	activeOnly := status == ""
	if status == "all" {
		status = ""
		activeOnly = false
	} else if status != "" && status != "archived" && status != store.TodoStatusDone && status != store.TodoStatusDropped {
		if err := validateWorkStatus(status); err != nil {
			return err
		}
	}

	if status == "archived" {
		return listArchived(creator)
	}

	var filtered []store.Todo
	for _, t := range tf.Items {
		if activeOnly && !store.TodoIsActive(t) {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		if todoListPriorityFlag != "" && t.Priority != todoListPriorityFlag {
			continue
		}
		if todoProjectFlag != "" && t.Project != todoProjectFlag {
			continue
		}
		if todoListLaneFlag != "" && t.Lane != todoListLaneFlag {
			continue
		}
		if creator != "" && t.Creator != creator {
			continue
		}
		if !todoMatchesQuery(t, todoListQueryFlag) {
			continue
		}
		filtered = append(filtered, t)
	}
	filtered, err = paginate(filtered, todoListOffsetFlag, todoListLimitFlag)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(filtered)
		return nil
	}

	if len(filtered) == 0 {
		fmt.Println("No todos found.")
		return nil
	}

	// The creator column shows the stored token, not the display name: it is the
	// vocabulary `--creator` takes, and it keeps the column ASCII-width so the
	// table stays aligned.
	fmt.Printf("  %-6s %-4s %-12s %-12s %-12s %-10s %-16s %s\n", "ID", "Pri", "Status", "Lane", "Created", "Creator", "Project", "Title")
	fmt.Printf("  %-6s %-4s %-12s %-12s %-12s %-10s %-16s %s\n",
		strings.Repeat("-", 6), strings.Repeat("-", 4), strings.Repeat("-", 12),
		strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 10),
		strings.Repeat("-", 16), strings.Repeat("-", 30))
	for _, t := range filtered {
		id := t.ID
		if store.TodoDocExists(t.ID) {
			id += "*"
		}
		fmt.Printf("  %-6s %-4s %-12s %-12s %-12s %-10s %-16s %s\n", id, t.Priority, t.Status, t.Lane, t.Created,
			emptyAs(t.Creator, "-"), t.Project, t.Title)
	}
	return nil
}

func runTodoArchive(cmd *cobra.Command, args []string) error {
	return runTodoArchiveMove(args, "archived", "Archived",
		func(transaction *workapp.Transaction, ids []string) ([]string, error) {
			return transaction.ArchiveTodos(ids)
		})
}

func runTodoUnarchive(cmd *cobra.Command, args []string) error {
	return runTodoArchiveMove(args, "unarchived", "Unarchived",
		func(transaction *workapp.Transaction, ids []string) ([]string, error) {
			return transaction.UnarchiveTodos(ids)
		})
}

// runTodoArchiveMove is archive and unarchive, which differ only in the verb:
// both move a set of ids across the archive boundary and report the ones that
// actually moved.
func runTodoArchiveMove(args []string, jsonKey, verb string,
	move func(*workapp.Transaction, []string) ([]string, error)) error {
	var moved []string
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		var err error
		moved, err = move(transaction, uniqueStrings(args))
		return err
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{jsonKey: moved})
		return nil
	}
	fmt.Printf("%s %s\n", verb, strings.Join(moved, ", "))
	return nil
}

func listArchived(creator string) error {
	archived, err := store.LoadArchivedTodos()
	if err != nil {
		return err
	}

	var filtered []store.ArchivedTodo
	for _, t := range archived {
		if todoListPriorityFlag != "" && t.Priority != todoListPriorityFlag {
			continue
		}
		if todoProjectFlag != "" && t.Project != todoProjectFlag {
			continue
		}
		if todoListLaneFlag != "" && t.Lane != todoListLaneFlag {
			continue
		}
		if creator != "" && t.Creator != creator {
			continue
		}
		if !todoMatchesQuery(t.Todo, todoListQueryFlag) {
			continue
		}
		filtered = append(filtered, t)
	}
	filtered, err = paginate(filtered, todoListOffsetFlag, todoListLimitFlag)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(filtered)
		return nil
	}
	if len(filtered) == 0 {
		fmt.Println("No archived todos.")
		return nil
	}

	fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n", "ID", "Pri", "Status", "Created", "Archived", "Creator", "Project", "Title")
	fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n",
		strings.Repeat("-", 6), strings.Repeat("-", 4), strings.Repeat("-", 8),
		strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 10),
		strings.Repeat("-", 16), strings.Repeat("-", 30))
	for _, t := range filtered {
		archivedOn := time.Unix(t.ArchivedAt, 0).In(config.Loc).Format("2006-01-02")
		fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n",
			t.ID, t.Priority, t.Status, t.Created, archivedOn, emptyAs(t.Creator, "-"), t.Project, t.Title)
	}
	return nil
}

type batchInput struct {
	Project  string      `yaml:"project" json:"project"`
	Source   string      `yaml:"source" json:"source"`
	Creator  string      `yaml:"creator" json:"creator"`
	Priority string      `yaml:"priority" json:"priority"`
	Lane     string      `yaml:"lane" json:"lane"`
	Status   string      `yaml:"status" json:"status"`
	Items    []batchItem `yaml:"items" json:"items"`
}

type batchItem struct {
	Title         string `yaml:"title" json:"title"`
	Desc          string `yaml:"desc" json:"desc"`
	Priority      string `yaml:"priority" json:"priority"`
	Project       string `yaml:"project" json:"project"`
	Source        string `yaml:"source" json:"source"`
	Creator       string `yaml:"creator" json:"creator"`
	Lane          string `yaml:"lane" json:"lane"`
	Status        string `yaml:"status" json:"status"`
	WakeCondition string `yaml:"wake" json:"wake"`
	ReviewAt      string `yaml:"review_at" json:"review_at"`
}

func runTodoAdd(cmd *cobra.Command, args []string) error {
	if todoBatchFlag {
		if todoDescFileFlag != "" {
			return fmt.Errorf("--batch and --desc-file cannot be used together because both may read stdin")
		}
		return runTodoBatchAdd()
	}

	if len(args) == 0 {
		return fmt.Errorf("requires at least 1 arg(s), use --batch for batch input")
	}

	title := strings.Join(args, " ")
	if len([]rune(title)) < 8 && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Warning: title is very short (%d chars), consider being more descriptive\n", len([]rune(title)))
	}

	lane, err := normalizeLane(todoAddLaneFlag)
	if err != nil {
		return err
	}
	status := todoAddStatusFlag
	if err := validateWorkStatus(status); err != nil {
		return err
	}
	if err := validateReviewAt(todoAddReviewAtFlag); err != nil {
		return err
	}
	if status == store.TodoStatusWaiting && todoAddWakeFlag == "" && todoAddReviewAtFlag == "" {
		return fmt.Errorf("waiting todos require --wake or --review-at")
	}

	source := todoSourceFlag
	if source == "" {
		source = todoSourceFromSession()
	}

	creator, err := resolveTodoCreator(todoAddCreatorFlag)
	if err != nil {
		return err
	}

	description, err := todoAddDescription(cmd)
	if err != nil {
		return err
	}

	var tf *store.TodoFile
	var t store.Todo
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		t = store.Todo{
			ID:            store.NextTodoID(tf),
			Title:         title,
			Priority:      todoAddPriorityFlag,
			Status:        status,
			Project:       todoAddProjectFlag,
			Lane:          lane,
			WakeCondition: todoAddWakeFlag,
			ReviewAt:      todoAddReviewAtFlag,
			Created:       store.Today(),
			Source:        source,
			Creator:       creator,
			Description:   description,
			OnDone:        todoOnDoneFlag,
		}
		if t.Status != store.TodoStatusWaiting {
			t.WakeCondition = ""
			t.ReviewAt = ""
		}
		tf.Items = append(tf.Items, t)
		return nil
	})
	if err != nil {
		return err
	}
	if err := ensureTodoDocs(tf, t.ID); err != nil {
		return err
	}

	// Humans need to know when work appears — agents often create via --json.
	notifyTodoEvent(&t, notifyEventCreated)
	if t.Status == store.TodoStatusReview {
		notifyTodoEvent(&t, notifyEventReview)
	}

	if jsonOutput {
		output.JSON(t)
		return nil
	}
	fmt.Println(t.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Created %s: %s\n", t.ID, t.Title)
	return nil
}

// readBodyFlagOrFile resolves a body that may be given inline or read from a
// file, where "-" means stdin. Every parameter carrying multiline prose needs the
// file door: a requirement or an analysis note routinely contains backticks, `$`,
// braces and quotes, and pushing it through a shell argument makes correctness
// depend on the caller quoting a heredoc properly. Getting that wrong fails
// silently — command substitution runs, `$VAR` becomes empty, and the write still
// reports success, so the damage is only visible by reading the text back.
//
// name is the inline flag's name; the file flag is assumed to be name + "-file".
func readBodyFlagOrFile(cmd *cobra.Command, name, inline, path string) (string, error) {
	if path == "" {
		return inline, nil
	}
	if inline != "" {
		return "", fmt.Errorf("--%s and --%s-file cannot be used together", name, name)
	}
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("reading --%s-file from %s: %w", name, path, err)
	}
	return string(data), nil
}

func todoAddDescription(cmd *cobra.Command) (string, error) {
	description, err := readBodyFlagOrFile(cmd, "desc", todoDescFlag, todoDescFileFlag)
	if err != nil {
		return "", err
	}
	return description, store.ValidateTodoDescription(description)
}

func runTodoBatchAdd() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("no input from stdin")
	}

	var batch batchInput
	if err := yaml.Unmarshal(data, &batch); err != nil {
		return fmt.Errorf("parsing batch input: %w", err)
	}
	if len(batch.Items) == 0 {
		return fmt.Errorf("no items in batch input")
	}

	// Defaults: CLI flags > batch-level > hardcoded
	defaultPriority := "P1"
	if batch.Priority != "" {
		defaultPriority = batch.Priority
	}
	if todoAddPriorityFlag != "P1" {
		defaultPriority = todoAddPriorityFlag
	}

	defaultProject := batch.Project
	if todoAddProjectFlag != "" {
		defaultProject = todoAddProjectFlag
	}
	defaultLane := batch.Lane
	if todoAddLaneFlag != "" {
		defaultLane = todoAddLaneFlag
	}
	defaultStatus := batch.Status
	if defaultStatus == "" {
		defaultStatus = store.TodoStatusOpen
	}
	if todoAddStatusFlag != store.TodoStatusOpen {
		defaultStatus = todoAddStatusFlag
	}

	defaultSource := batch.Source
	if todoSourceFlag != "" {
		defaultSource = todoSourceFlag
	}
	if defaultSource == "" {
		defaultSource = todoSourceFromSession()
	}

	// One batch is filed by one caller, so the creator is resolved once. Items
	// may still name their own, which is what a batch assembled from several
	// intake paths needs.
	defaultCreator, err := resolveTodoCreator(emptyAs(todoAddCreatorFlag, batch.Creator))
	if err != nil {
		return err
	}

	var tf *store.TodoFile
	var added []store.Todo
	var changedDocIDs []string
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		for _, item := range batch.Items {
			if item.Title == "" {
				continue
			}
			priority := defaultPriority
			if item.Priority != "" {
				priority = item.Priority
			}
			project := defaultProject
			if item.Project != "" {
				project = item.Project
			}
			source := defaultSource
			if item.Source != "" {
				source = item.Source
			}
			creator := defaultCreator
			if item.Creator != "" {
				normalized, err := store.NormalizeTodoCreator(item.Creator)
				if err != nil {
					return fmt.Errorf("item %q: %w", item.Title, err)
				}
				creator = normalized
			}
			lane := defaultLane
			if item.Lane != "" {
				lane = item.Lane
			}
			var err error
			lane, err = normalizeLane(lane)
			if err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}
			status := defaultStatus
			if item.Status != "" {
				status = item.Status
			}
			if err := validateWorkStatus(status); err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}
			if err := validateReviewAt(item.ReviewAt); err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}
			if err := store.ValidateTodoDescription(item.Desc); err != nil {
				return fmt.Errorf("item %q: %w", item.Title, err)
			}

			t := store.Todo{
				ID:            store.NextTodoID(tf),
				Title:         item.Title,
				Description:   item.Desc,
				Priority:      priority,
				Status:        status,
				Project:       project,
				Lane:          lane,
				WakeCondition: item.WakeCondition,
				ReviewAt:      item.ReviewAt,
				Created:       store.Today(),
				Source:        source,
				Creator:       creator,
			}
			if t.Status == store.TodoStatusWaiting && t.WakeCondition == "" && t.ReviewAt == "" {
				return fmt.Errorf("item %q: waiting todos require wake or review_at", item.Title)
			}
			if t.Status != store.TodoStatusWaiting {
				t.WakeCondition = ""
				t.ReviewAt = ""
			}
			tf.Items = append(tf.Items, t)
			added = append(added, t)
			changedDocIDs = append(changedDocIDs, t.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := ensureTodoDocs(tf, changedDocIDs...); err != nil {
		return err
	}

	for i := range added {
		notifyTodoEvent(&added[i], notifyEventCreated)
		if added[i].Status == store.TodoStatusReview {
			notifyTodoEvent(&added[i], notifyEventReview)
		}
	}

	if jsonOutput {
		output.JSON(added)
		return nil
	}
	for _, t := range added {
		fmt.Printf("Added %s: %s\n", t.ID, t.Title)
	}
	fmt.Fprintf(os.Stderr, "(%d items added)\n", len(added))
	return nil
}

func runTodoEdit(cmd *cobra.Command, args []string) error {
	// Resolved before the mutation opens, so a missing file or an unreadable
	// stdin fails without having touched the todo.
	editedDescription, err := readBodyFlagOrFile(cmd, "desc", todoEditDescFlag, todoEditDescFileFlag)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("desc") || cmd.Flags().Changed("desc-file") {
		if err := store.ValidateTodoDescription(editedDescription); err != nil {
			return err
		}
	}
	var enteredReview bool
	tf, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, transaction *workapp.Transaction) error {
		changed := false
		prevStatus := t.Status
		if cmd.Flags().Changed("title") {
			t.Title = todoEditTitleFlag
			changed = true
		}
		if cmd.Flags().Changed("desc") || cmd.Flags().Changed("desc-file") {
			t.Description = editedDescription
			changed = true
		}
		if cmd.Flags().Changed("priority") {
			t.Priority = todoEditPriorityFlag
			changed = true
		}
		if cmd.Flags().Changed("project") {
			t.Project = todoEditProjectFlag
			changed = true
		}
		if cmd.Flags().Changed("source") {
			t.Source = todoEditSourceFlag
			changed = true
		}
		if cmd.Flags().Changed("lane") {
			if err := applyLane(t, todoEditLaneFlag, true); err != nil {
				return err
			}
			changed = true
		}
		if cmd.Flags().Changed("status") {
			if err := validateWorkStatus(todoEditStatusFlag); err != nil {
				return err
			}
			t.Status = todoEditStatusFlag
			changed = true
		}
		if cmd.Flags().Changed("wake") {
			t.WakeCondition = todoEditWakeFlag
			changed = true
		}
		if cmd.Flags().Changed("review-at") {
			if err := validateReviewAt(todoEditReviewAtFlag); err != nil {
				return err
			}
			t.ReviewAt = todoEditReviewAtFlag
			changed = true
		}
		if !changed {
			return fmt.Errorf("nothing to update, use --title, --desc, --priority, --project, --source, --lane, --status, --wake, or --review-at")
		}
		if t.Status == store.TodoStatusWaiting && t.WakeCondition == "" && t.ReviewAt == "" && len(t.DependsOn) == 0 {
			return fmt.Errorf("waiting todos require --wake or --review-at")
		}
		if t.Status != store.TodoStatusWaiting {
			t.WakeCondition = ""
			t.ReviewAt = ""
		}
		if cmd.Flags().Changed("status") && t.Status != store.TodoStatusInProgress {
			if _, err := transaction.UnbindTodoSessions(t.ID, "status:"+t.Status); err != nil {
				return fmt.Errorf("unbind todo sessions before status change: %w", err)
			}
		}
		enteredReview = prevStatus != store.TodoStatusReview && t.Status == store.TodoStatusReview
		return nil
	})
	if err != nil {
		return err
	}
	if enteredReview {
		notifyTodoEvent(t, notifyEventReview)
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Updated %s: %s", t.ID, t.Title))
}

func runTodoFocus(cmd *cobra.Command, args []string) error {
	tf, t, err := startTodo(args[0], todoFocusLaneFlag)
	if err != nil {
		return err
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Started %s: %s", t.ID, t.Title))
}

func runTodoWait(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	tf, t, err := mutateTodo(id, func(t *store.Todo, _ *store.TodoFile, transaction *workapp.Transaction) error {
		if !store.TodoIsActive(*t) {
			return fmt.Errorf("cannot wait todo %s with status %s", t.ID, t.Status)
		}
		if todoWaitLaneFlag != "" {
			lane, err := normalizeLane(todoWaitLaneFlag)
			if err != nil {
				return err
			}
			t.Lane = lane
		}
		if todoWaitWakeFlag != "" {
			t.WakeCondition = todoWaitWakeFlag
		}
		if todoWaitReviewAtFlag != "" {
			if err := validateReviewAt(todoWaitReviewAtFlag); err != nil {
				return err
			}
			t.ReviewAt = todoWaitReviewAtFlag
		}
		if t.WakeCondition == "" && t.ReviewAt == "" {
			return fmt.Errorf("wait requires --wake or --review-at")
		}
		t.Status = store.TodoStatusWaiting
		if _, err := transaction.UnbindTodoSessions(t.ID, "waiting"); err != nil {
			return fmt.Errorf("unbind todo sessions before waiting: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := syncExistingTodoDocs(tf, t.ID); err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(t)
		return nil
	}
	fmt.Printf("Waiting %s: %s\n", t.ID, t.Title)
	if t.WakeCondition != "" {
		fmt.Printf("  Wake:   %s\n", t.WakeCondition)
	}
	if t.ReviewAt != "" {
		fmt.Printf("  Review: %s\n", t.ReviewAt)
	}
	return nil
}

func runTodoMaintain(cmd *cobra.Command, args []string) error {
	if todoMaintainLimitFlag < 1 {
		return fmt.Errorf("maintenance limit must be at least 1")
	}
	tf, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
		if !store.TodoIsActive(*t) {
			return fmt.Errorf("cannot maintain todo %s with status %s", t.ID, t.Status)
		}
		if todoMaintainLaneFlag != "" {
			lane, err := normalizeLane(todoMaintainLaneFlag)
			if err != nil {
				return err
			}
			t.Lane = lane
		}
		store.AddTodoTag(t, store.TodoTagMaintenance)
		t.MaintenanceLimit = todoMaintainLimitFlag
		return nil
	})
	if err != nil {
		return err
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Maintaining %s (limit %d): %s", t.ID, t.MaintenanceLimit, t.Title))
}

func runTodoMove(cmd *cobra.Command, args []string) error {
	var old string
	tf, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
		old = t.Project
		t.Project = todoMoveProjectFlag
		return nil
	})
	if err != nil {
		return err
	}
	if err := syncExistingTodoDocs(tf, t.ID); err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(t)
		return nil
	}
	if old != "" {
		fmt.Printf("Moved %s: %s → %s\n", t.ID, old, t.Project)
	} else {
		fmt.Printf("Moved %s → %s\n", t.ID, t.Project)
	}
	return nil
}

func runTodoStart(cmd *cobra.Command, args []string) error {
	tf, t, err := startTodo(args[0], "")
	if err != nil {
		return err
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Started %s: %s", t.ID, t.Title))
}

// startTodo backs both `todo start` and its deprecated alias `todo focus`, which
// differed only in accepting --lane. An empty laneFlag leaves the lane alone.
func startTodo(id, laneFlag string) (*store.TodoFile, *store.Todo, error) {
	return mutateTodo(id, func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
		if laneFlag != "" {
			lane, err := normalizeLane(laneFlag)
			if err != nil {
				return err
			}
			t.Lane = lane
		}
		// Starting a closed todo is an explicit reopen. Its previous lifecycle
		// timestamps must not leak into the new run: otherwise session linking
		// spans the completed attempt and duration can end before the new start.
		// The todo document keeps the historical completion log when one exists.
		if !store.TodoIsActive(*t) {
			now := time.Now().In(config.Loc).Unix()
			t.StartTS = &now
			t.DoneTS = nil
			t.Closed = nil
			t.ClosedReason = nil
		} else if t.StartTS == nil {
			now := time.Now().In(config.Loc).Unix()
			t.StartTS = &now
		}
		t.Status = store.TodoStatusInProgress
		t.WakeCondition = ""
		t.ReviewAt = ""
		return nil
	})
}

// runTodoPrompt writes the line a human pastes into a fresh agent session.
//
// It deliberately hands over a pointer rather than the task itself. The agent
// reads the requirement from the database on its own, so what it works from is
// always current; a copied snapshot would start drifting the moment the todo
// changed. The canonical ID is spelled out because todo lookups are exact
// matches -- `#101` and `101` resolve to nothing -- and `todo doc` is named
// explicitly because `todo show` prints only the one-line description, not the
// requirement body.
func runTodoPrompt(cmd *cobra.Command, args []string) error {
	_, t, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}

	prompt := buildTodoPrompt(t)

	if todoPromptCopyFlag {
		if err := copyToClipboard(prompt); err != nil {
			return err
		}
	}

	if jsonOutput {
		output.JSON(map[string]any{"prompt": prompt})
		return nil
	}

	fmt.Println(prompt)
	if todoPromptCopyFlag {
		fmt.Fprintln(os.Stderr, "Copied to clipboard.")
	}
	return nil
}

func buildTodoPrompt(t *store.Todo) string {
	return fmt.Sprintf(
		"使用 atm 实现任务 %s：%s\n先跑 atm todo doc %s 拿需求正文，再 atm session bind %s。",
		t.ID, t.Title, t.ID, t.ID,
	)
}

func runTodoSubmit(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	message := "[submit]"
	if todoSubmitReasonFlag != "" {
		message += " " + todoSubmitReasonFlag
	}
	var alreadyReview bool
	tf, t, err := mutateTodo(id, func(t *store.Todo, tf *store.TodoFile, transaction *workapp.Transaction) error {
		if t.Status == store.TodoStatusReview {
			alreadyReview = true
			return nil
		}
		if t.Status != store.TodoStatusInProgress {
			return fmt.Errorf("cannot submit todo %s with status %s", t.ID, t.Status)
		}
		if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
			return err
		}
		if err := validateTodoLogReferences(tf, message); err != nil {
			return err
		}
		t.Status = store.TodoStatusReview
		t.WakeCondition = ""
		t.ReviewAt = ""
		if _, err := transaction.UnbindTodoSessions(t.ID, "submit:review"); err != nil {
			return fmt.Errorf("unbind todo sessions before submit: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if alreadyReview {
		if jsonOutput {
			output.JSON(t)
		} else {
			fmt.Printf("Submitted %s already review: %s\n", t.ID, t.Title)
		}
		return nil
	}
	if _, err := store.AppendTodoLog(t, message, ""); err != nil {
		return err
	}
	// Submit is the human gate: agent finished, person needs to accept.
	notifyTodoEvent(t, notifyEventReview)
	return finishTodoMutation(tf, t, fmt.Sprintf("Submitted %s for confirmation: %s", t.ID, t.Title))
}

func runTodoDone(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	return closeTodo(id, "done")
}

func runTodoDrop(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	return closeTodo(id, "dropped")
}

func closeTodo(id, status string) error {
	var alreadyClosed bool
	var awakened []store.TodoWakeEvent
	tf, t, err := mutateTodo(id, func(t *store.Todo, tf *store.TodoFile, transaction *workapp.Transaction) error {
		if t.Status == status {
			alreadyClosed = true
			return nil
		}
		if todoReasonFlag != "" {
			message := fmt.Sprintf("[%s] %s", status, todoReasonFlag)
			if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
				return err
			}
			if err := validateTodoLogReferences(tf, message); err != nil {
				return err
			}
		}
		t.Status = status
		today := store.Today()
		t.Closed = &today
		now := time.Now().In(config.Loc).Unix()
		t.DoneTS = &now
		if todoReasonFlag != "" {
			t.ClosedReason = &todoReasonFlag
		}
		if status == "done" {
			awakened = store.ReconcileTodoDependencies(tf)
		}
		if _, err := transaction.UnbindTodoSessions(t.ID, status); err != nil {
			return fmt.Errorf("unbind todo sessions before close: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if alreadyClosed {
		if jsonOutput {
			output.JSON(t)
		} else {
			fmt.Printf("%s %s already %s: %s\n", status, t.ID, status, t.Title)
		}
		return nil
	}
	appendTodoWakeLogs(tf, awakened)
	syncIDs := []string{t.ID}
	for _, event := range awakened {
		syncIDs = append(syncIDs, event.TodoID)
	}
	if err := syncExistingTodoDocs(tf, syncIDs...); err != nil {
		return err
	}
	if store.TodoDocExists(t.ID) {
		if todoReasonFlag != "" {
			if _, err := store.AppendTodoLog(t, fmt.Sprintf("[%s] %s", status, todoReasonFlag), ""); err != nil {
				return fmt.Errorf("append todo log: %w", err)
			}
		}
	}

	// Notify even under --json: agents close todos with machine output, but the
	// banner is for the human watching the desk.
	event := notifyEventDone
	if status == store.TodoStatusDropped {
		event = notifyEventDropped
	}
	notifyTodoEvent(t, event)

	if t.OnDone != "" && status == "done" {
		fmt.Fprintf(os.Stderr, "on-done: %s\n", t.OnDone)
		cmd := exec.Command("sh", "-c", t.OnDone)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Start()
	}

	if jsonOutput {
		output.JSON(t)
		for _, event := range awakened {
			fmt.Fprintf(os.Stderr, "awakened %s: %s\n", event.TodoID, event.Reason)
		}
		return nil
	}
	fmt.Printf("%s %s: %s\n", status, t.ID, t.Title)
	for _, event := range awakened {
		fmt.Printf("awakened %s: %s\n", event.TodoID, event.Reason)
	}
	return nil
}

// Human-facing lifecycle events. Start/edit/start-work are noise; these are not.
const (
	notifyEventCreated = "created"
	notifyEventReview  = "review"
	notifyEventDone    = "done"
	notifyEventDropped = "dropped"
)

// notifyCopy is the pure title/subtitle/body for a human local notification.
// Extracted so tests can assert copy without spawning osascript.
func notifyCopy(t *store.Todo, event string) (title, subtitle, body string) {
	title = "ATM"
	if t.Project != "" {
		title = fmt.Sprintf("ATM · %s", t.Project)
	}
	switch event {
	case notifyEventCreated:
		subtitle = fmt.Sprintf("%s 新建", t.ID)
	case notifyEventReview:
		subtitle = fmt.Sprintf("%s 待验收", t.ID)
	case notifyEventDone:
		subtitle = fmt.Sprintf("%s 已完成", t.ID)
	case notifyEventDropped:
		subtitle = fmt.Sprintf("%s 已放弃", t.ID)
	default:
		subtitle = fmt.Sprintf("%s %s", t.ID, event)
	}
	body = t.Title
	if event == notifyEventDone && t.StartTS != nil && t.DoneTS != nil {
		dur := time.Duration(*t.DoneTS-*t.StartTS) * time.Second
		body = fmt.Sprintf("%s (%s)", t.Title, fmtDuration(dur))
	}
	return title, subtitle, body
}

func notifyTodoEvent(t *store.Todo, event string) {
	if skipLocalNotification() {
		return
	}

	title, subtitle, msg := notifyCopy(t, event)

	bin, err := os.Executable()
	if err != nil {
		bin = "atm"
	}
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		exec.Command(path,
			"-title", title,
			"-subtitle", subtitle,
			"-message", msg,
			"-execute", fmt.Sprintf("%s todo show %s", bin, t.ID),
		).Start()
		return
	}
	switch runtime.GOOS {
	case "darwin":
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification %q with title %q subtitle %q`, msg, title, subtitle),
		).Start()
	case "linux":
		if path, err := exec.LookPath("notify-send"); err == nil {
			exec.Command(path, title, subtitle+": "+msg).Start()
		}
	}
}

func skipLocalNotification() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("ATM_SKIP_LOCAL_NOTIFICATION")))
	return value == "1" || value == "true" || value == "yes"
}

func fmtDuration(d time.Duration) string {
	return formatShortDuration(int64(d.Seconds()))
}

func extractRecentLogs(content string, n int) []string {
	inProgress := false
	var logs []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## 进展") {
			inProgress = true
			continue
		}
		if inProgress && strings.HasPrefix(line, "## ") {
			break
		}
		if inProgress && strings.HasPrefix(line, "- [") {
			logs = append(logs, line)
		}
	}
	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	return logs
}

func runTodoLog(cmd *cobra.Command, args []string) error {
	id := ""
	messageArgs := args
	// With --message-file the body comes from the file, so the only positional
	// left is the optional id. A 分析 entry is the longest prose ATM accepts and
	// the one most likely to carry code, which is exactly what a shell argument
	// mangles; see readBodyFlagOrFile.
	if todoLogMessageFileFlag != "" {
		if len(args) > 1 {
			return fmt.Errorf("--message-file takes the entry text, so at most an id may be given as an argument")
		}
		id = strings.Join(args, "")
		messageArgs = nil
	} else if len(args) > 1 {
		id = args[0]
		messageArgs = args[1:]
	}
	if id == "" || id == "current" {
		var err error
		id, err = resolveCurrentTodoID()
		if err != nil {
			return err
		}
	}
	tf, t, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	msg, err := readBodyFlagOrFile(cmd, "message", strings.Join(messageArgs, " "), todoLogMessageFileFlag)
	if err != nil {
		return err
	}
	msg = strings.TrimRight(msg, "\n")
	if err := store.ValidateTodoLogMessage(msg, todoLogSectionFlag); err != nil {
		return err
	}
	if err := validateTodoLogReferences(tf, msg); err != nil {
		return err
	}
	if store.TodoDocExists(t.ID) {
		if err := store.SyncTodoDocMetadata(t); err != nil {
			return fmt.Errorf("sync todo doc: %w", err)
		}
	}
	entry, err := store.AppendTodoLog(t, msg, todoLogSectionFlag)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"success": true,
			"path":    store.TodoDocPath(t.ID),
			"entry":   strings.TrimSpace(entry),
		})
		return nil
	}
	fmt.Printf("Logged to %s: %s", t.ID, entry)
	return nil
}

func runTodoDoc(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	_, t, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	if todoDocInitFlag {
		path, err := store.InitTodoDoc(t)
		if err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{"success": true, "path": path})
			return nil
		}
		fmt.Printf("Created %s\n", path)
		return nil
	}

	// Agent handoff always starts with `todo doc`. GUI-created todos may lack a
	// card even though the structured row exists; materialize one so bind+read
	// never looks like a missing task.
	if !store.TodoDocExists(t.ID) {
		if _, err := store.EnsureTodoDoc(t); err != nil {
			return err
		}
	}

	content, err := store.ReadTodoDoc(t.ID)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"path":    store.TodoDocPath(t.ID),
			"exists":  true,
			"content": content,
		})
		return nil
	}
	fmt.Print(content)
	return nil
}

// nameBoundSessions fills in each bound session's human-readable topic. The
// binding ledger only guarantees an id, and the transcript index only carries a
// title for agents that write one — codex writes none into its rollout, so
// every codex session reached this point unnamed and the list read as a column
// of bare short ids. Codex does keep generated thread names in its own index,
// which is keyed by exactly the id the ledger stores; a session's first real
// prompt is the last resort for everything else.
func nameBoundSessions(db *sql.DB, sessions []store.TodoBoundSession) error {
	var codexTitles map[string]string
	var pending []string
	for i := range sessions {
		session := &sessions[i]
		if session.Summary != "" {
			continue
		}
		if strings.EqualFold(session.Agent, "codex") {
			if codexTitles == nil {
				codexTitles = parser.CodexThreadTitles()
			}
			if title := strings.TrimSpace(codexTitles[session.SessionID]); title != "" {
				session.Summary = title
				continue
			}
		}
		if session.IndexedID != "" {
			pending = append(pending, session.IndexedID)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Agents inject plugin lists and instruction preambles as user turns, so the
	// opening prompt is rarely the first stored message.
	messages, err := store.EarliestUserMessages(db, pending, 8)
	if err != nil {
		return err
	}
	for i := range sessions {
		session := &sessions[i]
		if session.Summary != "" {
			continue
		}
		for _, message := range messages[session.IndexedID] {
			if topic := truncLine(cleanMsg(message), 120); topic != "" {
				session.Summary = topic
				break
			}
		}
	}
	return nil
}

func runTodoShow(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	_, t, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	var boundSessions []store.TodoBoundSession
	if err := withDB(true, func(db *sql.DB) error {
		var e error
		if boundSessions, e = store.FindSessionsForTodo(db, t.ID); e != nil {
			return e
		}
		return nameBoundSessions(db, boundSessions)
	}); err != nil {
		return err
	}

	docPath := store.TodoDocPath(t.ID)
	docExists := store.TodoDocExists(t.ID)
	bindings, err := store.ListTodoSessionBindings(t.ID)
	if err != nil {
		return fmt.Errorf("load todo session bindings: %w", err)
	}

	if jsonOutput {
		encodedTodo, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("encode todo: %w", err)
		}
		var out map[string]any
		if err := json.Unmarshal(encodedTodo, &out); err != nil {
			return fmt.Errorf("build todo response: %w", err)
		}
		// Keep the established nested object while exposing the identical todo
		// fields at the top level, matching `todo list --json` for scripts.
		out["todo"] = t
		out["doc_path"] = docPath
		out["doc_exists"] = docExists
		if len(bindings) > 0 {
			out["bindings"] = bindings
		}
		if len(boundSessions) > 0 {
			out["sessions"] = boundSessions
			var totalCost float64
			var totalQueries, totalTools int
			for _, s := range boundSessions {
				totalCost += s.CostUSD
				totalQueries += s.Queries
				totalTools += s.ToolCalls
			}
			out["summary"] = map[string]any{
				"sessions":   len(boundSessions),
				"queries":    totalQueries,
				"tool_calls": totalTools,
				"cost_usd":   totalCost,
			}
		}
		output.JSON(out)
		return nil
	}

	fmt.Printf("ID:       %s\n", t.ID)
	fmt.Printf("Title:    %s\n", t.Title)
	fmt.Printf("Priority: %s\n", t.Priority)
	fmt.Printf("Status:   %s\n", t.Status)
	if len(t.Tags) > 0 {
		fmt.Printf("Tags:     %s\n", strings.Join(t.Tags, ", "))
	}
	if t.Lane != "" {
		fmt.Printf("Lane:     %s\n", t.Lane)
	}
	if t.WakeCondition != "" {
		fmt.Printf("Wake:     %s\n", t.WakeCondition)
	}
	if t.ReviewAt != "" {
		fmt.Printf("Review:   %s\n", t.ReviewAt)
	}
	if t.MaintenanceLimit > 0 {
		fmt.Printf("Limit:    %d\n", t.MaintenanceLimit)
	}
	if t.Project != "" {
		fmt.Printf("Project:  %s\n", t.Project)
	}
	fmt.Printf("Created:  %s\n", t.Created)
	if t.Creator != "" {
		fmt.Printf("Creator:  %s\n", store.TodoCreatorDisplay(t.Creator))
	}
	if t.Source != "" {
		fmt.Printf("Source:   %s\n", t.Source)
	}
	if t.Description != "" {
		fmt.Printf("Desc:     %s\n", t.Description)
	}
	if len(t.Links) > 0 {
		fmt.Println("Links:")
		for _, link := range t.Links {
			label := link.Title
			if label == "" {
				label = link.URL
			}
			if link.Kind != "" {
				fmt.Printf("  [%s] %s — %s\n", link.Kind, label, link.URL)
			} else {
				fmt.Printf("  %s\n", link.URL)
			}
		}
	}
	if t.StartTS != nil {
		fmt.Printf("Started:  %s\n", time.Unix(*t.StartTS, 0).In(config.Loc).Format("2006-01-02 15:04:05"))
	}
	if t.Closed != nil {
		fmt.Printf("Closed:   %s\n", *t.Closed)
	}
	if t.DoneTS != nil {
		fmt.Printf("Finished: %s\n", time.Unix(*t.DoneTS, 0).In(config.Loc).Format("2006-01-02 15:04:05"))
	}
	if t.StartTS != nil && t.DoneTS != nil {
		dur := time.Duration(*t.DoneTS-*t.StartTS) * time.Second
		fmt.Printf("Duration: %s\n", dur.Round(time.Second))
	}
	if t.ClosedReason != nil {
		fmt.Printf("Reason:   %s\n", *t.ClosedReason)
	}
	if len(bindings) > 0 {
		fmt.Printf("\nSession Binding History (%d):\n", len(bindings))
		for _, binding := range bindings {
			state := "bound"
			if binding.UnboundAt != nil {
				state = binding.Reason
				if state == "" {
					state = "unbound"
				}
			}
			boundAt := time.Unix(binding.BoundAt, 0).In(config.Loc).Format("01-02 15:04")
			fmt.Printf("  %-8s %-7s %-10s %-11s %s\n", shortSessionID(binding.SessionID), emptyAs(binding.Agent, "agent"), state, boundAt, binding.Project)
		}
	}

	if len(boundSessions) > 0 {
		fmt.Printf("\nBound Sessions (%d):\n", len(boundSessions))
		var totalCost float64
		var totalQueries, totalTools int
		for _, s := range boundSessions {
			summary := s.Summary
			if summary == "" {
				if s.Indexed {
					summary = "(untitled session)"
				} else {
					summary = "session details not indexed"
				}
			}
			bindingLabel := "bound"
			if s.UnboundAt != nil {
				bindingLabel = emptyAs(s.Reason, "unbound")
			}
			if s.BindingCount > 1 {
				bindingLabel += fmt.Sprintf(" x%d", s.BindingCount)
			}
			fmt.Printf("  %s  %-8s %-16s Q:%-3d Tools:%-4d $%.4f  %s\n",
				s.ShortID, s.Agent, bindingLabel, s.Queries, s.ToolCalls, s.CostUSD, summary)
			totalCost += s.CostUSD
			totalQueries += s.Queries
			totalTools += s.ToolCalls
		}
		fmt.Printf("  %s\n", strings.Repeat("-", 60))
		fmt.Printf("  Total: %d sessions, %d queries, %d tool calls, $%.4f\n",
			len(boundSessions), totalQueries, totalTools, totalCost)
	}

	if docExists {
		fmt.Printf("\nDoc:      %s\n", docPath)
		if content, err := store.ReadTodoDoc(t.ID); err == nil {
			if logs := extractRecentLogs(content, 5); len(logs) > 0 {
				fmt.Println("\nRecent Progress:")
				for _, l := range logs {
					fmt.Printf("  %s\n", l)
				}
			}
		}
	}
	return nil
}

var todoCaptureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture todos from Claude TodoWrite hook",
	Long:  "Read TodoWrite JSON from $TOOL_INPUT (set by Claude Code hook) or stdin, deduplicate against existing open todos, and create new ATM todos.",
	RunE:  runTodoCapture,
}

type todoWriteInput struct {
	Todos []todoWriteItem `json:"todos"`
}

type todoWriteItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

func runTodoCapture(cmd *cobra.Command, args []string) error {
	var data []byte
	if toolInput := os.Getenv("TOOL_INPUT"); toolInput != "" {
		data = []byte(toolInput)
	} else {
		var err error
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
	}

	if len(data) == 0 {
		return nil
	}

	var input todoWriteInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("parsing TodoWrite input: %w", err)
	}

	var candidates []todoWriteItem
	for _, item := range input.Todos {
		if item.Status != "completed" && item.Content != "" {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	project := todoCaptureProjectFlag
	if project == "" {
		if cwd, err := os.Getwd(); err == nil {
			project = filepath.Base(cwd)
		}
	}

	source := todoSourceFromSession()
	// This command only runs from Claude's TodoWrite hook, so the creator is
	// known even when the hook process carries no session ID to detect it from.
	creator := todoCreatorFromEnvironment()
	if creator == store.TodoCreatorMe {
		creator = "claude"
	}

	var tf *store.TodoFile
	var added []store.Todo
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		tf = transaction.Todos()
		existing := make(map[string]bool)
		for _, todo := range tf.Items {
			if store.TodoIsActive(todo) && todo.Project == project {
				existing[todo.Title] = true
			}
		}
		for _, item := range candidates {
			if existing[item.Content] {
				continue
			}
			t := store.Todo{
				ID:       store.NextTodoID(tf),
				Title:    item.Content,
				Priority: "P1",
				Status:   store.TodoStatusOpen,
				Project:  project,
				Created:  store.Today(),
				Source:   source,
				Creator:  creator,
			}
			tf.Items = append(tf.Items, t)
			added = append(added, t)
			existing[item.Content] = true
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(added) == 0 {
		return nil
	}

	if jsonOutput {
		output.JSON(added)
		return nil
	}
	for _, t := range added {
		fmt.Printf("Captured %s: %s\n", t.ID, t.Title)
	}
	return nil
}
