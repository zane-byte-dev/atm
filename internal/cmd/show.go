package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
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
	sid := args[0]
	if showLastFlag < 0 {
		return fmt.Errorf("--last must be at least 0")
	}
	if showMaxCharsFlag < 0 {
		return fmt.Errorf("--max-chars must be at least 0")
	}
	if strings.TrimSpace(showTurnsFlag) != "" && showLastFlag > 0 {
		return fmt.Errorf("--turns and --last are mutually exclusive")
	}
	turnStart, turnEnd, err := parseTurnRange(showTurnsFlag)
	if err != nil {
		return err
	}

	return withDB(true, func(db *sql.DB) error {
		s, err := store.GetSession(db, sid)
		if err != nil {
			return fmt.Errorf("session not found: %s", sid)
		}

		// Q/A comes from the mirror, but thinking is only ever read back from the
		// transcript, and ATM keeps sessions after the agent rotates its logs
		// away. The extractors return nothing when the file is gone, so say so
		// rather than let it read as a session that never thought.
		var thinkingBlocks []parser.ThinkingBlock
		thinkingSourceMissing := false
		if showThinking && s.FilePath != "" {
			if _, statErr := os.Stat(s.FilePath); statErr != nil {
				thinkingSourceMissing = true
			} else {
				thinkingBlocks = extractSessionThinking(s.AgentKey, s.FilePath)
			}
		}

		// Claude Code stores its thinking blocks with the text stripped, keeping
		// only the signature, so "no thinking" is a property of the transcript
		// rather than of the request. Saying so beats rendering an empty section
		// that reads like a failed read.
		thinkingAbsent := showThinking && !thinkingSourceMissing && len(thinkingBlocks) == 0

		allQAs := buildSessionQAs(s, thinkingBlocks)
		totalTurns := len(s.Inputs)
		qas := selectSessionQAs(allQAs, turnStart, turnEnd, showLastFlag)
		rangeTruncated := len(qas) != len(allQAs)
		qas, contentTruncated := limitSessionQAChars(qas, showMaxCharsFlag)
		truncated := rangeTruncated || contentTruncated

		if jsonOutput {
			payload := map[string]any{
				"id":             s.FullID,
				"agent":          s.Agent,
				"project":        s.Project,
				"qa":             qas,
				"tools":          s.Tools,
				"total_turns":    totalTurns,
				"returned_turns": len(qas),
				"truncated":      truncated,
			}
			if thinkingSourceMissing {
				payload["thinking_source_missing"] = true
			}
			if thinkingAbsent {
				payload["thinking_absent"] = true
			}
			output.JSON(payload)
			return nil
		}

		fmt.Printf("[%s] %s  %s\n", s.Agent, s.Project, s.FullID)
		fmt.Println(strings.Repeat("=", 60))
		if thinkingSourceMissing {
			fmt.Printf("\n(thinking unavailable: %s is no longer on disk; the Q/A below comes from the ATM index)\n", s.FilePath)
		}
		if thinkingAbsent {
			fmt.Printf("\n(no thinking text in this transcript: %s records none)\n", s.Agent)
		}
		if truncated {
			if len(qas) == 0 {
				fmt.Printf("\n(showing 0 of %d turns)\n", totalTurns)
			} else {
				suffix := ""
				if contentTruncated {
					suffix = "; character budget reached"
				}
				fmt.Printf("\n(showing turns %d-%d of %d%s)\n",
					qas[0].Turn, qas[len(qas)-1].Turn, totalTurns,
					suffix)
			}
		}

		for _, qa := range qas {
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

		if len(s.Tools) > 0 {
			var pairs []string
			for k, v := range s.Tools {
				pairs = append(pairs, fmt.Sprintf("%s:%d", k, v))
			}
			sort.Strings(pairs)
			fmt.Printf("\nTools: %s\n", strings.Join(pairs, ", "))
		}
		return nil
	})
}

