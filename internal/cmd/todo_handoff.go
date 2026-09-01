package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
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

// buildTodoPrompt writes the pointer a person pastes into a fresh Agent
// session. The Agent reads the live requirement through `todo doc` instead of
// starting from a copied snapshot that can immediately drift.
func buildTodoPrompt(todo *store.Todo) string {
	return fmt.Sprintf(
		"使用 atm 实现任务 %s：%s\n先跑 atm todo doc %s 拿需求正文，再 atm session bind %s。",
		todo.ID, todo.Title, todo.ID, todo.ID,
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
// ATM never starts a session on the user's behalf: the pointer text and the
// working directory are prepared here, the conversation, the approvals and the
// final Enter belong to Codex. The Todo is not started — `atm session bind`,
// which the pointer tells the Agent to run, is what records that work actually
// began.
func runTodoHandoff(cmd *cobra.Command, args []string) error {
	_, todo, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	prompt := buildTodoPrompt(todo)

	// --copy is the former `todo prompt`: hand the pointer over for pasting into
	// a session the user opens themselves. It deliberately returns before the
	// workspace is resolved, because the working directory only matters to the
	// deep link. Resolving it anyway would make copying a line of text fail on a
	// Todo bound to several worktrees, which is a question only the caller who
	// is actually opening Codex has to answer.
	if todoHandoffCopyFlag {
		if err := copyToClipboard(prompt); err != nil {
			return err
		}
		if jsonOutput {
			output.JSON(map[string]any{
				"todo": todo.ID, "prompt": prompt, "opened": false, "copied": true,
			})
			return nil
		}
		fmt.Println(prompt)
		fmt.Fprintln(os.Stderr, "Copied to clipboard.")
		return nil
	}

	workDir, source, err := resolveTodoWorkspace(todo, todoHandoffCWDFlag)
	if err != nil {
		return err
	}
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

ATM does not launch Codex or any other Agent in the background.`,
	Example: `  atm todo handoff t240
  atm todo handoff t240 --cwd /path/to/worktree
  atm todo handoff t240 --print          # 只输出深链，不打开
  atm todo handoff t240 --copy           # 只把指针复制到剪贴板，不打开`,
	Args: cobra.ExactArgs(1),
	RunE: runTodoHandoff,
}
