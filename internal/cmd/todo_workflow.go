package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"

	"github.com/spf13/cobra"
)

func runTodoSubmit(cmd *cobra.Command, args []string) error {
	call := todoSubmitCLICall()
	id, sessionID := todoTransitionTarget(args)
	result, err := workapp.Default.Submit(cmd.Context(), call, workapp.SubmitInput{
		TodoID:    id,
		SessionID: sessionID,
		Reason:    todoSubmitReasonFlag,
	})
	if err != nil {
		return err
	}
	// An already-review retry may be the first process alive long enough to
	// apply an effect committed by an earlier invocation.
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if result.AlreadyReview {
		if jsonOutput {
			output.JSON(&result.Todo)
		} else {
			fmt.Printf("Submitted %s already review: %s\n", result.Todo.ID, result.Todo.Title)
		}
		return nil
	}
	if jsonOutput {
		output.JSON(&result.Todo)
		return nil
	}
	fmt.Printf("Submitted %s for confirmation: %s\n", result.Todo.ID, result.Todo.Title)
	return nil
}

// todoWorkflowCLICall derives consistent provenance from the process
// environment. Request flags describe work intent; they cannot self-declare an
// Agent or controller. See cliApplicationCall for the authentication limit.
func todoWorkflowCLICall(action string) application.Call {
	return cliApplicationCall("todo-"+action, "")
}

func todoSubmitCLICall() application.Call {
	return todoWorkflowCLICall("submit")
}

func todoTransitionTarget(args []string) (todoID, sessionID string) {
	if len(args) > 0 && args[0] != "current" {
		return args[0], ""
	}
	sessionID, _ = resolveSessionID(false)
	return "", sessionID
}

// Human-facing lifecycle events. Start/edit/start-work are noise; these are not.
const (
	notifyEventCreated  = "created"
	notifyEventReview   = "review"
	notifyEventDone     = "done"
	notifyEventArchived = "archived"
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
	case notifyEventArchived:
		subtitle = fmt.Sprintf("%s 已归档", t.ID)
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
	if forwardTodoNotification(t, event, title, subtitle, msg) {
		return
	}
	postLocalBanner(title, subtitle, msg, "todo show "+t.ID)
}

// postLocalBanner raises one desktop notification, fire and forget.
//
// `execute` is the argument string for a click-through back into this same
// binary, and is only honoured by terminal-notifier — the osascript and
// notify-send fallbacks have nowhere to put it. Pass "" when there is nothing to
// open.
//
// Every path is .Start() and never waited on: a banner is a courtesy, and a
// notifier that hangs must not become the caller's problem. In particular the
// outbound action gate calls this while an agent is blocked on it.
func postLocalBanner(title, subtitle, msg, execute string) {
	if skipLocalNotification() {
		return
	}
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		args := []string{"-title", title, "-subtitle", subtitle, "-message", msg}
		if execute != "" {
			bin, err := os.Executable()
			if err != nil {
				bin = "atm"
			}
			args = append(args, "-execute", bin+" "+execute)
		}
		exec.Command(path, args...).Start()
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
