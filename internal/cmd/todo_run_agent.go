package cmd

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

// TaskRunAgentInfo is the stable capability contract shared by the CLI and the
// macOS picker. A dispatch always selects exactly one Agent; ATM never fans a
// Todo out implicitly, which keeps both execution and billing predictable.
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

var taskRunAgentCatalog = []TaskRunAgentInfo{
	{
		ID: "codex", Name: "Codex", Binary: "codex", GuardedSupported: true, ResumeSupported: true,
		CostNote: "使用当前 Codex 套餐或 API 配额；每次执行和继续修改都会产生新的模型用量。",
	},
	{
		ID: "claude", Name: "Claude Code", Binary: "claude", GuardedSupported: true, ResumeSupported: true,
		CostNote: "使用当前 Claude 订阅或 API 配额；每次执行和继续修改都会产生新的模型用量。",
		SafetyNote: "Claude Code 没有可由 ATM 强制的文件系统 sandbox；guarded 依赖它自己的权限规则：" +
			"工作目录内的文件编辑自动放行，其余工具在非交互下按未授权拒绝。",
	},
	{
		ID: "grokbuild", Name: "Grok Build", Binary: "grok", GuardedSupported: true, ResumeSupported: true,
		CostNote: "使用当前 Grok Build 账号的 credits 或套餐配额；ATM 只启动这一个 Agent。",
	},
	{
		ID: "pi", Name: "Pi", Binary: "pi", GuardedSupported: false, ResumeSupported: true,
		CostNote:   "费用取决于 Pi 当前配置的 provider 和模型，可能按 API token 单独计费。",
		SafetyNote: "Pi CLI 没有可由 ATM 强制的原生文件系统沙箱，只能以 trusted 策略运行。",
	},
}

func listTaskRunAgents() []TaskRunAgentInfo {
	agents := make([]TaskRunAgentInfo, len(taskRunAgentCatalog))
	copy(agents, taskRunAgentCatalog)
	for index := range agents {
		_, err := resolveTaskRunAgentBinary(agents[index].Binary)
		agents[index].Available = err == nil
	}
	return agents
}

func resolveTaskRunAgent(value string) (TaskRunAgentInfo, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "codex"
	}
	normalized := config.NormalizeAgent(value)
	for _, agent := range taskRunAgentCatalog {
		if agent.ID == normalized {
			return agent, nil
		}
	}
	supported := make([]string, 0, len(taskRunAgentCatalog))
	for _, agent := range taskRunAgentCatalog {
		supported = append(supported, agent.ID)
	}
	return TaskRunAgentInfo{}, fmt.Errorf("unsupported task-run agent %q (use %s)", value, strings.Join(supported, ", "))
}

func newTaskRunSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func buildTaskRunCommand(run store.TaskRun) (*exec.Cmd, error) {
	switch run.Agent {
	case "codex":
		return buildCodexTaskRunCommand(run)
	case "claude":
		return buildClaudeTaskRunCommand(run)
	case "grokbuild":
		return buildGrokTaskRunCommand(run)
	case "pi":
		return buildPiTaskRunCommand(run)
	default:
		return nil, fmt.Errorf("unsupported task-run agent %q", run.Agent)
	}
}

// buildClaudeTaskRunCommand runs Claude Code headlessly. Unlike Codex and Grok
// it exposes no CLI sandbox flag, so a guarded run leans on Claude's own
// permission model: acceptEdits auto-approves edits inside the working
// directory, and every other tool falls back to a permission prompt that a
// non-interactive session resolves as a denial rather than a hang. ATM only
// widens that with the `atm` CLI the prompt tells the Agent to call; both
// pattern dialects are passed because the allow-rule syntax differs across
// Claude Code versions and an unmatched rule would silently deny.
func buildClaudeTaskRunCommand(run store.TaskRun) (*exec.Cmd, error) {
	binary, err := resolveTaskRunAgentBinary("claude")
	if err != nil {
		return nil, fmt.Errorf("claude not found in PATH")
	}
	// stream-json keeps the run log parseable and requires --verbose in print mode.
	args := []string{"--print", "--output-format", "stream-json", "--verbose"}
	if run.Policy == "trusted" {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args,
			"--permission-mode", "acceptEdits",
			"--add-dir", config.AtmDir,
			"--allowedTools", "Bash(atm:*)", "Bash(atm *)")
	}
	if resumeID := taskRunResumeID(run); resumeID != "" {
		// Claude keeps the resumed session's own id, so the run stays bound to the
		// same transcript ATM already registered.
		args = append(args, "--resume", resumeID)
	} else if run.SessionID != nil {
		args = append(args, "--session-id", strings.TrimSpace(*run.SessionID))
	}
	command := exec.Command(binary, args...)
	// The prompt goes through stdin: it is multi-line user text, and print mode
	// reads stdin when no positional prompt is given.
	command.Stdin = strings.NewReader(run.Prompt)
	command.Env = taskRunAgentEnvironment(run)
	return command, nil
}

