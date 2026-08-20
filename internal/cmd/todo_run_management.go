package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/taskrun"
)

var taskRunManagementService = taskrun.Default

func runTodoRuns(cmd *cobra.Command, args []string) error {
	result, err := taskRunManagementService.List(cmd.Context(), todoWorkflowCLICall("runs"), taskrun.ListInput{
		TodoID: args[0],
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Runs)
		return nil
	}
	if len(result.Runs) == 0 {
		fmt.Println("No runs recorded.")
		return nil
	}
	for _, run := range result.Runs {
		age := time.Unix(run.StartTS, 0).In(config.Loc).Format("01-02 15:04:05")
		session := "-"
		if run.SessionID != nil {
			session = *run.SessionID
			if len(session) > 12 {
				session = session[:12]
			}
		}
		fmt.Printf("%-20s %-9s %-9s pid=%-7d session=%-12s %s\n",
			age, run.Agent, run.Status, run.PID, session, run.Message)
	}
	return nil
}

func runTodoRunInterrupt(cmd *cobra.Command, args []string) error {
	result, err := taskRunManagementService.Interrupt(
		cmd.Context(), todoWorkflowCLICall("interrupt"), taskrun.InterruptInput{TodoID: args[0]},
	)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{"run": result.Run})
		return nil
	}
	fmt.Printf("Interrupted %s agent run %s\n", args[0], result.Run.ID)
	return nil
}

func runTodoRunTail(cmd *cobra.Command, args []string) error {
	_, err := taskRunManagementService.Tail(cmd.Context(), todoWorkflowCLICall("tail"), taskrun.TailInput{
		TodoID: args[0], MaxBytes: todoRunTailBytesFlag, Follow: todoRunTailFollowFlag,
	}, cmd.OutOrStdout())
	return err
}

func runTodoAgents(cmd *cobra.Command, _ []string) error {
	result, err := taskRunManagementService.Agents(cmd.Context(), todoWorkflowCLICall("agents"), taskrun.AgentsInput{})
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(result.Agents)
		return nil
	}
	for _, agent := range result.Agents {
		status := "missing"
		if agent.Available {
			status = "available"
		}
		fmt.Printf("%-12s %-10s %s\n", agent.ID, status, agent.CostNote)
	}
	return nil
}

var todoAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Show whether Codex is available for Todo dispatch",
	Args:  cobra.NoArgs,
	RunE:  runTodoAgents,
}

func init() {
	todoCmd.AddCommand(todoAgentsCmd)
}
