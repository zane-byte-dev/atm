package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
)

const (
	defaultSearchLimit   = 50
	defaultSearchSnippet = 400
)

var (
	searchLimitFlag   int
	searchProjectFlag string
	searchSinceFlag   string
	searchDaysFlag    int
	searchRoleFlag    string
	searchSnippetFlag int
	searchQueryFlag   string
)

func init() {
	searchCmd.Flags().IntVar(&searchLimitFlag, "limit", defaultSearchLimit, "maximum number of message matches to return")
	searchCmd.Flags().StringVar(&searchProjectFlag, "project", "", "filter by project name (case-insensitive substring)")
	searchCmd.Flags().StringVar(&searchSinceFlag, "since", "", "search messages since RFC3339 timestamp or YYYY-MM-DD")
	searchCmd.Flags().IntVar(&searchDaysFlag, "days", 0, "search today plus the previous N-1 days")
	searchCmd.Flags().StringVar(&searchRoleFlag, "role", "", "filter by message role: user or assistant")
	searchCmd.Flags().IntVar(&searchSnippetFlag, "snippet", defaultSearchSnippet, "maximum characters returned around each match")
	searchCmd.Flags().StringVar(&searchQueryFlag, "query", "", "search text (alias for the positional keyword)")
	searchCmd.MarkFlagsMutuallyExclusive("since", "days")
	sessionCmd.AddCommand(searchCmd)
}

var searchCmd = &cobra.Command{
	Use:   "search [keyword]",
	Short: "Search all AI session history",
	Example: `  atm session search "release check"
  atm session search --query "release check" --project atm --days 7 --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSearch,
}

func runSearch(cmd *cobra.Command, args []string) error {
	keyword := strings.TrimSpace(searchQueryFlag)
	if len(args) > 0 {
		if keyword != "" {
			return fmt.Errorf("pass the search text either as [keyword] or --query, not both")
		}
		keyword = args[0]
	}
	if strings.TrimSpace(keyword) == "" {
		return fmt.Errorf("requires a search keyword: use `atm session search <keyword>` or `atm session search --query <keyword>`")
	}
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	result, err := currentSessionService().Search(cmd.Context(), sessionapp.SearchInput{
		Keyword: keyword, Agent: agent, Project: searchProjectFlag,
		Since: searchSinceFlag, Days: searchDaysFlag, Role: searchRoleFlag,
		Limit: searchLimitFlag, Snippet: searchSnippetFlag, SyncBeforeRead: syncBeforeRead,
	})
	if err != nil {
		return err
	}
	renderSessionReadMeta(result.Meta)

	if jsonOutput {
		payload := struct {
			SchemaVersion int                    `json:"schema_version"`
			Keyword       string                 `json:"keyword"`
			Total         int                    `json:"total"`
			Returned      int                    `json:"returned"`
			Truncated     bool                   `json:"truncated"`
			Limit         int                    `json:"limit"`
			Matches       []sessionapp.SearchHit `json:"matches"`
		}{
			SchemaVersion: sessionCLIOutputSchemaVersion,
			Keyword:       result.Keyword, Total: result.Total, Returned: result.Returned,
			Truncated: result.Truncated, Limit: result.Limit, Matches: result.Matches,
		}
		output.JSON(payload)
		return nil
	}

	if result.Truncated {
		fmt.Printf("Search: \"%s\"  (%d of %d matches; raise --limit for more)\n",
			result.Keyword, result.Returned, result.Total)
	} else {
		fmt.Printf("Search: \"%s\"  (%d matches)\n", result.Keyword, result.Total)
	}
	fmt.Println(strings.Repeat("=", 60))
	if len(result.Matches) == 0 {
		fmt.Println("\nNo matches found.")
		return nil
	}

	keys := make([]string, 0, len(result.Matches))
	grouped := make(map[string][]sessionapp.SearchHit, len(result.Matches))
	for _, hit := range result.Matches {
		created := hit.IndexedCreated
		if created == "" {
			created = hit.CreatedAt
		}
		key := fmt.Sprintf("%s | %s | %s | %s", hit.ShortID, created, hit.Agent, hit.Project)
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], hit)
	}
	for _, key := range keys {
		group := grouped[key]
		shortID := strings.SplitN(key, " | ", 2)[0]
		fmt.Printf("\n%s  (%d matches)  → atm session show %s\n", key, len(group), shortID)
		fmt.Println(strings.Repeat("-", 50))
		seen := map[string]bool{}
		for _, match := range group {
			firstLine := strings.Join(strings.Fields(match.Content), " ")
			dedupKey := truncLine(firstLine, 60)
			if seen[dedupKey] {
				continue
			}
			seen[dedupKey] = true
			roleTag := "A"
			if match.Role == "user" {
				roleTag = "Q"
			}
			fmt.Printf("  [%s] %s\n", roleTag, firstLine)
		}
	}
	return nil
}
