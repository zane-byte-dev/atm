package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var todoDependCmd = &cobra.Command{
	Use:   "depend",
	Short: "Manage structured dependencies between todos",
	Args:  noSubcommandArgs,
	RunE:  showHelp,
}

var todoDependAddCmd = &cobra.Command{
	Use:   "add <todo-id> <dependency-id>",
	Short: "Make a todo depend on another todo",
	Example: `  # t77 depends on t76 being done
  atm todo depend add t77 t76`,
	Args: cobra.ExactArgs(2),
	RunE: runTodoDependAdd,
}

var todoDependRemoveCmd = &cobra.Command{
	Use:   "remove <todo-id> <dependency-id>",
	Short: "Remove a todo dependency",
	Args:  cobra.ExactArgs(2),
	RunE:  runTodoDependRemove,
}

var todoDependListCmd = &cobra.Command{
	Use:   "list <todo-id>",
	Short: "List dependency status for a todo",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoDependList,
}

func init() {
	todoDependCmd.AddCommand(todoDependAddCmd, todoDependRemoveCmd, todoDependListCmd)
	todoCmd.AddCommand(todoDependCmd)
}

func runTodoDependAdd(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("depend-add")
	result, err := workapp.Default.AddDependency(cmd.Context(), call, workapp.AddDependencyInput{
		TodoID:       args[0],
		DependencyID: args[1],
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result)
		return nil
	}
	fmt.Printf("Added dependency: %s depends on %s\n", args[0], args[1])
	for _, event := range result.Awakened {
		fmt.Printf("Awakened %s: %s\n", event.TodoID, event.Reason)
	}
	return nil
}

func runTodoDependRemove(cmd *cobra.Command, args []string) error {
	call := todoWorkflowCLICall("depend-remove")
	result, err := workapp.Default.RemoveDependency(cmd.Context(), call, workapp.RemoveDependencyInput{
		TodoID:       args[0],
		DependencyID: args[1],
	})
	if err != nil {
		return err
	}
	if err := workapp.Default.DeliverEffects(cmd.Context(), call, result.Effects, localWorkEffectExecutor{}); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result)
		return nil
	}
	if !result.Removed {
		fmt.Printf("Dependency was not present: %s -> %s\n", args[0], args[1])
		return nil
	}
	fmt.Printf("Removed dependency: %s -> %s\n", args[0], args[1])
	return nil
}

func runTodoDependList(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.ListDependencies(cmd.Context(), todoWorkflowCLICall("depend-list"), workapp.ListDependenciesInput{
		TodoID: args[0],
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Dependencies)
		return nil
	}
	if len(result.Dependencies) == 0 {
		fmt.Println("No dependencies.")
		return nil
	}
	for _, dependency := range result.Dependencies {
		fmt.Printf("%-6s %-12s %s\n", dependency.ID, dependency.Status, dependency.Title)
	}
	return nil
}