type sessionQA struct {
	Turn     int    `json:"turn"`
	Q        string `json:"q,omitempty"`
	A        string `json:"a,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

// extractSessionThinking picks the extractor that matches how the agent records
// its reasoning. Codex and Grok write OpenAI-style reasoning items, Pi and
// Claude write a `thinking` content block; reading every transcript as Claude's
// shape made two of the four agents look like they never thought.
func extractSessionThinking(agent, filePath string) []parser.ThinkingBlock {
	switch agent {
	case "pi":
		return parser.PiExtractThinking(filePath)
	case "codex", "grokbuild":
		return parser.ReasoningExtractThinking(filePath)
	default:
		return parser.ClaudeExtractThinking(filePath)
	}
}

func buildSessionQAs(s *store.ShowResult, thinkingBlocks []parser.ThinkingBlock) []sessionQA {
	qas := make([]sessionQA, 0, len(s.Inputs))
	thinkIdx := 0
	for i, inp := range s.Inputs {
		qa := sessionQA{Turn: i + 1, Q: cleanMsg(inp)}
		if i < len(s.Outputs) {
			qa.A = cleanMsg(s.Outputs[i])
			if showThinking {
				qa.Thinking, thinkIdx = collectTurnThinking(thinkingBlocks, thinkIdx, qa.A)
			}
		}
		if qa.Q == "" && qa.A == "" && qa.Thinking == "" {
			continue
		}
		qas = append(qas, qa)
	}
	return qas
}

// collectTurnThinking returns every thinking block belonging to one turn, and
// where the next turn should resume. Reasoning models emit a block per model
// response while a turn usually spans several of them — tool calls think without
// answering — so taking one block per turn attributed the tail of a session's
// reasoning to turns that never produced it. The turn's own answer marks its
// end; when no block claims that answer the single-block behaviour is kept so an
// unrecognised transcript still shows something rather than dumping the whole
// chain onto the first turn.
func collectTurnThinking(blocks []parser.ThinkingBlock, from int, answer string) (string, int) {
	if from >= len(blocks) {
		return "", from
	}
	end := -1
	if answer != "" {
		for index := from; index < len(blocks); index++ {
			if cleanMsg(blocks[index].Response) == answer {
				end = index
				break
			}
		}
	}
	if end < 0 {
		return blocks[from].Thinking, from + 1
	}
	parts := make([]string, 0, end-from+1)
	for _, block := range blocks[from : end+1] {
		if text := strings.TrimSpace(block.Thinking); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n"), end + 1
}

func parseTurnRange(value string) (int, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid --turns %q: use N or START-END", value)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("invalid --turns %q: use N or START-END", value)
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil || end < start {
			return 0, 0, fmt.Errorf("invalid --turns %q: use N or START-END", value)
		}
	}
	return start, end, nil
}

func selectSessionQAs(qas []sessionQA, start, end, last int) []sessionQA {
	if last > 0 {
		if last >= len(qas) {
			return qas
		}
		return qas[len(qas)-last:]
	}
	if start == 0 {
		return qas
	}
	selected := make([]sessionQA, 0, end-start+1)
	for _, qa := range qas {
		if qa.Turn >= start && qa.Turn <= end {
			selected = append(selected, qa)
		}
	}
	return selected
}

func limitSessionQAChars(qas []sessionQA, maxChars int) ([]sessionQA, bool) {
	if maxChars == 0 {
		return qas, false
	}
	remaining := maxChars
	limited := make([]sessionQA, 0, len(qas))
	truncated := false
	for _, original := range qas {
		if remaining == 0 {
			truncated = true
			break
		}
		qa := sessionQA{Turn: original.Turn}
		var fieldTruncated bool
		qa.Q, remaining, fieldTruncated = takeCharacterBudget(original.Q, remaining)
		truncated = truncated || fieldTruncated
		if remaining > 0 {
			qa.Thinking, remaining, fieldTruncated = takeCharacterBudget(original.Thinking, remaining)
			truncated = truncated || fieldTruncated
		} else if original.Thinking != "" || original.A != "" {
			truncated = true
		}
		if remaining > 0 {
			qa.A, remaining, fieldTruncated = takeCharacterBudget(original.A, remaining)
			truncated = truncated || fieldTruncated
		} else if original.A != "" {
			truncated = true
		}
		if qa.Q != "" || qa.A != "" || qa.Thinking != "" {
			limited = append(limited, qa)
		}
		if truncated && remaining == 0 {
			break
		}
	}
	if len(limited) < len(qas) {
		truncated = true
	}
	return limited, truncated
}

func takeCharacterBudget(value string, remaining int) (string, int, bool) {
	if value == "" {
		return "", remaining, false
	}
	runes := []rune(value)
	if len(runes) <= remaining {
		return value, remaining - len(runes), false
	}
	if remaining == 0 {
		return "", 0, true
	}
	if remaining == 1 {
		return "…", 0, true
	}
	return string(runes[:remaining-1]) + "…", 0, true
}
