package refine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/textmodel"
)

const todoTitleSchema = `{"type":"object","additionalProperties":false,"required":["title"],"properties":{"title":{"type":"string"}}}`

// GenerateTitle turns a human's requirement block into one concise list label.
// It does not mutate a Todo and deliberately shares Refine's sanitization rules.
func GenerateTitle(ctx context.Context, description string) (string, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return "", fmt.Errorf("todo description is required")
	}
	prompt := "Generate a concise Todo title from the description below. " +
		"Use at most 40 Chinese characters or an equivalent length, with no ID, priority, or trailing punctuation.\n\n" +
		"Todo description:\n" + description
	data, err := runModel(ctx, textmodel.TaskTodoTitle, 45*time.Second, todoTitleSchema, prompt)
	if err != nil {
		return "", err
	}
	var result struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode generated todo title: %w", err)
	}
	title := sanitizeTitle(result.Title)
	if title == "" {
		return "", fmt.Errorf("text model returned an empty todo title")
	}
	return title, nil
}
