package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
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
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	tf, todo, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	issues := []store.TodoLintIssue{}
	content, err := store.ReadTodoDoc(todo.ID)
	if os.IsNotExist(err) {
		issues = append(issues, store.TodoLintIssue{
			Severity:   "info",
			Code:       "doc_missing",
			Detail:     "the todo has no markdown card",
			Suggestion: "run `atm todo doc " + todo.ID + " --init` or record the first milestone to create it",
		})
	} else if err != nil {
		return err
	} else {
		issues, err = store.LintTodoDoc(tf, todo, content)
		if err != nil {
			return err
		}
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"todo_id": todo.ID,
			"issues":  issues,
			"summary": map[string]int{"issues": len(issues)},
		})
		return nil
	}
	fmt.Printf("Todo lint %s: %d issue(s)\n", todo.ID, len(issues))
	if len(issues) == 0 {
		fmt.Println("  clean")
		return nil
	}
	for _, issue := range issues {
		fmt.Printf("  %-7s %-40s %s\n", issue.Severity, issue.Code, issue.Detail)
		fmt.Printf("          next: %s\n", issue.Suggestion)
	}
	return nil
}

func validateTodoLogReferences(tf *store.TodoFile, message string) error {
	if unknown := store.UnknownTodoReferences(tf, message); len(unknown) > 0 {
		return fmt.Errorf("todo log references unknown todo IDs: %s; create and verify structured todos before logging them", strings.Join(unknown, ", "))
	}
	return nil
}

func syncExistingTodoDocs(tf *store.TodoFile, ids ...string) error {
	for _, id := range uniqueStrings(ids) {
		todo := store.FindTodo(tf, id)
		if todo == nil {
			continue
		}
		if !store.TodoDocExists(todo.ID) {
			continue
		}
		if err := store.SyncTodoDocMetadata(todo); err != nil {
			return fmt.Errorf("sync todo doc %s: %w", todo.ID, err)
		}
	}
	return nil
}

// ensureTodoDocs creates missing markdown cards and syncs metadata for the
// given todos. Use this on create and agent-handoff paths so `todo doc` always
// has something to return.
func ensureTodoDocs(tf *store.TodoFile, ids ...string) error {
	for _, id := range uniqueStrings(ids) {
		todo := store.FindTodo(tf, id)
		if todo == nil {
			continue
		}
		if _, err := store.EnsureTodoDoc(todo); err != nil {
			return fmt.Errorf("ensure todo doc %s: %w", todo.ID, err)
		}
	}
	return nil
}
