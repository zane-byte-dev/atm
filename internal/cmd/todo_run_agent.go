package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/taskrun"
)

// TaskRunAgentInfo describes the dispatch target for the CLI and the macOS
// confirm sheet. One row, because dispatch is Codex only; what the App still
// needs from it is whether the binary is on PATH and what a run costs.
//
// The per-agent capability fields are gone with the other agents. They existed
// to describe differences — Pi had no sandbox ATM could enforce, so it was
// trusted-only — and a single row makes each of them a constant that reads as if
// a choice were still being made.
type TaskRunAgentInfo = taskrun.AgentInfo

const taskRunDispatchAgentID = taskrun.DispatchAgentID

var taskRunDispatchAgent = taskrun.DispatchAgent()

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
