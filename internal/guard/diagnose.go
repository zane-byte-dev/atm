package guard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// DiagnosticIssue is one way the outbound action gate can be present but not
// working. It is Guard's own vocabulary: the health command that renders these
// maps them into its report rather than Guard knowing about that report.
type DiagnosticIssue struct {
	Severity   string `json:"severity"`
	Domain     string `json:"domain"`
	Code       string `json:"code"`
	Subject    string `json:"subject"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

// stuckApprovalAge is how long an approved-and-started request may go without
// reporting back before it is worth saying so. Short enough to surface a gate
// that died mid-send, long enough that an ordinary slow send does not trip it.
const stuckApprovalAge = 5 * time.Minute

// stuckApprovalLimit bounds the read: this is a health check, not a listing, and
// a user with hundreds of stuck rows has one problem, not hundreds.
const stuckApprovalLimit = 20

// Diagnose reports the ways the gate can be installed and silently ineffective.
// Each of these is invisible by nature: nothing fails, no command errors, and
// the user goes on believing sends are being reviewed.
//
// It never fails on a missing or unreadable store. A gate that cannot be
// inspected must not take down the health command that would have told the user
// about every other problem too.
func (service Service) Diagnose(ctx context.Context, call application.Call) ([]DiagnosticIssue, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return nil, err
	}
	if !execInterpositionSupported() {
		return nil, nil
	}

	var issues []DiagnosticIssue
	installed := 0
	for _, tool := range service.diagnosableTools() {
		binPath, err := Resolve(tool, "")
		if err != nil {
			// The tool is not on this machine at all. Not a problem — the gate only
			// covers what is here.
			continue
		}
		state, err := Status(tool, binPath)
		if err != nil {
			continue
		}
		if state.Installed {
			installed++
		}
		if state.Clobbered {
			issues = append(issues, DiagnosticIssue{
				Severity: "warning", Domain: "guard", Code: "guard_shim_clobbered",
				Subject: state.BinPath,
				Detail: fmt.Sprintf(
					"%s 的闸门已经不在位了（通常是这个 CLI 自己升级时覆盖掉的），它的外发动作现在无人审核", tool),
				Suggestion: fmt.Sprintf("atm guard install %s --bin %s", tool, state.BinPath),
			})
		}
		if state.Installed && state.ShadowedBy != "" {
			issues = append(issues, DiagnosticIssue{
				Severity: "warning", Domain: "guard", Code: "guard_bin_shadowed",
				Subject: state.ShadowedBy,
				Detail: fmt.Sprintf(
					"闸门装在 %s，但 PATH 会先找到 %s；按名字调用 %s 完全绕过闸门",
					state.BinPath, state.ShadowedBy, tool),
				Suggestion: fmt.Sprintf("atm guard install %s（不带 --bin，默认装到 PATH 解析到的那份）", tool),
			})
		}
	}

	issues = append(issues, service.stuckIssues()...)
	if installed > 0 {
		issues = append(issues, mcpIssues()...)
	}
	return issues, nil
}

// diagnosableTools are the tools that have rules to enforce. A tool with no
// rules gates nothing, so its shim state says nothing about coverage.
func (service Service) diagnosableTools() []string {
	tools := make([]string, 0, len(Tools()))
	for tool, toolConfig := range Tools() {
		if len(toolConfig.Rules) > 0 {
			tools = append(tools, tool)
		}
	}
	sort.Strings(tools)
	return tools
}

// stuckIssues reports requests that started executing and never reported back.
// Nothing can resolve these automatically — whether the message went out is not
// recorded anywhere — so the only useful action is to say so and let the user
// check the target.
func (service Service) stuckIssues() []DiagnosticIssue {
	db, err := service.openRead()
	if err != nil {
		return nil
	}
	defer db.Close()
	return service.stuckIssuesFrom(db)
}

func (service Service) stuckIssuesFrom(db *sql.DB) []DiagnosticIssue {
	if db == nil {
		return nil
	}
	now := service.now().In(config.Loc)
	running, err := store.ListApprovals(db, []string{ApprovalRunning}, now.Unix(), stuckApprovalLimit)
	if err != nil {
		return nil
	}
	var issues []DiagnosticIssue
	for _, approval := range running {
		age := now.Sub(time.Unix(approval.RequestedAt, 0))
		if age < stuckApprovalAge {
			continue
		}
		issues = append(issues, DiagnosticIssue{
			Severity: "warning", Domain: "guard", Code: "guard_stuck_running",
			Subject: approval.ID,
			Detail: fmt.Sprintf("%s 已批准并开始执行 %s，但没有回报结果；ATM 无法判断是否真的发出去了",
				ActionLine(approval), shortDuration(int64(age.Seconds()))),
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

// mcpIssues warns the day an MCP server appears. Raised only when the gate is
// installed, because the harm is specifically the false sense of coverage: a
// user who has installed a gate has stopped watching sends, and a channel the
// gate cannot see is worse for them than for someone who never installed it.
func mcpIssues() []DiagnosticIssue {
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
	servers = dedupeSortedStrings(servers)
	return []DiagnosticIssue{{
		Severity: "info", Domain: "guard", Code: "guard_mcp_uncovered",
		Subject: strings.Join(servers, ", "),
		Detail: fmt.Sprintf(
			"配置了 %d 个 MCP server。闸门只看命令执行，通过 MCP 工具完成的外发动作它看不到，也拦不到",
			len(servers)),
		Suggestion: "如果其中有能对外发消息/写文档的工具，只能在对应 agent 自己的权限设置里限制",
	}}
}

func dedupeSortedStrings(values []string) []string {
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

// ActionLine names what was attempted, in one line: what kind of action, and who
// it reaches. Exported because the gate's own blocked-send copy and this
// diagnostic both have to describe the same request the same way.
func ActionLine(approval Approval) string {
	label := strings.TrimSpace(approval.Label)
	if label == "" {
		label = approval.Tool
	}
	if target := strings.TrimSpace(approval.PreviewTarget); target != "" {
		return label + " → " + target
	}
	return label
}

// shortDuration renders an elapsed span as Ns / Nm / NhNm. Guard carries its own
// copy rather than borrowing the command layer's: the sentence it appears in is
// the domain's own wording, and a health finding must not depend on a renderer.
func shortDuration(seconds int64) string {
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	}
	hours, minutes := seconds/3600, (seconds%3600)/60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}
