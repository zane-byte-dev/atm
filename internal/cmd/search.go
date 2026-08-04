package cmd

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
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
)

func init() {
	searchCmd.Flags().IntVar(&searchLimitFlag, "limit", defaultSearchLimit, "maximum number of message matches to return")
	searchCmd.Flags().StringVar(&searchProjectFlag, "project", "", "filter by project name (substring match)")
	searchCmd.Flags().StringVar(&searchSinceFlag, "since", "", "search messages since RFC3339 timestamp or YYYY-MM-DD")
	searchCmd.Flags().IntVar(&searchDaysFlag, "days", 0, "search today plus the previous N-1 days")
	searchCmd.Flags().StringVar(&searchRoleFlag, "role", "", "filter by message role: user or assistant")
	searchCmd.Flags().IntVar(&searchSnippetFlag, "snippet", defaultSearchSnippet, "maximum characters returned around each match")
	searchCmd.MarkFlagsMutuallyExclusive("since", "days")
	sessionCmd.AddCommand(searchCmd)
}

var searchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search all AI session history",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func runSearch(cmd *cobra.Command, args []string) error {
	keyword := args[0]
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	if searchLimitFlag < 1 {
		return fmt.Errorf("--limit must be at least 1")
	}
	if searchSnippetFlag < 1 {
		return fmt.Errorf("--snippet must be at least 1")
	}
	if strings.TrimSpace(searchSinceFlag) != "" && searchDaysFlag != 0 {
		return fmt.Errorf("--since and --days are mutually exclusive")
	}
	role := strings.ToLower(strings.TrimSpace(searchRoleFlag))
	if role != "" && role != "user" && role != "assistant" {
		return fmt.Errorf("invalid --role %q: use user or assistant", searchRoleFlag)
	}
	var sinceTS int64
	if searchSinceFlag != "" {
		since, err := parseSessionSince(searchSinceFlag)
		if err != nil {
			return err
		}
		sinceTS = since.Unix()
	} else if searchDaysFlag != 0 {
		if searchDaysFlag < 1 {
			return fmt.Errorf("--days must be at least 1")
		}
		sinceTS = startOfDayWindow(time.Now().In(config.Loc), searchDaysFlag).Unix()
	}

	return withDB(true, func(db *sql.DB) error {
		matches, err := store.SearchMessagesWithOptions(db, keyword, store.SearchOptions{
			Agent:   agent,
			Project: strings.TrimSpace(searchProjectFlag),
			Role:    role,
			SinceTS: sinceTS,
			Limit:   searchLimitFlag,
			// Boilerplate the reader never asked for should not consume the page,
			// and it should not inflate the total either.
			Keep: func(content string) bool { return cleanMsg(content) != "" },
		})
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}
		results := matches.Hits

		if jsonOutput {
			type jsonHit struct {
				ID               string `json:"id"`
				ShortID          string `json:"short_id"`
				Agent            string `json:"agent"`
				Project          string `json:"project"`
				CreatedAt        string `json:"created_at"`
				Role             string `json:"role"`
				Content          string `json:"content"`
				SnippetTruncated bool   `json:"snippet_truncated,omitempty"`
			}
			// An envelope, not a bare array: a caller that reads len(matches) off
			// an array has no way to learn the limit hid the other 1070 hits.
			type searchPayload struct {
				Keyword   string    `json:"keyword"`
				Total     int       `json:"total"`
				Returned  int       `json:"returned"`
				Truncated bool      `json:"truncated"`
				Limit     int       `json:"limit"`
				Matches   []jsonHit `json:"matches"`
			}
			hits := make([]jsonHit, 0, len(results))
			for _, r := range results {
				snippet, truncated := matchSnippet(cleanMsg(r.Content), keyword, searchSnippetFlag)
				hits = append(hits, jsonHit{
					ID:               r.FullID,
					ShortID:          r.ShortID,
					Agent:            r.Agent,
					Project:          r.Project,
					CreatedAt:        searchCreatedAt(r),
					Role:             r.Role,
					Content:          snippet,
					SnippetTruncated: truncated,
				})
			}
			output.JSON(searchPayload{
				Keyword:   keyword,
				Total:     matches.Total,
				Returned:  len(hits),
				Truncated: matches.Truncated,
				Limit:     searchLimitFlag,
				Matches:   hits,
			})
			return nil
		}

		if matches.Truncated {
			fmt.Printf("Search: \"%s\"  (%d of %d matches; raise --limit for more)\n", keyword, len(results), matches.Total)
		} else {
			fmt.Printf("Search: \"%s\"  (%d matches)\n", keyword, matches.Total)
		}
		fmt.Println(strings.Repeat("=", 60))

		if len(results) == 0 {
			fmt.Println("\nNo matches found.")
			return nil
		}

		var keys []string
		grouped := map[string][]store.SearchHit{}
		for _, r := range results {
			key := fmt.Sprintf("%s | %s | %s | %s", r.ShortID, r.CreatedAt, r.Agent, r.Project)
			if _, ok := grouped[key]; !ok {
				keys = append(keys, key)
			}
			grouped[key] = append(grouped[key], r)
		}

		for _, key := range keys {
			group := grouped[key]
			shortID := strings.SplitN(key, " | ", 2)[0]
			fmt.Printf("\n%s  (%d matches)  → atm session show %s\n", key, len(group), shortID)
			fmt.Println(strings.Repeat("-", 50))
			seen := map[string]bool{}
			for _, m := range group {
				snippet, _ := matchSnippet(cleanMsg(m.Content), keyword, searchSnippetFlag)
				firstLine := strings.Join(strings.Fields(snippet), " ")
				dedupKey := firstLine
				dedupKey = truncLine(dedupKey, 60)
				if seen[dedupKey] {
					continue
				}
				seen[dedupKey] = true
				roleTag := "A"
				if m.Role == "user" {
					roleTag = "Q"
				}
				fmt.Printf("  [%s] %s\n", roleTag, firstLine)
			}
		}
		return nil
	})
}

