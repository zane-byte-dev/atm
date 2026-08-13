package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"

	"github.com/spf13/cobra"
)

func runTodoFocus(cmd *cobra.Command, args []string) error {
	tf, t, err := startTodo(args[0])
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
		store.AddTodoTag(t, store.TodoTagMaintenance)
		t.MaintenanceLimit = todoMaintainLimitFlag
		return nil
	})
	if err != nil {
		return err
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Maintaining %s (limit %d): %s", t.ID, t.MaintenanceLimit, t.Title))
}

func runTodoStart(cmd *cobra.Command, args []string) error {
	tf, t, err := startTodo(args[0])
	if err != nil {
		return err
	}
	return finishTodoMutation(tf, t, fmt.Sprintf("Started %s: %s", t.ID, t.Title))
}

// startTodo backs both `todo start` and its deprecated alias `todo focus`. The
// two differed only in that focus accepted --lane, which no longer exists, so
// the alias is now the same command under an older name.
func startTodo(id string) (*store.TodoFile, *store.Todo, error) {
	return mutateTodo(id, func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
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

func runTodoSubmit(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	tf, t, alreadyReview, err := submitTodo(id, todoSubmitReasonFlag)
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
	return finishTodoMutation(tf, t, fmt.Sprintf("Submitted %s for confirmation: %s", t.ID, t.Title))
}

// submitTodo is shared by the foreground command and the detached run
// controller. It is the only automatic success transition: review is a human
// gate, so an Agent exit can never call the done path.
func submitTodo(id, reason string) (*store.TodoFile, *store.Todo, bool, error) {
	message := "[submit]"
	if reason != "" {
		message += " " + reason
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
		return nil, nil, false, err
	}
	if alreadyReview {
		return tf, t, true, nil
	}
	if _, err := store.AppendTodoLog(t, message, ""); err != nil {
		return nil, nil, false, err
	}
	// Submit is the human gate: agent finished, person needs to accept.
	notifyTodoEvent(t, notifyEventReview)
	if err := syncExistingTodoDocs(tf, t.ID); err != nil {
		return nil, nil, false, err
	}
	return tf, t, false, nil
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
