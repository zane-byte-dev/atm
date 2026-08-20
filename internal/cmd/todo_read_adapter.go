package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func runTodoList(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.List(cmd.Context(), todoReadCall("todo-list"), workapp.ListInput{
		Status: todoStatusFlag, Priority: todoListPriorityFlag, Project: todoProjectFlag,
		Query: todoListQueryFlag, Creator: todoListCreatorFlag,
		Limit: todoListLimitFlag, Offset: todoListOffsetFlag,
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		if result.Kind == workapp.ListKindArchived {
			output.JSON(result.Archived)
		} else {
			output.JSON(result.Todos)
		}
		return nil
	}
	if result.Kind == workapp.ListKindArchived {
		return printArchivedTodoList(result.Archived)
	}
	if len(result.Todos) == 0 {
		fmt.Println("No todos found.")
		return nil
	}
	fmt.Printf("  %-6s %-4s %-12s %-12s %-10s %-16s %s\n", "ID", "Pri", "Status", "Created", "Creator", "Project", "Title")
	fmt.Printf("  %-6s %-4s %-12s %-12s %-10s %-16s %s\n",
		output.Dashes(6, 4, 12, 12, 10, 16, 30)...)
	for _, todo := range result.Todos {
		id := todo.ID
		if result.DocumentExists[todo.ID] {
			id += "*"
		}
		fmt.Printf("  %-6s %-4s %-12s %-12s %-10s %-16s %s\n", id, todo.Priority, todo.Status, todo.Created,
			emptyAs(todo.Creator, "-"), todo.Project, todo.Title)
	}
	return nil
}

func printArchivedTodoList(todos []workapp.ArchivedTodo) error {
	if len(todos) == 0 {
		fmt.Println("No archived todos.")
		return nil
	}
	fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n", "ID", "Pri", "Status", "Created", "Archived", "Creator", "Project", "Title")
	fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n",
		output.Dashes(6, 4, 8, 12, 12, 10, 16, 30)...)
	for _, todo := range todos {
		archivedOn := time.Unix(todo.ArchivedAt, 0).In(config.Loc).Format("2006-01-02")
		fmt.Printf("  %-6s %-4s %-8s %-12s %-12s %-10s %-16s %s\n",
			todo.ID, todo.Priority, todo.Status, todo.Created, archivedOn,
			emptyAs(todo.Creator, "-"), todo.Project, todo.Title)
	}
	return nil
}

func runTodoShow(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.Show(cmd.Context(), todoReadCall("todo-show"), workapp.ShowInput{TodoID: todoReadID(args)})
	if err != nil {
		return err
	}
	if jsonOutput {
		return printTodoShowJSON(result)
	}
	printTodoShowText(result)
	return nil
}

