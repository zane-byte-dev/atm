package session

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

type ShowInput struct {
	SessionID       string `json:"session_id"`
	IncludeThinking bool   `json:"include_thinking,omitempty"`
	Turns           string `json:"turns,omitempty"`
	Last            int    `json:"last,omitempty"`
	MaxChars        int    `json:"max_chars,omitempty"`
	SyncBeforeRead  bool   `json:"sync_before_read,omitempty"`
}

type QA struct {
	Turn     int      `json:"turn"`
	Q        string   `json:"q,omitempty"`
	A        string   `json:"a,omitempty"`
	Progress []string `json:"progress,omitempty"`
	Thinking string   `json:"thinking,omitempty"`
}

type ShowResult struct {
	ID                    string         `json:"id"`
	Agent                 string         `json:"agent"`
	Project               string         `json:"project"`
	QA                    []QA           `json:"qa"`
	Tools                 map[string]int `json:"tools"`
	TotalTurns            int            `json:"total_turns"`
	ReturnedTurns         int            `json:"returned_turns"`
	Truncated             bool           `json:"truncated"`
	ContentTruncated      bool           `json:"-"`
	ThinkingSourceMissing bool           `json:"thinking_source_missing,omitempty"`
	ThinkingAbsent        bool           `json:"thinking_absent,omitempty"`
	TranscriptPath        string         `json:"-"`
	ResumeID              string         `json:"resume_id,omitempty"`
	RootSessionID         string         `json:"root_session_id,omitempty"`
	ParentSessionID       string         `json:"parent_session_id,omitempty"`
	AgentPath             string         `json:"agent_path,omitempty"`
	AgentNickname         string         `json:"agent_nickname,omitempty"`
	SubagentDepth         int            `json:"subagent_depth,omitempty"`
	IsSubagent            bool           `json:"is_subagent,omitempty"`
	ParserVersion         int            `json:"parser_version"`
	ContentState          string         `json:"content_state"`
	ResultStatus          string         `json:"result_status"`
	LatestProgress        string         `json:"latest_progress,omitempty"`
	FinalResult           string         `json:"final_result,omitempty"`
	Meta                  ReadMeta       `json:"meta"`
}

func (service Service) Show(ctx context.Context, input ShowInput) (ShowResult, error) {
	if err := contextError(ctx); err != nil {
		return ShowResult{}, err
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Turns = strings.TrimSpace(input.Turns)
	if input.SessionID == "" {
		return ShowResult{}, invalidArgument("session id must not be empty", "session_id", input.SessionID)
	}
	if input.Last < 0 {
		return ShowResult{}, invalidArgument("last must be at least 0", "last", input.Last)
	}
	if input.MaxChars < 0 {
		return ShowResult{}, invalidArgument("max chars must be at least 0", "max_chars", input.MaxChars)
	}
	if input.Turns != "" && input.Last > 0 {
		return ShowResult{}, invalidArgument("turns and last are mutually exclusive", "last", input.Last)
	}
	turnStart, turnEnd, err := parseTurnRange(input.Turns)
	if err != nil {
		return ShowResult{}, err
	}

	db, meta, err := service.openRead(ctx, input.SyncBeforeRead)
	if err != nil {
		return ShowResult{}, err
	}
	defer db.Close()
	stored, err := store.GetSession(db, input.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ShowResult{}, sessionNotFound(input.SessionID, err)
		}
		return ShowResult{}, unavailable("show session", err)
	}

	var thinkingBlocks []parser.ThinkingBlock
	thinkingSourceMissing := false
	if input.IncludeThinking && stored.FilePath != "" {
		if _, statErr := os.Stat(stored.FilePath); statErr != nil {
			thinkingSourceMissing = true
		} else {
			thinkingBlocks = extractThinking(stored.AgentKey, stored.FilePath)
		}
	}
	thinkingAbsent := input.IncludeThinking && !thinkingSourceMissing && len(thinkingBlocks) == 0

	allQAs := buildQAs(stored, thinkingBlocks, input.IncludeThinking)
	qas := selectQAs(allQAs, turnStart, turnEnd, input.Last)
	rangeTruncated := len(qas) != len(allQAs)
	qas, contentTruncated := limitQAChars(qas, input.MaxChars)
	if err := contextError(ctx); err != nil {
		return ShowResult{}, err
	}
	return ShowResult{
		ID: stored.FullID, Agent: stored.Agent,
		Project: stored.Project, QA: qas, Tools: stored.Tools,
		TotalTurns: len(allQAs), ReturnedTurns: len(qas),
		Truncated: rangeTruncated || contentTruncated, ContentTruncated: contentTruncated,
		ThinkingSourceMissing: thinkingSourceMissing, ThinkingAbsent: thinkingAbsent,
		TranscriptPath: stored.FilePath, ResumeID: stored.ResumeID,
		RootSessionID: stored.RootSessionID, ParentSessionID: stored.ParentSessionID,
		AgentPath: stored.AgentPath, AgentNickname: stored.AgentNickname,
		SubagentDepth: stored.SubagentDepth, IsSubagent: stored.IsSubagent,
		ParserVersion: stored.ParserVersion, ContentState: stored.ContentState,
		ResultStatus: stored.ResultStatus, LatestProgress: stored.LatestProgress,
		FinalResult: stored.FinalResult, Meta: meta,
	}, nil
}

