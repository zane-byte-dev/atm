package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

var (
	todoLinkKindFlag     string
	todoLinkTitleFlag    string
	todoLinkRelationFlag string
)

func init() {
	todoLinkAddCmd.Flags().StringVar(&todoLinkKindFlag, "kind", "", "link kind, for example: cr, mr, pipeline, workitem, document")
	todoLinkAddCmd.Flags().StringVar(&todoLinkTitleFlag, "title", "", "human-readable link title")
	todoLinkAddCmd.Flags().StringVar(&todoLinkRelationFlag, "relation", "", "relationship to the todo, for example: tracks, blocks, evidence")
	todoLinkCmd.AddCommand(todoLinkAddCmd, todoLinkListCmd, todoLinkRemoveCmd)
	todoCmd.AddCommand(todoLinkCmd)
}

var todoLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage full-URL links attached to a todo",
	Args:  cobra.NoArgs, // unknown subcommand errors instead of silently showing help
	RunE:  showHelp,
}

var todoLinkAddCmd = &cobra.Command{
	Use:   "add <id> <url>",
	Short: "Attach a full URL to a todo",
	Args:  cobra.ExactArgs(2),
	RunE:  runTodoLinkAdd,
}

var todoLinkListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List URLs attached to a todo",
	Args:  cobra.ExactArgs(1),
	RunE:  runTodoLinkList,
}

var todoLinkRemoveCmd = &cobra.Command{
	Use:   "remove <id> <url>",
	Short: "Remove a URL from a todo",
	Args:  cobra.ExactArgs(2),
	RunE:  runTodoLinkRemove,
}

func runTodoLinkAdd(cmd *cobra.Command, args []string) error {
	cleanURL, err := normalizeTodoLinkURL(args[1])
	if err != nil {
		return err
	}
	link := store.TodoLink{
		URL:      cleanURL,
		Kind:     strings.TrimSpace(todoLinkKindFlag),
		Title:    strings.TrimSpace(todoLinkTitleFlag),
		Relation: strings.TrimSpace(todoLinkRelationFlag),
	}
	if link.Kind == "" {
		link.Kind = inferTodoLinkKind(cleanURL)
	}

	created := true
	_, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
		for i := range t.Links {
			if t.Links[i].URL != cleanURL {
				continue
			}
			created = false
			if link.Kind != "" {
				t.Links[i].Kind = link.Kind
			}
			if link.Title != "" {
				t.Links[i].Title = link.Title
			}
			if link.Relation != "" {
				t.Links[i].Relation = link.Relation
			}
			link = t.Links[i]
			return nil
		}
		t.Links = append(t.Links, link)
		return nil
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{"todo_id": t.ID, "created": created, "link": link})
		return nil
	}
	action := "Updated"
	if created {
		action = "Linked"
	}
	fmt.Printf("%s %s: %s\n", action, t.ID, link.URL)
	return nil
}

func runTodoLinkList(cmd *cobra.Command, args []string) error {
	_, t, err := loadTodoByID(args[0])
	if err != nil {
		return err
	}
	links := t.Links
	if links == nil {
		links = []store.TodoLink{}
	}
	if jsonOutput {
		output.JSON(links)
		return nil
	}
	if len(links) == 0 {
		fmt.Println("No links found.")
		return nil
	}
	for i, link := range links {
		label := link.Title
		if label == "" {
			label = link.URL
		}
		meta := []string{}
		if link.Kind != "" {
			meta = append(meta, link.Kind)
		}
		if link.Relation != "" {
			meta = append(meta, link.Relation)
		}
		if len(meta) > 0 {
			fmt.Printf("%d. %s [%s]\n   %s\n", i+1, label, strings.Join(meta, ", "), link.URL)
		} else {
			fmt.Printf("%d. %s\n", i+1, link.URL)
		}
	}
	return nil
}

func runTodoLinkRemove(cmd *cobra.Command, args []string) error {
	cleanURL, err := normalizeTodoLinkURL(args[1])
	if err != nil {
		return err
	}
	var removed store.TodoLink
	_, t, err := mutateTodo(args[0], func(t *store.Todo, _ *store.TodoFile, _ *workapp.Transaction) error {
		index := -1
		for i, link := range t.Links {
			if link.URL == cleanURL {
				index = i
				removed = link
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("link not found on todo %s", t.ID)
		}
		t.Links = append(t.Links[:index], t.Links[index+1:]...)
		return nil
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		output.JSON(map[string]any{"todo_id": t.ID, "removed": removed})
		return nil
	}
	fmt.Printf("Unlinked %s: %s\n", t.ID, removed.URL)
	return nil
}

func normalizeTodoLinkURL(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("link must be a complete http/https URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported link scheme %q (use http or https)", parsed.Scheme)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("link must not contain embedded credentials")
	}
	for key := range parsed.Query() {
		if sensitiveURLParameter(key) {
			return "", fmt.Errorf("link contains sensitive query parameter %q", key)
		}
	}
	fragment := strings.ToLower(parsed.Fragment)
	if index := strings.Index(raw, "#"); index >= 0 {
		fragment = strings.ToLower(raw[index+1:])
	}
	if strings.Contains(fragment, "token=") || strings.Contains(fragment, "password=") || strings.Contains(fragment, "signature=") {
		return "", fmt.Errorf("link fragment appears to contain credentials")
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String(), nil
}

func sensitiveURLParameter(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"token", "secret", "password", "passwd", "signature", "credential", "authorization", "api_key", "apikey", "access_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return lower == "auth"
}

func inferTodoLinkKind(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	switch {
	case strings.Contains(path, "merge_requests") || strings.Contains(path, "/pull/"):
		return "mr"
	case strings.Contains(path, "/cr/") || strings.Contains(path, "change-request"):
		return "cr"
	case strings.Contains(path, "pipeline"):
		return "pipeline"
	case strings.Contains(path, "workitem") || strings.Contains(path, "/issues/"):
		return "workitem"
	default:
		return ""
	}
}
