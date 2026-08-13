package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func todoMatchesQuery(todo store.Todo, rawQuery string) bool {
	return todoQueryRelevance(todo, rawQuery) >= 0
}

// todoQueryRelevance keeps the existing AND matching contract and adds field
// weighting for query mode. A title or exact Todo ID is a stronger answer than
// a passing mention buried in the generated Todo document.
func todoQueryRelevance(todo store.Todo, rawQuery string) int {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	if query == "" {
		return 0
	}
	document := ""
	if loaded, err := store.ReadTodoDoc(todo.ID); err == nil {
		document = strings.ToLower(loaded)
	}
	id := strings.ToLower(todo.ID)
	title := strings.ToLower(todo.Title)
	description := strings.ToLower(todo.Description)
	project := strings.ToLower(todo.Project)
	source := strings.ToLower(todo.Source)
	haystack := strings.Join([]string{id, title, description, project, source, document}, "\n")
	terms := strings.Fields(query)
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return -1
		}
	}

	score := 0
	if id == query {
		score += 1000
	} else if strings.Contains(id, query) {
		score += 300
	}
	if strings.Contains(title, query) {
		score += 500
		titleRunes := len([]rune(title))
		queryRunes := len([]rune(query))
		score += 1000 * queryRunes / max(titleRunes, 1)
		if position := strings.Index(title, query); position >= 0 {
			score += 100 / (1 + len([]rune(title[:position])))
		}
	}
	if strings.Contains(description, query) {
		score += 180
	}
	if project == query {
		score += 100
	}
	if source == query {
		score += 60
	}
	if strings.Contains(document, query) {
		score += 30
	}
	for _, term := range terms {
		if strings.Contains(title, term) {
			score += 120
		}
		if strings.Contains(description, term) {
			score += 40
		}
		if strings.Contains(document, term) {
			score += 5
		}
	}
	return score
}

func runTodoList(cmd *cobra.Command, args []string) error {
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	// Normalized up front so `--creator 收集` and `--creator collection` select
	// the same rows the stored token does, and so a typo is rejected instead of
	// quietly matching nothing.
	creator, err := store.NormalizeTodoCreator(todoListCreatorFlag)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(todoStatusFlag)
	activeOnly := status == ""
	if status == "all" {
		status = ""
		activeOnly = false
	} else if status != "" && status != "archived" && status != "trashed" && status != store.TodoStatusDone && status != store.TodoStatusDropped {
		if err := validateWorkStatus(status); err != nil {
			return err
		}
	}

	if status == "archived" || status == "trashed" {
		return listArchived(creator)
	}

	var filtered []store.Todo
	for _, t := range tf.Items {
		if activeOnly && !store.TodoIsActive(t) {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		if todoListPriorityFlag != "" && t.Priority != todoListPriorityFlag {
			continue
		}
		if todoProjectFlag != "" && t.Project != todoProjectFlag {
			continue
		}
		if creator != "" && t.Creator != creator {
			continue
		}
		if !todoMatchesQuery(t, todoListQueryFlag) {
			continue
		}
		filtered = append(filtered, t)
	}
	if strings.TrimSpace(todoListQueryFlag) != "" {
		sort.SliceStable(filtered, func(i, j int) bool {
			left := todoQueryRelevance(filtered[i], todoListQueryFlag)
			right := todoQueryRelevance(filtered[j], todoListQueryFlag)
			return left > right
		})
	}
	filtered, err = paginate(filtered, todoListOffsetFlag, todoListLimitFlag)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(filtered)
		return nil
	}

	if len(filtered) == 0 {
		fmt.Println("No todos found.")
		return nil
	}

	// The creator column shows the stored token, not the display name: it is the
	// vocabulary `--creator` takes, and it keeps the column ASCII-width so the
	// table stays aligned.
	fmt.Printf("  %-6s %-4s %-12s %-12s %-10s %-16s %s\n", "ID", "Pri", "Status", "Created", "Creator", "Project", "Title")
	fmt.Printf("  %-6s %-4s %-12s %-12s %-10s %-16s %s\n",
		output.Dashes(6, 4, 12, 12, 10, 16, 30)...)
	for _, t := range filtered {
		id := t.ID
		if store.TodoDocExists(t.ID) {
			id += "*"
		}
		fmt.Printf("  %-6s %-4s %-12s %-12s %-10s %-16s %s\n", id, t.Priority, t.Status, t.Created,
			emptyAs(t.Creator, "-"), t.Project, t.Title)
	}
	return nil
}

