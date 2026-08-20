package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

// runTodoPrompt writes the pointer a human pastes into a fresh Agent session.
// The Agent reads the live requirement through todo doc instead of starting
// from a copied snapshot that can immediately drift.
func runTodoPrompt(cmd *cobra.Command, args []string) error {
	_, todo, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	prompt := buildTodoPrompt(todo)
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

func buildTodoPrompt(todo *store.Todo) string {
	return fmt.Sprintf(
		"使用 atm 实现任务 %s：%s\n先跑 atm todo doc %s 拿需求正文，再 atm session bind %s。",
		todo.ID, todo.Title, todo.ID, todo.ID,
	)
}
