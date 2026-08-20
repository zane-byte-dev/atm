package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zane-byte-dev/atm/internal/output"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
)

var (
	showThinking     bool
	showTurnsFlag    string
	showLastFlag     int
	showMaxCharsFlag int
)

func init() {
	showCmd.Flags().BoolVar(&showThinking, "thinking", false, "include model thinking blocks")
	showCmd.Flags().StringVar(&showTurnsFlag, "turns", "", "show an inclusive turn range such as 1-10")
	showCmd.Flags().IntVar(&showLastFlag, "last", 0, "show only the last N turns")
	showCmd.Flags().IntVar(&showMaxCharsFlag, "max-chars", 0, "maximum Q/A/thinking characters to return (0 is unlimited)")
	showCmd.MarkFlagsMutuallyExclusive("turns", "last")
	sessionCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show <session-id>",
	Short: "Show full Q/A for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	result, err := currentSessionService().Show(cmd.Context(), sessionapp.ShowInput{
		SessionID: args[0], IncludeThinking: showThinking, Turns: showTurnsFlag,
		Last: showLastFlag, MaxChars: showMaxCharsFlag, SyncBeforeRead: syncBeforeRead,
	})
	if err != nil {
		return err
	}
	renderSessionReadMeta(result.Meta)

	if jsonOutput {
		payload := struct {
			ID                    string          `json:"id"`
			Agent                 string          `json:"agent"`
			Project               string          `json:"project"`
			QA                    []sessionapp.QA `json:"qa"`
			Tools                 map[string]int  `json:"tools"`
			TotalTurns            int             `json:"total_turns"`
			ReturnedTurns         int             `json:"returned_turns"`
			Truncated             bool            `json:"truncated"`
			ThinkingSourceMissing bool            `json:"thinking_source_missing,omitempty"`
			ThinkingAbsent        bool            `json:"thinking_absent,omitempty"`
		}{
			ID: result.ID, Agent: result.Agent, Project: result.Project,
			QA: result.QA, Tools: result.Tools, TotalTurns: result.TotalTurns,
			ReturnedTurns: result.ReturnedTurns, Truncated: result.Truncated,
			ThinkingSourceMissing: result.ThinkingSourceMissing,
			ThinkingAbsent:        result.ThinkingAbsent,
		}
		output.JSON(payload)
		return nil
	}

	fmt.Printf("[%s] %s  %s\n", result.Agent, result.Project, result.ID)
	fmt.Println(strings.Repeat("=", 60))
	if result.ThinkingSourceMissing {
		fmt.Printf("\n(thinking unavailable: %s is no longer on disk; the Q/A below comes from the ATM index)\n", result.TranscriptPath)
	}
	if result.ThinkingAbsent {
		fmt.Printf("\n(no thinking text in this transcript: %s records none)\n", result.Agent)
	}
	if result.Truncated {
		if len(result.QA) == 0 {
			fmt.Printf("\n(showing 0 of %d turns)\n", result.TotalTurns)
		} else {
			suffix := ""
			if result.ContentTruncated {
				suffix = "; character budget reached"
			}
			fmt.Printf("\n(showing turns %d-%d of %d%s)\n",
				result.QA[0].Turn, result.QA[len(result.QA)-1].Turn, result.TotalTurns, suffix)
		}
	}
	for _, qa := range result.QA {
		if qa.Q != "" {
			fmt.Printf("\nQ: %s\n", qa.Q)
		}
		if qa.Thinking != "" {
			fmt.Printf("\n💭 %s\n", qa.Thinking)
		}
		if qa.A != "" {
			fmt.Printf("\nA: %s\n", qa.A)
		}
	}
	if len(result.Tools) > 0 {
		pairs := make([]string, 0, len(result.Tools))
		for name, count := range result.Tools {
			pairs = append(pairs, fmt.Sprintf("%s:%d", name, count))
		}
		sort.Strings(pairs)
		fmt.Printf("\nTools: %s\n", strings.Join(pairs, ", "))
	}
	return nil
}
