package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	sessionCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what AI tools are currently doing",
	RunE:  runStatus,
}

func compactTools(tools []string) string {
	counts := make(map[string]int)
	var order []string
	for _, t := range tools {
		if counts[t] == 0 {
			order = append(order, t)
		}
		counts[t]++
	}
	var parts []string
	for _, t := range order {
		if counts[t] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", t, counts[t]))
		} else {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func formatAge(seconds int) string {
	return formatShortDuration(int64(seconds)) + " ago"
}

type aiProcess struct {
	PID              string
	Name             string
	StartTime        time.Time
	TTY              string
	TerminalBundleID string
}

const statusSessionRetention = 30 * time.Minute

type statusSessionView struct {
	Tool          string                    `json:"tool"`
	SessionID     string                    `json:"session_id,omitempty"`
	ResumeID      string                    `json:"resume_id,omitempty"`
	Project       string                    `json:"project"`
	Client        string                    `json:"client,omitempty"`
	CWD           string                    `json:"cwd,omitempty"`
	Model         string                    `json:"model,omitempty"`
	Summary       string                    `json:"summary,omitempty"`
	AgeSeconds    int                       `json:"age_seconds"`
	ActivityState string                    `json:"activity_state"`
	BindingState  string                    `json:"binding_state"`
	Binding       *store.TodoSessionBinding `json:"binding,omitempty"`
	Todo          *compactTodoContext       `json:"todo,omitempty"`
	PID           string                    `json:"pid,omitempty"`
	TTY           string                    `json:"tty,omitempty"`
	TerminalApp   string                    `json:"terminal_app,omitempty"`
	FirstQ        string                    `json:"first_q,omitempty"`
	LastQ         string                    `json:"last_q,omitempty"`
	LastA         string                    `json:"last_a,omitempty"`
	LatestResult  string                    `json:"latest_result,omitempty"`
	Updates       []string                  `json:"updates,omitempty"`
	Tools         []string                  `json:"tools,omitempty"`
	Topics        []string                  `json:"topics,omitempty"`
}

type statusView struct {
	GeneratedAt string                  `json:"generated_at"`
	Time        string                  `json:"time"`
	Sessions    []statusSessionView     `json:"sessions"`
	Bindings    []sessionBindingContext `json:"bindings"`
}

func buildStatusView(agent string) (statusView, error) {
	procs := findAIProcesses()
	// Match Ping Island's primary-session retention: activity becomes idle
	// quickly, but the session remains available for navigation for 30 minutes.
	maxAge := statusSessionRetention
	var all []parser.Session
	for _, adapter := range parser.All() {
		if agent != "" && agent != adapter.Name() {
			continue
		}
		age := maxAge
		if adapter.Name() == "copilot" {
			age = 5 * time.Minute
		}
		all = append(all, adapter.LiveSessions(age)...)
	}
	for index := range all {
		all[index].Project = config.CanonicalProject(all[index].Project)
	}

	now := time.Now().In(config.Loc)
	bindingContexts, bindingMatches, err := loadSessionBindingContexts(all)
	if err != nil {
		return statusView{}, err
	}
	bindingBySession := map[int]*sessionBindingContext{}
	for bindingIndex, sessionIndex := range bindingMatches {
		bindingBySession[sessionIndex] = &bindingContexts[bindingIndex]
	}

	sessions := make([]statusSessionView, 0, len(all))
	usedProcs := make([]bool, len(procs))
	for sessionIndex, session := range all {
		agentKey := sessionAgentKey(session.Tool)
		pid := ""
		processIndex := matchingAIProcessIndex(session, agentKey, procs, usedProcs)
		if processIndex >= 0 {
			pid = procs[processIndex].PID
			usedProcs[processIndex] = true
		}
		var cleanTopics []string
		for _, topic := range session.Topics {
			if clean := cleanMsg(topic); clean != "" {
				cleanTopics = append(cleanTopics, clean)
			}
		}
		view := statusSessionView{
			Tool:          session.Tool,
			SessionID:     session.SessionID,
			ResumeID:      session.ResumeID,
			Project:       session.Project,
			Client:        session.Client,
			CWD:           session.CWD,
			Model:         session.Model,
			Summary:       session.Summary,
			AgeSeconds:    session.AgeSeconds,
			ActivityState: statusActivityState(session.AgeSeconds),
			BindingState:  sessionBindingStateUnbound,
			PID:           pid,
			FirstQ:        cleanMsg(session.FirstQ),
			LastQ:         cleanMsg(session.LastUserMsg),
			LastA:         cleanMsg(session.LastAssistant),
			LatestResult:  cleanMsg(session.LatestResult),
			Tools:         session.RecentTools,
			Topics:        cleanTopics,
		}
		for _, update := range session.RecentUpdates {
			if clean := cleanMsg(update); clean != "" {
				view.Updates = append(view.Updates, clean)
			}
		}
		if pid != "" {
			for _, process := range procs {
				if process.PID == pid {
					view.TTY = process.TTY
					view.TerminalApp = process.TerminalBundleID
					break
				}
			}
		}
		if context := bindingBySession[sessionIndex]; context != nil {
			binding := context.Binding
			view.Binding = &binding
			view.BindingState = context.State
			view.Todo = context.Todo
		}
		sessions = append(sessions, view)
	}
	return statusView{
		GeneratedAt: now.Format(time.RFC3339),
		Time:        now.Format("15:04:05"),
		Sessions:    sessions,
		Bindings:    bindingContexts,
	}, nil
}

func matchingAIProcessIndex(
	session parser.Session,
	agentKey string,
	processes []aiProcess,
	used []bool,
) int {
	if strings.Contains(strings.ToLower(session.Client), "desktop") {
		return -1
	}

	requireTerminal := session.Tool == "Claude Code" || strings.Contains(strings.ToLower(session.Client), "cli")
	bestIndex := -1
	bestDistance := time.Duration(1<<63 - 1)
	for index, process := range processes {
		if used[index] || process.Name != agentKey {
			continue
		}
		if requireTerminal && process.TerminalBundleID == "" {
			continue
		}
		if session.StartedAt.IsZero() {
			return index
		}
		distance := process.StartTime.Sub(session.StartedAt)
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex
}

func findAIProcesses() []aiProcess {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,tty=,lstart=,command=").Output()
	if err != nil {
		return nil
	}
	type processRecord struct {
		pid, ppid, tty, command string
		startTime               time.Time
	}
	records := make(map[string]processRecord)
	var ordered []processRecord
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue
		}
		startTime, _ := time.ParseInLocation(
			"Mon Jan 2 15:04:05 2006",
			strings.Join(parts[3:8], " "),
			config.Loc,
		)
		record := processRecord{
			pid:       parts[0],
			ppid:      parts[1],
			tty:       normalizeProcessTTY(parts[2]),
			startTime: startTime,
			command:   strings.Join(parts[8:], " "),
		}
		records[record.pid] = record
		ordered = append(ordered, record)
	}

	terminalBundle := func(pid string) string {
		for depth := 0; depth < 20 && pid != "" && pid != "0"; depth++ {
			record, ok := records[pid]
			if !ok {
				break
			}
			if bundleID := terminalBundleIdentifier(record.command); bundleID != "" {
				return bundleID
			}
			pid = record.ppid
		}
		return ""
	}

	var procs []aiProcess
	for _, record := range ordered {
		cmd := record.command
		name := ""
		if strings.Contains(cmd, "native-binary/claude") ||
			(strings.Contains(cmd, "claude") && strings.Contains(cmd, "--output-format")) {
			name = "claude"
		} else if strings.Contains(cmd, "codex") && !strings.Contains(cmd, "grep") {
			if strings.Contains(cmd, "app-server") || strings.Contains(cmd, "exec") || strings.HasSuffix(strings.TrimSpace(cmd), "codex") {
				name = "codex"
			}
		} else if isGrokProcessCommand(cmd) {
			// Grok Build's CLI process is often just "grok" (or a versioned
			// download binary under ~/.grok/downloads/). Matching it is what
			// lets the notch resolve TTY + terminal App for "回到会话".
			name = "grokbuild"
		}
		if name == "" {
			continue
		}
		procs = append(procs, aiProcess{
			PID:              record.pid,
			Name:             name,
			StartTime:        record.startTime,
			TTY:              record.tty,
			TerminalBundleID: terminalBundle(record.pid),
		})
	}
	return procs
}

func normalizeProcessTTY(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "??" || value == "-" {
		return ""
	}
	return strings.TrimPrefix(value, "/dev/")
}

func terminalBundleIdentifier(command string) string {
	normalized := strings.ToLower(command)
	switch {
	case strings.Contains(normalized, "/terminal.app/"):
		return "com.apple.Terminal"
	case strings.Contains(normalized, "/iterm.app/") || strings.Contains(normalized, "/iterm2.app/"):
		return "com.googlecode.iterm2"
	case strings.Contains(normalized, "/ghostty.app/"):
		return "com.mitchellh.ghostty"
	case strings.Contains(normalized, "/cursor.app/"):
		return "com.todesktop.230313mzl4w4u92"
	case strings.Contains(normalized, "/visual studio code.app/"), strings.Contains(normalized, "/code.app/"):
		return "com.microsoft.VSCode"
	case strings.Contains(normalized, "/qoder.app/"), strings.Contains(normalized, "/qoderwork.app/"):
		return "com.qoder.work"
	default:
		return ""
	}
}

func sessionAgentKey(tool string) string {
	switch tool {
	case "Claude Code":
		return "claude"
	case "Codex":
		return "codex"
	case "Grok Build":
		return "grokbuild"
	}
	return strings.ToLower(tool)
}

// isGrokProcessCommand reports whether a ps command line is a Grok Build
// agent process rather than a shell snippet that merely mentions "grok".
func isGrokProcessCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	base := filepath.Base(fields[0])
	switch {
	case base == "grok":
		return true
	case strings.HasPrefix(base, "grok-") && strings.Contains(base, "macos"):
		return true
	case strings.Contains(fields[0], "/.grok/") && strings.Contains(base, "grok"):
		return true
	default:
		return false
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	if jsonOutput {
		view, err := buildStatusView(agent)
		if err != nil {
			return err
		}
		output.JSON(view)
		return nil
	}

	procs := findAIProcesses()
	maxAge := 10 * time.Minute
	var all []parser.Session
	for _, a := range parser.All() {
		if agent != "" && agent != a.Name() {
			continue
		}
		d := maxAge
		if a.Name() == "copilot" {
			d = 5 * time.Minute
		}
		all = append(all, a.LiveSessions(d)...)
	}
	for index := range all {
		all[index].Project = config.CanonicalProject(all[index].Project)
	}

	now := time.Now().In(config.Loc)
	bindingContexts, bindingMatches, err := loadSessionBindingContexts(all)
	if err != nil {
		return err
	}
	bindingBySession := map[int]*sessionBindingContext{}
	for bindingIndex, sessionIndex := range bindingMatches {
		bindingBySession[sessionIndex] = &bindingContexts[bindingIndex]
	}

	fmt.Printf("AI Live Status  (%s)\n", now.Format("15:04:05"))
	fmt.Println(strings.Repeat("=", 60))

	if len(all) == 0 && len(procs) == 0 && len(bindingContexts) == 0 {
		fmt.Println("\nNo AI activity.")
		return nil
	}

	usedProcs := make([]bool, len(procs))

	for sessionIndex, s := range all {
		agentKey := sessionAgentKey(s.Tool)
		pid := ""
		for i, p := range procs {
			if !usedProcs[i] && p.Name == agentKey {
				pid = p.PID
				usedProcs[i] = true
				break
			}
		}

		sid := shortSessionID(s.SessionID)

		fmt.Println()
		fmt.Printf("  %-14s %-16s %s   %s", s.Tool, s.Project, sid, formatAge(s.AgeSeconds))
		if s.Model != "" {
			fmt.Printf("   %s", s.Model)
		}
		if pid != "" {
			fmt.Printf("   PID %s", pid)
		}
		if context := bindingBySession[sessionIndex]; context != nil {
			fmt.Printf("   binding=%s:%s", context.State, context.Binding.TodoID)
		}
		fmt.Println()
		title := s.Summary
		usedFirstQ := false
		if title == "" && s.FirstQ != "" {
			title = truncLine(cleanMsg(s.FirstQ), 60)
			usedFirstQ = true
		}
		if title != "" {
			fmt.Printf("    :: %s\n", title)
		}
		if q := cleanMsg(s.LastUserMsg); q != "" {
			if !(usedFirstQ && q == cleanMsg(s.FirstQ)) {
				fmt.Printf("    Q: %s\n", truncLine(q, 80))
			}
		}
		if a := cleanMsg(s.LastAssistant); a != "" {
			fmt.Printf("    A: %s\n", truncLine(a, 80))
		}
		if len(s.Topics) > 0 {
			fmt.Print("    Topics:")
			for _, t := range s.Topics {
				fmt.Printf(" [%s]", truncLine(cleanMsg(t), 40))
			}
			fmt.Println()
		}
		if len(s.RecentTools) > 0 {
			fmt.Printf("    Tools: %s\n", compactTools(s.RecentTools))
		}
	}

	var unmatched []aiProcess
	for i, p := range procs {
		if !usedProcs[i] {
			unmatched = append(unmatched, p)
		}
	}
	if len(unmatched) > 0 {
		fmt.Printf("\n  Other AI processes (%d):\n", len(unmatched))
		for _, p := range unmatched {
			age := int(now.Sub(p.StartTime).Seconds())
			fmt.Printf("    PID %-6s  %s  %s\n", p.PID, p.Name, formatAge(age))
		}
	}

	var unobservedBindings []sessionBindingContext
	for _, context := range bindingContexts {
		if !context.Observed {
			unobservedBindings = append(unobservedBindings, context)
		}
	}
	if len(unobservedBindings) > 0 {
		fmt.Printf("\n  Explicit bindings without live activity (%d):\n", len(unobservedBindings))
		for _, context := range unobservedBindings {
			fmt.Printf("    %-8s %-20s %s\n", context.Binding.TodoID, shortSessionID(context.Binding.SessionID), context.State)
		}
	}

	if len(all) == 0 {
		fmt.Println("\n  No active sessions.")
	}
	return nil
}

func statusActivityState(ageSeconds int) string {
	if ageSeconds >= 120 {
		return "idle"
	}
	return "active"
}
