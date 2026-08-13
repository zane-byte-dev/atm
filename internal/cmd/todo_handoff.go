package cmd

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
)

// openHandoffURL is injectable so tests can assert the URL without opening
// anything on the machine running them.
var openHandoffURL = openURLWithSystemOpener

// codexNewThreadDeepLink builds the deep link that opens a new Codex chat in one
// workspace with the handoff pointer already typed.
//
// The route and the two parameter names are the ones that actually work in Codex
// Desktop 0.147.0-alpha; the plausible-looking alternatives do not, and each
// failure is silent, which is why they are recorded here:
//
//   - `codex://new?path=…` sets the workspace. `codex://threads/new?cwd=…` is
//     accepted and ignored — the chat opens in whatever workspace the app last
//     had, so the task text lands pointing at the wrong repository.
//   - `prompt=` fills the composer on both routes. `q=` is ignored.
//   - Neither route submits the turn. The human presses Enter, which is the
//     property that keeps this a handoff rather than ATM launching a session.
//
// Spaces are percent-encoded rather than left as `+`: `+` is only equivalent to
// a space by the form-encoding convention, and this app is not a form.
func codexNewThreadDeepLink(workDir, prompt string) string {
	return fmt.Sprintf(
		"codex://new?path=%s&prompt=%s",
		queryEscapeSpaces(workDir),
		queryEscapeSpaces(prompt),
	)
}

func queryEscapeSpaces(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func openURLWithSystemOpener(target string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("opening Codex is macOS only; use --print to get the URL")
	}
	return exec.Command("open", target).Run()
}

// runTodoHandoff hands a Todo to Codex Desktop without ATM running an Agent.
//
// The counterpart of `todo run`, and the one that matches ATM's own rule that it
// never starts a session on the user's behalf: the pointer text and the working
// directory are prepared here, the conversation, the approvals and the final
// Enter belong to Codex. Nothing is claimed in `task_runs`, and the Todo is not
// started — `atm session bind`, which the pointer tells the Agent to run, is
// what records that work actually began.
func runTodoHandoff(cmd *cobra.Command, args []string) error {
	_, todo, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	workDir, source, err := resolveTaskRunCWD(todo, todoHandoffCWDFlag)
	if err != nil {
		return err
	}
	prompt := buildTodoPrompt(todo)
	target := codexNewThreadDeepLink(workDir, prompt)

	if !todoHandoffPrintFlag {
		if err := openHandoffURL(target); err != nil {
			return fmt.Errorf("open Codex: %w", err)
		}
	}
	if jsonOutput {
		output.JSON(map[string]any{
			"todo": todo.ID, "cwd": workDir, "cwd_source": source,
			"url": target, "prompt": prompt, "opened": !todoHandoffPrintFlag,
		})
		return nil
	}
	if todoHandoffPrintFlag {
		fmt.Println(target)
		return nil
	}
	fmt.Printf("Handed %s to Codex\n", todo.ID)
	fmt.Printf("  Dir:    %s (%s)\n", workDir, source)
	fmt.Printf("  Prompt: %s\n", strings.SplitN(prompt, "\n", 2)[0])
	// The composer is filled but not submitted, so saying so is not a nicety:
	// without it the window looks like a run that silently did nothing.
	fmt.Println("  Codex 已打开并填好这段文字，按回车开始。")
	return nil
}

var todoHandoffCmd = &cobra.Command{
	Use:   "handoff <id>",
	Short: "Open the Todo in Codex Desktop with the pointer prefilled",
	Long: `Open a new Codex Desktop chat in the Todo's working directory with the
handoff pointer already typed, and stop there. ATM starts no Agent, claims no
run, and does not change the Todo's status: the conversation, the approvals and
the Enter that starts the turn all belong to Codex.

Use ` + "`todo run`" + ` instead when nobody will be at the keyboard.`,
	Example: `  atm todo handoff t240
  atm todo handoff t240 --cwd /path/to/worktree
  atm todo handoff t240 --print          # 只输出深链，不打开`,
	Args: cobra.ExactArgs(1),
	RunE: runTodoHandoff,
}