func listArchived(creator string) error {
	archived, err := store.LoadArchivedTodos()
	if err != nil {
		return err
	}

	var filtered []store.ArchivedTodo
	for _, t := range archived {
		if todoListPriorityFlag != "" && t.Priority != todoListPriorityFlag {
			continue
		}
		if todoProjectFlag != "" && t.Project != todoProjectFlag {
			continue
		}
		if creator != "" && t.Creator != creator {
			continue
		}
		if !todoMatchesQuery(t.Todo, todoListQueryFlag) {
			continue
		}
		filtered = append(filtered, t)
	}
	filtered, err = paginate(filtered, todoListOffsetFlag, todoListLimitFlag)
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(filtered)
		return nil
	}
	if len(filtered) == 0 {
		fmt.Println("No archived todos.")
		return nil
	}

	fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n", "ID", "Pri", "Status", "Created", "Archived", "Creator", "Project", "Title")
	fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n",
		output.Dashes(6, 4, 8, 12, 12, 10, 16, 30)...)
	for _, t := range filtered {
		archivedOn := time.Unix(t.ArchivedAt, 0).In(config.Loc).Format("2006-01-02")
		fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n",
			t.ID, t.Priority, t.Status, t.Created, archivedOn, emptyAs(t.Creator, "-"), t.Project, t.Title)
	}
	return nil
}

// runTodoPrompt writes the line a human pastes into a fresh agent session.
//
// It deliberately hands over a pointer rather than the task itself. The agent
// reads the requirement from the database on its own, so what it works from is
// always current; a copied snapshot would start drifting the moment the todo
// changed. The canonical ID is spelled out because todo lookups are exact
// matches -- `#101` and `101` resolve to nothing -- and `todo doc` is named
// explicitly because `todo show` prints only the one-line description, not the
// requirement body.
func runTodoPrompt(cmd *cobra.Command, args []string) error {
	_, t, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}

	prompt := buildTodoPrompt(t)

	if todoPromptCopyFlag {
		if err := copyToClipboard(prompt); err != nil {
			return err
		}
	}

	if jsonOutput {
		output.JSON(map[string]any{"prompt": prompt})
		return nil
	}

	fmt.Println(prompt)
	if todoPromptCopyFlag {
		fmt.Fprintln(os.Stderr, "Copied to clipboard.")
	}
	return nil
}

func buildTodoPrompt(t *store.Todo) string {
	return fmt.Sprintf(
		"使用 atm 实现任务 %s：%s\n先跑 atm todo doc %s 拿需求正文，再 atm session bind %s。",
		t.ID, t.Title, t.ID, t.ID,
	)
}

func extractRecentLogs(content string, n int) []string {
	inProgress := false
	var logs []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## 进展") {
			inProgress = true
			continue
		}
		if inProgress && strings.HasPrefix(line, "## ") {
			break
		}
		if inProgress && strings.HasPrefix(line, "- [") {
			logs = append(logs, line)
		}
	}
	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	return logs
}

