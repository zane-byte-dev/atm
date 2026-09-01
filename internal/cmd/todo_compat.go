package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

// Compatibility entry points for the spellings that existed before the
// lifecycle collapsed to open / in_progress / review / done and the three
// disposal states became one archive layer.
//
// They are Hidden rather than deleted, and the distinction is deliberate:
// hiding keeps a stale skill file, a copied note or a shell history entry
// working, while removing them from `atm todo --help` stops the help text from
// advertising a vocabulary the UI no longer uses. Nothing in ATM calls these —
// the App moved to archive/restore — so they exist purely for what is already
// typed elsewhere.
//
// Each one is a thin alias over the canonical use case, never a second
// implementation: `wait` adds waiting presentation to in_progress work,
// `drop`/`trash` archive, and `unarchive` restores.
var (
	todoWaitWakeFlag     string
	todoWaitReviewAtFlag string
	todoWakeStatusFlag   string
	todoWakeReasonFlag   string
)

func init() {
	todoWaitCmd.Flags().StringVar(&todoWaitWakeFlag, "wake", "", "external condition that should end the wait")
	todoWaitCmd.Flags().StringVar(&todoWaitReviewAtFlag, "review-at", "", "calendar review date YYYY-MM-DD")
	// Waking clears waiting presentation; it does not move the Todo, which stays
	// in_progress. The flag survives only so an existing `--status in_progress`
	// keeps parsing.
	todoWakeCmd.Flags().StringVar(&todoWakeStatusFlag, "status", store.TodoStatusInProgress, "status after waking; only in_progress is accepted")
	todoWakeCmd.Flags().StringVar(&todoWakeReasonFlag, "reason", "", "why the todo was woken")
	todoDropCmd.Flags().StringVar(&todoReasonFlag, "reason", "", "reason recorded while archiving")

	todoCmd.AddCommand(
		todoWaitCmd, todoWakeCmd, todoReconcileCmd,
		todoDropCmd, todoTrashCmd, todoUnarchiveCmd,
	)
}

var todoWaitCmd = &cobra.Command{
	Use:    "wait [id]",
	Short:  "Deprecated: keep a wake condition or review date on in-progress work",
	Long:   "Waiting is not a lifecycle state. This records a wake condition or review date on an in_progress Todo and releases its bound sessions; use `atm todo edit --wake` or `--review-at` instead.",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE:   runTodoWait,
}

var todoWakeCmd = &cobra.Command{
	Use:    "wake <todo-id>",
	Short:  "Deprecated: clear waiting metadata and return the todo to open or review",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runTodoWake,
}

var todoReconcileCmd = &cobra.Command{
	Use:    "reconcile",
	Short:  "Deprecated: wake satisfied dependencies and audit the dependency graph",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runTodoReconcile,
}

var todoDropCmd = &cobra.Command{
	Use:    "drop [id]",
	Short:  "Deprecated: alias for `atm todo archive`",
	Long:   "Abandoning is no longer its own state. This archives the Todo, which is reversible with `atm todo restore`.",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE:   runTodoDrop,
}

var todoTrashCmd = &cobra.Command{
	Use:    "trash <id>...",
	Short:  "Deprecated: alias for `atm todo archive`",
	Hidden: true,
	Args:   cobra.MinimumNArgs(1),
	RunE:   runTodoTrash,
}

var todoUnarchiveCmd = &cobra.Command{
	Use:    "unarchive <id>...",
	Short:  "Deprecated: alias for `atm todo restore`",
	Hidden: true,
	Args:   cobra.MinimumNArgs(1),
	RunE:   runTodoUnarchive,
}

func runTodoWait(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("wait")
	id, sessionID := todoTransitionTarget(args)
	result, err := workapp.Default.Wait(cmd.Context(), call, workapp.WaitInput{
		TodoID:        id,
		SessionID:     sessionID,
		WakeCondition: todoWaitWakeFlag,
		ReviewAt:      todoWaitReviewAtFlag,
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(&result.Todo)
		return nil
	}
	// The Todo stays in_progress, so the heading says what changed rather than
	// naming a state that no longer exists.
	fmt.Printf("Waiting %s: %s\n", result.Todo.ID, result.Todo.Title)
	if result.Todo.WakeCondition != "" {
		fmt.Printf("  Wake:   %s\n", result.Todo.WakeCondition)
	}
	if result.Todo.ReviewAt != "" {
		fmt.Printf("  Review: %s\n", result.Todo.ReviewAt)
	}
	return nil
}

func runTodoWake(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("wake")
	result, err := workapp.Default.Wake(cmd.Context(), call, workapp.WakeInput{
		TodoID: args[0],
		Status: todoWakeStatusFlag,
		Reason: todoWakeReasonFlag,
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Todo)
		return nil
	}
	fmt.Printf("Awakened %s to %s: %s\n", result.Todo.ID, result.Todo.Status, result.Todo.Title)
	return nil
}

func runTodoReconcile(cmd *cobra.Command, _ []string) error {
	call := todoWorkflowCLICall("reconcile")
	result, err := workapp.Default.Reconcile(cmd.Context(), call, workapp.ReconcileInput{})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"awakened": result.Awakened, "issues": result.Issues})
		return nil
	}
	fmt.Printf("Reconciled dependencies: awakened=%d issues=%d\n", len(result.Awakened), len(result.Issues))
	for _, event := range result.Awakened {
		fmt.Printf("  wake  %-6s %s\n", event.TodoID, event.Reason)
	}
	for _, issue := range result.Issues {
		fmt.Printf("  issue %-6s %-20s %s\n", issue.TodoID, issue.Code, issue.Detail)
	}
	return nil
}

// runTodoDrop archives through the lifecycle use case rather than the retention
// one: unlike `trash`, `drop` accepts no ID and falls back to the bound
// session's Todo, and it reports the awakenings that closing a blocker causes.
func runTodoDrop(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("drop")
	id, sessionID := todoTransitionTarget(args)
	result, err := workapp.Default.Drop(cmd.Context(), call, workapp.CloseInput{
		TodoID: id, SessionID: sessionID, Reason: todoReasonFlag,
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Todo)
		for _, event := range result.Awakened {
			fmt.Fprintf(os.Stderr, "awakened %s: %s\n", event.TodoID, event.Reason)
		}
		return nil
	}
	fmt.Printf("Archived %s: %s\n", result.Todo.ID, result.Todo.Title)
	for _, event := range result.Awakened {
		fmt.Printf("awakened %s: %s\n", event.TodoID, event.Reason)
	}
	return nil
}

func runTodoTrash(cmd *cobra.Command, args []string) error {
	return runTodoRetention(cmd, args, workapp.RetentionArchive, "archived", "Archived")
}

func runTodoUnarchive(cmd *cobra.Command, args []string) error {
	return runTodoRetention(cmd, args, workapp.RetentionRestore, "restored", "Restored")
}
