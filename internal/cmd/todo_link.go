package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoLinkKindFlag     string
	todoLinkTitleFlag    string
	todoLinkRelationFlag string
)

func init() {
	todoLinkAddCmd.Flags().StringVar(&todoLinkKindFlag, "kind", "", "link kind, for example: cr, mr, pipeline, workitem, document")
	todoLinkAddCmd.Flags().StringVar(&todoLinkTitleFlag, "title", "", "human-readable link title")
	todoLinkAddCmd.Flags().StringVar(&todoLinkRelationFlag, "relation", "", "relationship to the todo, for example: tracks, blocks, evidence")
	todoLinkCmd.AddCommand(todoLinkAddCmd, todoLinkListCmd, todoLinkRemoveCmd)
	todoCmd.AddCommand(todoLinkCmd)
}

var todoLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage full-URL links attached to a todo",
	Args:  noSubcommandArgs, // unknown subcommand errors instead of silently showing help
	RunE:  showHelp,
}

var todoLinkAddCmd = &cobra.Command{
	Use:   "add <id> <url>",
	Short: "Attach a full URL to a todo",
	Args:  cobra.ExactArgs(2),
	RunE:  runTodoLinkAdd,
}

var todoLinkListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List URLs attached to a todo",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoLinkList,
}

var todoLinkRemoveCmd = &cobra.Command{
	Use:   "remove <id> <url>",
	Short: "Remove a URL from a todo",
	Args:  cobra.ExactArgs(2),
	RunE:  runTodoLinkRemove,
}

func runTodoLinkAdd(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.AddLink(cmd.Context(), cliApplicationCall("todo-link-add", ""), workapp.AddLinkInput{
		TodoID:   args[0],
		URL:      args[1],
		Kind:     strings.TrimSpace(todoLinkKindFlag),
		Title:    strings.TrimSpace(todoLinkTitleFlag),
		Relation: strings.TrimSpace(todoLinkRelationFlag),
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(result)
		return nil
	}
	action := "Updated"
	if result.Created {
		action = "Linked"
	}
	fmt.Printf("%s %s: %s\n", action, result.TodoID, result.Link.URL)
	return nil
}

func runTodoLinkList(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.ListLinks(cmd.Context(), cliApplicationCall("todo-link-list", ""), workapp.ListLinksInput{
		TodoID: args[0],
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Links)
		return nil
	}
	if len(result.Links) == 0 {
		fmt.Println("No links found.")
		return nil
	}
	for i, link := range result.Links {
		label := link.Title
		if label == "" {
			label = link.URL
		}
		meta := []string{}
		if link.Kind != "" {
			meta = append(meta, link.Kind)
		}
		if link.Relation != "" {
			meta = append(meta, link.Relation)
		}
		if len(meta) > 0 {
			fmt.Printf("%d. %s [%s]\n   %s\n", i+1, label, strings.Join(meta, ", "), link.URL)
		} else {
			fmt.Printf("%d. %s\n", i+1, link.URL)
		}
	}
	return nil
}

func runTodoLinkRemove(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.RemoveLink(cmd.Context(), cliApplicationCall("todo-link-remove", ""), workapp.RemoveLinkInput{
		TodoID: args[0],
		URL:    args[1],
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(result)
		return nil
	}
	fmt.Printf("Unlinked %s: %s\n", result.TodoID, result.Removed.URL)
	return nil
}

func normalizeTodoLinkURL(raw string) (string, error) {
	return workapp.NormalizeTodoLinkURL(raw)
}

func inferTodoLinkKind(raw string) string {
	return workapp.InferTodoLinkKind(raw)
}