func buildGrokTaskRunCommand(run store.TaskRun) (*exec.Cmd, error) {
	binary, err := resolveTaskRunAgentBinary("grok")
	if err != nil {
		return nil, fmt.Errorf("grok not found in PATH")
	}
	args := []string{
		"--cwd", run.WorkDir,
		"--output-format", "streaming-json",
		"--verbatim",
		"--no-subagents",
	}
	if run.Policy == "trusted" {
		args = append(args, "--permission-mode", "bypassPermissions")
	} else {
		// acceptEdits only auto-approves file edits: every run_terminal_command
		// still raises an approval request, and headless there is nobody to
		// answer it, so the first shell call is cancelled and the whole turn ends
		// before any work happens. `auto` approves what its safety check allows
		// and reports what it blocks back to the model instead of killing the
		// turn. The sandbox, not the prompt policy, stays the enforced boundary:
		// the workspace profile reads globally but restricts writes to the
		// repository, temporary files, and ~/.grok. ATM binds the known session
		// before launch, so the Agent itself does not need to write ~/.atm.
		// Both allow-rule dialects are passed because Grok matches `atm:*` as a
		// command prefix and `atm *` as a glob over the whole command string.
		args = append(args,
			"--permission-mode", "auto",
			"--sandbox", "workspace",
			"--allow", "Bash(atm:*)",
			"--allow", "Bash(atm *)")
	}
	if resumeID := taskRunResumeID(run); resumeID != "" {
		args = append(args, "--resume", resumeID)
	} else if run.SessionID != nil {
		args = append(args, "--session-id", strings.TrimSpace(*run.SessionID))
	}
	args = append(args, "--single", run.Prompt)
	command := exec.Command(binary, args...)
	command.Env = taskRunAgentEnvironment(run)
	return command, nil
}

func buildPiTaskRunCommand(run store.TaskRun) (*exec.Cmd, error) {
	if run.Policy != "trusted" {
		return nil, fmt.Errorf("pi requires --policy trusted because its CLI does not expose a filesystem sandbox")
	}
	binary, err := resolveTaskRunAgentBinary("pi")
	if err != nil {
		return nil, fmt.Errorf("pi not found in PATH")
	}
	args := []string{
		"--mode", "json",
		"--print",
		"--no-extensions",
		"--no-prompt-templates",
		"--no-approve",
	}
	if resumeID := taskRunResumeID(run); resumeID != "" {
		args = append(args, "--session", resumeID)
	} else if run.SessionID != nil {
		args = append(args, "--session-id", strings.TrimSpace(*run.SessionID))
	}
	args = append(args, run.Prompt)
	command := exec.Command(binary, args...)
	command.Env = taskRunAgentEnvironment(run)
	return command, nil
}

func taskRunAgentEnvironment(run store.TaskRun) []string {
	environment := append(os.Environ(), "ATM_RUN_ID="+run.ID, "ATM_TODO_ID="+run.TodoID)
	if run.SessionID != nil && strings.TrimSpace(*run.SessionID) != "" {
		environment = append(environment, "ATM_SESSION_ID="+strings.TrimSpace(*run.SessionID))
	}
	return environment
}

var todoAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "List Agent CLIs available for Todo dispatch",
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
			policy := "guarded/trusted"
			if !agent.GuardedSupported {
				policy = "trusted only"
			}
			fmt.Printf("%-12s %-10s %-15s %s\n", agent.ID, status, policy, agent.CostNote)
		}
		return nil
	},
}

func init() {
	todoCmd.AddCommand(todoAgentsCmd)
}
