package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"

	"github.com/spf13/cobra"
)

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

// validateInlineTodoDescription catches the common shell-quoting mistake where
// a caller passes "first\\n- second" to --desc expecting the backslash escape to
// become a newline. Cobra receives those bytes verbatim, and persisting them
// makes the Markdown reader show "\\n" in the task body. Keep this check on the
// inline flag only: --desc-file is the byte-preserving escape hatch for technical
// prose that intentionally discusses encoded newlines.
func validateInlineTodoDescription(description string) error {
	if strings.Contains(description, "\n") || strings.Count(description, `\n`) < 2 {
		return nil
	}
	for _, line := range strings.Split(description, `\n`)[1:] {
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") ||
			strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "#") {
			return fmt.Errorf(
				"description contains literal \\n sequences before Markdown structure; " +
					"use real line breaks (for multiline CLI input, use --desc-file)",
			)
		}
	}
	return nil
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
	} else if len(args) == 1 && store.LooksLikeTodoID(args[0]) {
		// A lone argument is the entry text, because the id is optional and defaults
		// to the bound todo. That made `atm todo log t65` — an id with the message
		// forgotten — append "t65" as a progress entry to whatever todo the session
		// was bound to, silently and successfully: the id is a valid reference, and
		// nothing else about a three-character entry is invalid.
		//
		// Refuse instead of guessing which of the two was meant. Writing to a doc is
		// not something to be clever about, and the caller is one word away from
		// saying it unambiguously.
		// Quote what they typed, but suggest the canonical id: the message is meant
		// to be edited and re-run, and `atm todo log #275 "..."` reads like a typo
		// even though it now works.
		return fmt.Errorf(
			"%q looks like a todo id, not a progress entry: pass the text too "+
				"(`atm todo log %s \"<entry>\"`), or use --message-file to read it from a file",
			args[0], store.NormalizeTodoID(args[0]))
	}
	if id == "" || id == "current" {
		var err error
		id, err = resolveCurrentTodoID()
		if err != nil {
			return err
		}
	}
	msg, err := readBodyFlagOrFile(cmd, "message", strings.Join(messageArgs, " "), todoLogMessageFileFlag)
	if err != nil {
		return err
	}
	msg = strings.TrimRight(msg, "\n")
	result, err := workapp.Default.Log(cmd.Context(), cliApplicationCall("todo-log", ""), workapp.LogInput{
		TodoID:  id,
		Message: msg,
		Section: todoLogSectionFlag,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"success": true,
			"path":    result.Path,
			"entry":   strings.TrimSpace(result.Entry),
		})
		return nil
	}
	fmt.Printf("Logged to %s: %s", result.TodoID, result.Entry)
	return nil
}
