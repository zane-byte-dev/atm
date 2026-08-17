package parser

import (
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// ReasoningExtractThinking reads the thinking chain out of the two transcripts
// that record it as OpenAI-style reasoning items with a `summary` array: Codex
// wraps each record in `payload`, Grok writes the same shape at the top level.
// Neither is readable by ClaudeExtractThinking, which looks for a `thinking`
// content block, so without this both agents present an empty thinking view even
// though their transcripts carry the reasoning in full.
//
// Reasoning items accumulate until an assistant message with visible text closes
// them, so a returned block holds every thought that preceded that response.
// Items whose summary is empty are skipped rather than emitted blank: both CLIs
// also store an encrypted-only copy of reasoning they will not surface.
func ReasoningExtractThinking(fp string) []ThinkingBlock {
	var blocks []ThinkingBlock
	var pending []string
	flush := func(response string) {
		if len(pending) == 0 {
			return
		}
		blocks = append(blocks, ThinkingBlock{
			Thinking: strings.Join(pending, "\n\n"),
			Response: response,
		})
		pending = nil
	}
	scanJSONL(fp, func(record map[string]any) bool {
		item := record
		if payload := config.GetMap(record, "payload"); len(payload) > 0 {
			item = payload
		}
		switch config.GetStr(item, "type") {
		case "reasoning":
			if text := reasoningSummaryText(item); text != "" {
				pending = append(pending, text)
			}
		case "message", "assistant":
			if role := config.GetStr(item, "role"); role != "" && role != "assistant" {
				return true
			}
			if text := assistantVisibleText(item); text != "" {
				flush(text)
			}
		}
		return true
	})
	// A trailing turn that ended on a tool call still thought; keep it rather
	// than dropping the reasoning that led to the last action.
	flush("")
	return blocks
}

func reasoningSummaryText(item map[string]any) string {
	var parts []string
	for _, entry := range config.GetSlice(item, "summary") {
		switch value := entry.(type) {
		case string:
			if text := strings.TrimSpace(value); text != "" {
				parts = append(parts, text)
			}
		case map[string]any:
			if text := strings.TrimSpace(config.GetStr(value, "text")); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// assistantVisibleText returns what the user actually saw. Grok stores the reply
// as a plain string, Codex as typed content blocks; tool calls carry no text and
// must not be read as a response, or they would close a turn's thinking early.
func assistantVisibleText(item map[string]any) string {
	if text := strings.TrimSpace(config.GetStr(item, "content")); text != "" {
		return text
	}
	var parts []string
	for _, entry := range config.GetSlice(item, "content") {
		block, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		switch config.GetStr(block, "type") {
		case "output_text", "text":
			if text := strings.TrimSpace(config.GetStr(block, "text")); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
