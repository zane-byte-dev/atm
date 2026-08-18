package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/store"
)

// guardIssues reports the ways the outbound action gate can be present but not
// working. Each of these is silent by nature: nothing fails, no command errors,
// and the user goes on believing sends are being reviewed.
func guardIssues(db *sql.DB) []doctorIssue {
	var issues []doctorIssue
	if !guardExecSupported() {
		return nil
	}

	tools := make([]string, 0, len(guard.Tools()))
	for tool, toolConfig := range guard.Tools() {
		if len(toolConfig.Rules) > 0 {
			tools = append(tools, tool)
		}
	}
	sort.Strings(tools)

	installed := 0
	for _, tool := range tools {
		binPath, err := guard.Resolve(tool, "")
		if err != nil {
			// The tool is not on this machine at all. Not a problem — the gate only
			// covers what is here.
			continue
		}
		state, err := guard.Status(tool, binPath)
		if err != nil {
			continue
		}
		if state.Installed {
			installed++
		}
		if state.Clobbered {
			issues = append(issues, doctorIssue{
				Severity: "warning", Domain: "guard", Code: "guard_shim_clobbered",
				Subject: state.BinPath,
				Detail: fmt.Sprintf(
					"%s 的闸门已经不在位了（通常是这个 CLI 自己升级时覆盖掉的），它的外发动作现在无人审核", tool),
				Suggestion: fmt.Sprintf("atm guard install %s --bin %s", tool, state.BinPath),
			})
		}
		if state.Installed && state.ShadowedBy != "" {
			issues = append(issues, doctorIssue{
				Severity: "warning", Domain: "guard", Code: "guard_bin_shadowed",
				Subject: state.ShadowedBy,
				Detail: fmt.Sprintf(
					"闸门装在 %s，但 PATH 会先找到 %s；按名字调用 %s 完全绕过闸门",
					state.BinPath, state.ShadowedBy, tool),
				Suggestion: fmt.Sprintf("atm guard install %s（不带 --bin，默认装到 PATH 解析到的那份）", tool),
			})
		}
	}

	issues = append(issues, guardStuckIssues(db)...)
	if installed > 0 {
		issues = append(issues, guardMCPIssues()...)
	}
	return issues
}

// guardStuckIssues reports requests that started executing and never reported
// back. Nothing can resolve these automatically — whether the message went out is
// not recorded anywhere — so the only useful action is to say so and let the user
// check the target.
func guardStuckIssues(db *sql.DB) []doctorIssue {
	if db == nil {
		return nil
	}
	now := time.Now().In(config.Loc)
	running, err := store.ListApprovals(db, []string{store.ApprovalRunning}, now.Unix(), 20)
	if err != nil {
		return nil
	}
	var issues []doctorIssue
	for _, approval := range running {
		age := now.Sub(time.Unix(approval.RequestedAt, 0))
		if age < 5*time.Minute {
			continue
		}
		issues = append(issues, doctorIssue{
			Severity: "warning", Domain: "guard", Code: "guard_stuck_running",
			Subject: approval.ID,
			Detail: fmt.Sprintf("%s 已批准并开始执行 %s，但没有回报结果；ATM 无法判断是否真的发出去了",
				guardActionLine(approval), formatShortDuration(int64(age.Seconds()))),
			Suggestion: fmt.Sprintf("自己到目标确认，然后 atm guard show %s 看记录；不要重跑", approval.ID),
		})
	}
	return issues
}

// mcpConfigPaths are the config files whose MCP server lists ATM can read
// without any integration. Checked rather than assumed: the gate covers commands,
// and an outbound action taken through an MCP tool never becomes one.
func mcpConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".qoder", "mcp.json"),
		filepath.Join(home, ".gemini", "config", "mcp_config.json"),
	}
}

// guardMCPIssues warns the day an MCP server appears. Raised only when the gate
// is installed, because the harm is specifically the false sense of coverage: a
// user who has installed a gate has stopped watching sends, and a channel the
// gate cannot see is worse for them than for someone who never installed it.
func guardMCPIssues() []doctorIssue {
	var servers []string
	for _, path := range mcpConfigPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var parsed struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
			Projects   map[string]struct {
				MCPServers map[string]json.RawMessage `json:"mcpServers"`
			} `json:"projects"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue
		}
		for name := range parsed.MCPServers {
			servers = append(servers, name)
		}
		// Claude Code also scopes MCP servers per project, and a server registered
		// in one project is just as invisible to the gate as a global one.
		for _, project := range parsed.Projects {
			for name := range project.MCPServers {
				servers = append(servers, name)
			}
		}
	}
	if len(servers) == 0 {
		return nil
	}
	sort.Strings(servers)
	servers = dedupeStrings(servers)
	return []doctorIssue{{
		Severity: "info", Domain: "guard", Code: "guard_mcp_uncovered",
		Subject: strings.Join(servers, ", "),
		Detail: fmt.Sprintf(
			"配置了 %d 个 MCP server。闸门只看命令执行，通过 MCP 工具完成的外发动作它看不到，也拦不到",
			len(servers)),
		Suggestion: "如果其中有能对外发消息/写文档的工具，只能在对应 agent 自己的权限设置里限制",
	}}
}

func dedupeStrings(values []string) []string {
	unique := values[:0]
	var previous string
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		unique = append(unique, value)
		previous = value
	}
	return unique
}
