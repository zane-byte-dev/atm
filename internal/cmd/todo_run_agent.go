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

// TaskRunAgentInfo is the capability contract shared by the CLI and the macOS
// confirm sheet. Dispatch is Codex only; the catalog is a single row so the
// App can still ask whether the binary is on PATH.
type TaskRunAgentInfo struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Binary           string `json:"binary"`
	Available        bool   `json:"available"`
	GuardedSupported bool   `json:"guarded_supported"`
	ResumeSupported  bool   `json:"resume_supported"`
	CostNote         string `json:"cost_note"`
	SafetyNote       string `json:"safety_note,omitempty"`
}

const taskRunDispatchAgentID = "codex"

var taskRunDispatchAgent = TaskRunAgentInfo{
	ID: taskRunDispatchAgentID, Name: "Codex", Binary: "codex",
	GuardedSupported: true, ResumeSupported: true,
	CostNote: "使用当前 Codex 套餐或 API 配额；每次执行和继续修改都会产生新的模型用量。",
}

func listTaskRunAgents() []TaskRunAgentInfo {
	agent := taskRunDispatchAgent
	_, err := resolveTaskRunAgentBinary(agent.Binary)
	agent.Available = err == nil
	return []TaskRunAgentInfo{agent}
}

// resolveTaskRunDispatchAgent returns the only dispatch target. The global
// --agent flag filters list/stats; it must not pick a different executor.
// A non-empty value that is not Codex is rejected so `atm --agent grokbuild
// todo run t1` cannot silently launch the wrong CLI.
func resolveTaskRunDispatchAgent() (TaskRunAgentInfo, error) {
	value := strings.TrimSpace(agentFlag)
	if value == "" {
		return taskRunDispatchAgent, nil
	}
	if config.NormalizeAgent(value) == taskRunDispatchAgentID {
		return taskRunDispatchAgent, nil
	}
	return TaskRunAgentInfo{}, fmt.Errorf("todo run only dispatches Codex (got %q)", value)
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
			fmt.Printf("%-12s %-10s %-15s %s\n", agent.ID, status, "guarded/trusted", agent.CostNote)
		}
		return nil
	},
}

func init() {
	todoCmd.AddCommand(todoAgentsCmd)
}
