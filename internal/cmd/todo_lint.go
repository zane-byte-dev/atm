package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var todoLintCmd = &cobra.Command{
	Use:   "lint [id]",
	Short: "Check a todo for progress and markdown consistency issues",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTodoLint,
}

func init() {
	todoCmd.AddCommand(todoLintCmd)
}

func runTodoLint(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.Lint(cmd.Context(), todoReadCall("todo-lint"), workapp.LintInput{
		TodoID: todoReadID(args),
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(result)
		return nil
	}
	fmt.Printf("Todo lint %s: %d issue(s)\n", result.TodoID, result.Summary.Issues)
	if len(result.Issues) == 0 {
		fmt.Println("  clean")
		return nil
	}
	for _, issue := range result.Issues {
		fmt.Printf("  %-7s %-40s %s\n", issue.Severity, issue.Code, issue.Detail)
		fmt.Printf("          next: %s\n", issue.Suggestion)
	}
	return nil
}
