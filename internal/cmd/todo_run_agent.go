package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

// TaskRunAgentInfo describes the dispatch target for the CLI and the macOS
// confirm sheet. One row, because dispatch is Codex only; what the App still
// needs from it is whether the binary is on PATH and what a run costs.
//
// The per-agent capability fields are gone with the other agents. They existed
// to describe differences — Pi had no sandbox ATM could enforce, so it was
// trusted-only — and a single row makes each of them a constant that reads as if
// a choice were still being made.
type TaskRunAgentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Binary    string `json:"binary"`
	Available bool   `json:"available"`
	CostNote  string `json:"cost_note"`
}

const taskRunDispatchAgentID = "codex"

var taskRunDispatchAgent = TaskRunAgentInfo{
	ID: taskRunDispatchAgentID, Name: "Codex", Binary: "codex",
	CostNote: "使用当前 Codex 套餐或 API 配额；每次执行和继续修改都会产生新的模型用量。",
}

func listTaskRunAgents() []TaskRunAgentInfo {
	agent := taskRunDispatchAgent
	_, err := resolveTaskRunAgentBinary(agent.Binary)
	agent.Available = err == nil
	return []TaskRunAgentInfo{agent}
}

// resolveTaskRunDispatchAgent returns the only dispatch target.
//
// `--agent` is a persistent root flag, so it reaches `todo run` whether or not
// dispatch has anything to do with it. It is a *read* filter — which agent's
// sessions, todos or usage to look at — and it used to double as the executor
// switch. Rejecting anything but Codex is the point: silently launching the CLI
// the flag names would send work to an Agent whose sandbox ATM cannot enforce.
func resolveTaskRunDispatchAgent() (TaskRunAgentInfo, error) {
	value := strings.TrimSpace(agentFlag)
	if value == "" {
		return taskRunDispatchAgent, nil
	}
	if config.NormalizeAgent(value) == taskRunDispatchAgentID {
		return taskRunDispatchAgent, nil
	}
	return TaskRunAgentInfo{}, fmt.Errorf(
		"--agent is a read filter, not a dispatch target: todo run only dispatches Codex (got %q)", value)
}

func buildTaskRunCommand(run store.TaskRun) (*exec.Cmd, error) {
	if run.Agent != "" && run.Agent != taskRunDispatchAgentID {
		return nil, fmt.Errorf("todo run only dispatches Codex (got %q)", run.Agent)
	}
	return buildCodexTaskRunCommand(run)
}

var todoAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Show whether Codex is available for Todo dispatch",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		agents := listTaskRunAgents()
		if jsonOutput {
			output.JSON(agents)
			return nil
		}
		for _, agent := range agents {
			status := "missing"
			if agent.Available {
				status = "available"
			}
			fmt.Printf("%-12s %-10s %s\n", agent.ID, status, agent.CostNote)
		}
		return nil
	},
}

func init() {
	todoCmd.AddCommand(todoAgentsCmd)
}