func searchCreatedAt(hit store.SearchHit) string {
	if hit.CreatedTS > 0 {
		return time.Unix(hit.CreatedTS, 0).In(config.Loc).Format(time.RFC3339)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, hit.CreatedAt); err == nil {
			return parsed.Format(time.RFC3339)
		}
	}
	return ""
}

func matchSnippet(content, keyword string, maxChars int) (string, bool) {
	contentRunes := []rune(content)
	if len(contentRunes) <= maxChars {
		return content, false
	}
	if maxChars == 1 {
		return "…", true
	}

	lowerContent := []rune(strings.ToLower(content))
	lowerKeyword := []rune(strings.ToLower(keyword))
	matchAt := runeSliceIndex(lowerContent, lowerKeyword)
	if matchAt < 0 {
		matchAt = 0
	}
	center := matchAt + len(lowerKeyword)/2
	start := center - maxChars/2
	if start < 0 {
		start = 0
	}
	end := start + maxChars
	if end > len(contentRunes) {
		end = len(contentRunes)
		start = end - maxChars
	}

	for {
		indicators := 0
		if start > 0 {
			indicators++
		}
		if end < len(contentRunes) {
			indicators++
		}
		allowed := maxChars - indicators
		if end-start <= allowed {
			break
		}
		start = center - allowed/2
		if start < 0 {
			start = 0
		}
		end = start + allowed
		if end > len(contentRunes) {
			end = len(contentRunes)
			start = end - allowed
		}
	}

	var snippet strings.Builder
	if start > 0 {
		snippet.WriteRune('…')
	}
	snippet.WriteString(string(contentRunes[start:end]))
	if end < len(contentRunes) {
		snippet.WriteRune('…')
	}
	return snippet.String(), true
}

func runeSliceIndex(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		matched := true
		for offset := range needle {
			if haystack[start+offset] != needle[offset] {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}