func extractThinking(agent, filePath string) []parser.ThinkingBlock {
	switch agent {
	case "pi":
		return parser.PiExtractThinking(filePath)
	case "codex", "grokbuild":
		return parser.ReasoningExtractThinking(filePath)
	default:
		return parser.ClaudeExtractThinking(filePath)
	}
}

func buildQAs(stored *store.ShowResult, thinkingBlocks []parser.ThinkingBlock, includeThinking bool) []QA {
	qas := make([]QA, 0, len(stored.Turns))
	thinkingIndex := 0
	for index, turn := range stored.Turns {
		qa := QA{Turn: index + 1, Q: cleanMessage(turn.Question)}
		for _, progress := range turn.Progress {
			if cleaned := cleanMessage(progress); cleaned != "" {
				qa.Progress = append(qa.Progress, cleaned)
			}
		}
		answer := turn.Answer
		if turn.Final != "" {
			answer = turn.Final
		}
		qa.A = cleanMessage(answer)
		if includeThinking && qa.A != "" {
			qa.Thinking, thinkingIndex = collectTurnThinking(thinkingBlocks, thinkingIndex, qa.A)
		}
		if qa.Q == "" && qa.A == "" && len(qa.Progress) == 0 && qa.Thinking == "" {
			continue
		}
		qas = append(qas, qa)
	}
	return qas
}

func collectTurnThinking(blocks []parser.ThinkingBlock, from int, answer string) (string, int) {
	if from >= len(blocks) {
		return "", from
	}
	end := -1
	if answer != "" {
		for index := from; index < len(blocks); index++ {
			if cleanMessage(blocks[index].Response) == answer {
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
	if value == "" {
		return 0, 0, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, invalidArgument(
			"invalid turns: use N or START-END", "turns", value,
		)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 1 {
		return 0, 0, invalidArgument(
			"invalid turns: use N or START-END", "turns", value,
		)
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
		if err != nil || end < start {
			return 0, 0, invalidArgument(
				"invalid turns: use N or START-END", "turns", value,
			)
		}
	}
	return start, end, nil
}

func selectQAs(qas []QA, start, end, last int) []QA {
	if last > 0 {
		if last >= len(qas) {
			return qas
		}
		return qas[len(qas)-last:]
	}
	if start == 0 {
		return qas
	}
	selected := make([]QA, 0, end-start+1)
	for _, qa := range qas {
		if qa.Turn >= start && qa.Turn <= end {
			selected = append(selected, qa)
		}
	}
	return selected
}

func limitQAChars(qas []QA, maxChars int) ([]QA, bool) {
	if maxChars == 0 {
		return qas, false
	}
	remaining := maxChars
	limited := make([]QA, 0, len(qas))
	truncated := false
	for _, original := range qas {
		if remaining == 0 {
			truncated = true
			break
		}
		qa := QA{Turn: original.Turn}
		var fieldTruncated bool
		qa.Q, remaining, fieldTruncated = takeCharacterBudget(original.Q, remaining)
		truncated = truncated || fieldTruncated
		for _, progress := range original.Progress {
			if remaining == 0 {
				truncated = true
				break
			}
			var limitedProgress string
			limitedProgress, remaining, fieldTruncated = takeCharacterBudget(progress, remaining)
			truncated = truncated || fieldTruncated
			if limitedProgress != "" {
				qa.Progress = append(qa.Progress, limitedProgress)
			}
		}
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