// nameBoundSessions fills in each bound session's human-readable topic. The
// binding ledger only guarantees an id, and the transcript index only carries a
// title for agents that write one — codex writes none into its rollout, so
// every codex session reached this point unnamed and the list read as a column
// of bare short ids. Codex does keep generated thread names in its own index,
// which is keyed by exactly the id the ledger stores; a session's first real
// prompt is the last resort for everything else.
func nameBoundSessions(db *sql.DB, sessions []store.TodoBoundSession) error {
	var codexTitles map[string]string
	var pending []string
	for i := range sessions {
		session := &sessions[i]
		if session.Summary != "" {
			continue
		}
		if strings.EqualFold(session.Agent, "codex") {
			if codexTitles == nil {
				codexTitles = parser.CodexThreadTitles()
			}
			if title := strings.TrimSpace(codexTitles[session.SessionID]); title != "" {
				session.Summary = title
				continue
			}
		}
		if session.IndexedID != "" {
			pending = append(pending, session.IndexedID)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Agents inject plugin lists and instruction preambles as user turns, so the
	// opening prompt is rarely the first stored message.
	messages, err := store.EarliestUserMessages(db, pending, 8)
	if err != nil {
		return err
	}
	for i := range sessions {
		session := &sessions[i]
		if session.Summary != "" {
			continue
		}
		for _, message := range messages[session.IndexedID] {
			if topic := truncLine(cleanMsg(message), 120); topic != "" {
				session.Summary = topic
				break
			}
		}
	}
	return nil
}

func runTodoShow(cmd *cobra.Command, args []string) error {
	id, err := optionalTodoID(args)
	if err != nil {
		return err
	}
	_, t, err := loadTodoByID(id)
	if err != nil {
		return err
	}

	var boundSessions []store.TodoBoundSession
	var latestRun *store.TaskRun
	if err := withDB(true, func(db *sql.DB) error {
		var e error
		if boundSessions, e = store.FindSessionsForTodo(db, t.ID); e != nil {
			return e
		}
		if err := nameBoundSessions(db, boundSessions); err != nil {
			return err
		}
		latestRun, e = store.LatestTaskRun(db, t.ID)
		return e
	}); err != nil {
		return err
	}

	docPath := store.TodoDocPath(t.ID)
	docExists := store.TodoDocExists(t.ID)
	bindings, err := store.ListTodoSessionBindings(t.ID)
	if err != nil {
		return fmt.Errorf("load todo session bindings: %w", err)
	}

	if jsonOutput {
		encodedTodo, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("encode todo: %w", err)
		}
		var out map[string]any
		if err := json.Unmarshal(encodedTodo, &out); err != nil {
			return fmt.Errorf("build todo response: %w", err)
		}
		// Keep the established nested object while exposing the identical todo
		// fields at the top level, matching `todo list --json` for scripts.
		out["todo"] = t
		out["doc_path"] = docPath
		out["doc_exists"] = docExists
		if len(bindings) > 0 {
			out["bindings"] = bindings
		}
		if len(boundSessions) > 0 {
			out["sessions"] = boundSessions
			var totalCost float64
			var totalQueries, totalTools int
			for _, s := range boundSessions {
				totalCost += s.CostUSD
				totalQueries += s.Queries
				totalTools += s.ToolCalls
			}
			out["summary"] = map[string]any{
				"sessions":   len(boundSessions),
				"queries":    totalQueries,
				"tool_calls": totalTools,
				"cost_usd":   totalCost,
			}
		}
		if latestRun != nil {
			out["latest_run"] = latestRun
		}
		output.JSON(out)
		return nil
	}

	fmt.Printf("ID:       %s\n", t.ID)
	fmt.Printf("Title:    %s\n", t.Title)
	fmt.Printf("Priority: %s\n", t.Priority)
	fmt.Printf("Status:   %s\n", t.Status)
	if len(t.Tags) > 0 {
		fmt.Printf("Tags:     %s\n", strings.Join(t.Tags, ", "))
	}
	if t.WakeCondition != "" {
		fmt.Printf("Wake:     %s\n", t.WakeCondition)
	}
	if t.ReviewAt != "" {
		fmt.Printf("Review:   %s\n", t.ReviewAt)
	}
	if t.MaintenanceLimit > 0 {
		fmt.Printf("Limit:    %d\n", t.MaintenanceLimit)
	}
	if t.Project != "" {
		fmt.Printf("Project:  %s\n", t.Project)
	}
	fmt.Printf("Created:  %s\n", t.Created)
	if t.Creator != "" {
		fmt.Printf("Creator:  %s\n", store.TodoCreatorDisplay(t.Creator))
	}
	if t.Source != "" {
		fmt.Printf("Source:   %s\n", t.Source)
	}
	if t.Description != "" {
		fmt.Printf("Desc:     %s\n", t.Description)
	}
	if len(t.Links) > 0 {
		fmt.Println("Links:")
		for _, link := range t.Links {
			label := link.Title
			if label == "" {
				label = link.URL
			}
			if link.Kind != "" {
				fmt.Printf("  [%s] %s — %s\n", link.Kind, label, link.URL)
			} else {
				fmt.Printf("  %s\n", link.URL)
			}
		}
	}
	if t.StartTS != nil {
		fmt.Printf("Started:  %s\n", time.Unix(*t.StartTS, 0).In(config.Loc).Format("2006-01-02 15:04:05"))
	}
	if latestRun != nil {
		fmt.Printf("Agent:    %s (%s, PID %d)\n", latestRun.Agent, latestRun.Status, latestRun.PID)
		if latestRun.SessionID != nil {
			fmt.Printf("Session:  %s\n", shortSessionID(*latestRun.SessionID))
		}
		fmt.Printf("Run log:  %s\n", latestRun.LogPath)
		if latestRun.Message != "" {
			fmt.Printf("Run note: %s\n", latestRun.Message)
		}
	}
	if t.Closed != nil {
		fmt.Printf("Closed:   %s\n", *t.Closed)
	}
	if t.DoneTS != nil {
		fmt.Printf("Finished: %s\n", time.Unix(*t.DoneTS, 0).In(config.Loc).Format("2006-01-02 15:04:05"))
	}
	if t.StartTS != nil && t.DoneTS != nil {
		dur := time.Duration(*t.DoneTS-*t.StartTS) * time.Second
		fmt.Printf("Duration: %s\n", dur.Round(time.Second))
	}
	if t.ClosedReason != nil {
		fmt.Printf("Reason:   %s\n", *t.ClosedReason)
	}
	if len(bindings) > 0 {
		fmt.Printf("\nSession Binding History (%d):\n", len(bindings))
		for _, binding := range bindings {
			state := "bound"
			if binding.UnboundAt != nil {
				state = binding.Reason
				if state == "" {
					state = "unbound"
				}
			}
			boundAt := time.Unix(binding.BoundAt, 0).In(config.Loc).Format("01-02 15:04")
			fmt.Printf("  %-8s %-7s %-10s %-11s %s\n", shortSessionID(binding.SessionID), emptyAs(binding.Agent, "agent"), state, boundAt, binding.Project)
		}
	}

	if len(boundSessions) > 0 {
		fmt.Printf("\nBound Sessions (%d):\n", len(boundSessions))
		var totalCost float64
		var totalQueries, totalTools int
		for _, s := range boundSessions {
			summary := s.Summary
			if summary == "" {
				if s.Indexed {
					summary = "(untitled session)"
				} else {
					summary = "session details not indexed"
				}
			}
			bindingLabel := "bound"
			if s.UnboundAt != nil {
				bindingLabel = emptyAs(s.Reason, "unbound")
			}
			if s.BindingCount > 1 {
				bindingLabel += fmt.Sprintf(" x%d", s.BindingCount)
			}
			fmt.Printf("  %s  %-8s %-16s Q:%-3d Tools:%-4d $%.4f  %s\n",
				s.ShortID, s.Agent, bindingLabel, s.Queries, s.ToolCalls, s.CostUSD, summary)
			totalCost += s.CostUSD
			totalQueries += s.Queries
			totalTools += s.ToolCalls
		}
		fmt.Printf("  %s\n", strings.Repeat("-", 60))
		fmt.Printf("  Total: %d sessions, %d queries, %d tool calls, $%.4f\n",
			len(boundSessions), totalQueries, totalTools, totalCost)
	}

	if docExists {
		fmt.Printf("\nDoc:      %s\n", docPath)
		if content, err := store.ReadTodoDoc(t.ID); err == nil {
			if logs := extractRecentLogs(content, 5); len(logs) > 0 {
				fmt.Println("\nRecent Progress:")
				for _, l := range logs {
					fmt.Printf("  %s\n", l)
				}
			}
		}
	}
	return nil
}