func printTodoShowJSON(result workapp.ShowResult) error {
	encodedTodo, err := json.Marshal(result.Todo)
	if err != nil {
		return fmt.Errorf("encode todo: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(encodedTodo, &out); err != nil {
		return fmt.Errorf("build todo response: %w", err)
	}
	out["todo"] = result.Todo
	out["doc_path"] = result.Document.Path
	out["doc_exists"] = result.Document.Exists
	if len(result.Bindings) > 0 {
		out["bindings"] = result.Bindings
	}
	if len(result.Sessions) > 0 {
		out["sessions"] = result.Sessions
		out["summary"] = result.Summary
	}
	if result.LatestRun != nil {
		out["latest_run"] = result.LatestRun
	}
	if result.LatestPlan != nil {
		out["latest_plan"] = result.LatestPlan
	}
	output.JSON(out)
	return nil
}

func printTodoShowText(result workapp.ShowResult) {
	todo := result.Todo
	fmt.Printf("ID:       %s\n", todo.ID)
	fmt.Printf("Title:    %s\n", todo.Title)
	fmt.Printf("Priority: %s\n", todo.Priority)
	fmt.Printf("Status:   %s\n", todo.Status)
	if len(todo.Tags) > 0 {
		fmt.Printf("Tags:     %s\n", strings.Join(todo.Tags, ", "))
	}
	if todo.WakeCondition != "" {
		fmt.Printf("Wake:     %s\n", todo.WakeCondition)
	}
	if todo.ReviewAt != "" {
		fmt.Printf("Review:   %s\n", todo.ReviewAt)
	}
	if todo.MaintenanceLimit > 0 {
		fmt.Printf("Limit:    %d\n", todo.MaintenanceLimit)
	}
	if todo.Project != "" {
		fmt.Printf("Project:  %s\n", todo.Project)
	}
	fmt.Printf("Created:  %s\n", todo.Created)
	if todo.Creator != "" {
		fmt.Printf("Creator:  %s\n", displayTodoCreator(todo.Creator))
	}
	if todo.Source != "" {
		fmt.Printf("Source:   %s\n", todo.Source)
	}
	if todo.Description != "" {
		fmt.Printf("Desc:     %s\n", todo.Description)
	}
	if plan := result.LatestPlan; plan != nil {
		fmt.Printf("Plan:     revision %d", plan.Revision)
		if plan.Explanation != "" {
			fmt.Printf(" — %s", strings.ReplaceAll(plan.Explanation, "\n", " "))
		}
		fmt.Println()
		for _, item := range plan.Items {
			fmt.Printf("  %-11s %s\n", item.Status, strings.ReplaceAll(item.Step, "\n", " "))
		}
	}
	if len(todo.Links) > 0 {
		fmt.Println("Links:")
		for _, link := range todo.Links {
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
	if len(todo.Images) > 0 {
		fmt.Println("Images:")
		for _, image := range todo.Images {
			fmt.Printf("  %s (%s, %d bytes) — %s\n", image.Name, image.MediaType, image.SizeBytes, image.Path)
		}
	}
	if todo.StartTS != nil {
		fmt.Printf("Started:  %s\n", time.Unix(*todo.StartTS, 0).In(config.Loc).Format("2006-01-02 15:04:05"))
	}
	if result.LatestRun != nil {
		run := result.LatestRun
		fmt.Printf("Agent:    %s (%s, PID %d)\n", run.Agent, run.Status, run.PID)
		if run.SessionID != nil {
			fmt.Printf("Session:  %s\n", shortSessionID(*run.SessionID))
		}
		fmt.Printf("Run log:  %s\n", run.LogPath)
		if run.Message != "" {
			fmt.Printf("Run note: %s\n", run.Message)
		}
	}
	if todo.Closed != nil {
		fmt.Printf("Closed:   %s\n", *todo.Closed)
	}
	if todo.DoneTS != nil {
		fmt.Printf("Finished: %s\n", time.Unix(*todo.DoneTS, 0).In(config.Loc).Format("2006-01-02 15:04:05"))
	}
	if todo.StartTS != nil && todo.DoneTS != nil {
		duration := time.Duration(*todo.DoneTS-*todo.StartTS) * time.Second
		fmt.Printf("Duration: %s\n", duration.Round(time.Second))
	}
	if todo.ClosedReason != nil {
		fmt.Printf("Reason:   %s\n", *todo.ClosedReason)
	}
	if len(result.Bindings) > 0 {
		fmt.Printf("\nSession Binding History (%d):\n", len(result.Bindings))
		for _, binding := range result.Bindings {
			state := "bound"
			if binding.UnboundAt != nil {
				state = emptyAs(binding.Reason, "unbound")
			}
			boundAt := time.Unix(binding.BoundAt, 0).In(config.Loc).Format("01-02 15:04")
			fmt.Printf("  %-8s %-7s %-10s %-11s %s\n", shortSessionID(binding.SessionID), emptyAs(binding.Agent, "agent"), state, boundAt, binding.Project)
		}
	}
	if len(result.Sessions) > 0 {
		fmt.Printf("\nBound Sessions (%d):\n", len(result.Sessions))
		for _, session := range result.Sessions {
			summary := session.Summary
			if summary == "" {
				if session.Indexed {
					summary = "(untitled session)"
				} else {
					summary = "session details not indexed"
				}
			}
			bindingLabel := "bound"
			if session.UnboundAt != nil {
				bindingLabel = emptyAs(session.Reason, "unbound")
			}
			if session.BindingCount > 1 {
				bindingLabel += fmt.Sprintf(" x%d", session.BindingCount)
			}
			fmt.Printf("  %s  %-8s %-16s Q:%-3d Tools:%-4d $%.4f  %s\n",
				session.ShortID, session.Agent, bindingLabel, session.Queries, session.ToolCalls, session.CostUSD, summary)
		}
		fmt.Printf("  %s\n", strings.Repeat("-", 60))
		if summary := result.Summary; summary != nil {
			fmt.Printf("  Total: %d sessions, %d queries, %d tool calls, $%.4f\n",
				summary.Sessions, summary.Queries, summary.ToolCalls, summary.CostUSD)
		}
	}
	if result.Document.Exists {
		fmt.Printf("\nDoc:      %s\n", result.Document.Path)
		if len(result.Document.RecentProgress) > 0 {
			fmt.Println("\nRecent Progress:")
			for _, line := range result.Document.RecentProgress {
				fmt.Printf("  %s\n", line)
			}
		}
	}
}

func runTodoDoc(cmd *cobra.Command, args []string) error {
	result, err := workapp.Default.Doc(cmd.Context(), todoReadCall("todo-doc"), workapp.DocInput{
		TodoID: todoReadID(args), Initialize: todoDocInitFlag,
	})
	if err != nil {
		return err
	}
	if todoDocInitFlag {
		if jsonOutput {
			output.JSON(map[string]any{"success": true, "path": result.Path})
		} else {
			fmt.Printf("Created %s\n", result.Path)
		}
		return nil
	}
	if jsonOutput {
		output.JSON(map[string]any{"path": result.Path, "exists": result.Exists, "content": result.Content})
		return nil
	}
	fmt.Print(result.Content)
	return nil
}

func todoMatchesQuery(todo workapp.Todo, rawQuery string) bool {
	return workapp.QueryRelevance(todo, rawQuery) >= 0
}

func todoQueryRelevance(todo workapp.Todo, rawQuery string) int {
	return workapp.QueryRelevance(todo, rawQuery)
}

func todoReadID(args []string) string {
	if len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "current") {
		return ""
	}
	return args[0]
}

func todoReadCall(scope string) application.Call {
	sessionID, _ := resolveSessionID(false)
	return cliApplicationCall(scope, sessionID)
}

func displayTodoCreator(creator string) string {
	switch creator {
	case "me":
		if name := strings.TrimSpace(config.OwnerName); name != "" {
			return name + "（我）"
		}
		return "我"
	case "collect":
		return "收集"
	default:
		return creator
	}
}
