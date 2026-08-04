package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

type MemoryEvent struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Op            string            `json:"op"`
	Scope         string            `json:"scope"`
	Content       string            `json:"content,omitempty"`
	TargetID      string            `json:"targetId,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type MemoryHit struct {
	ID        string            `json:"id"`
	Scope     string            `json:"scope"`
	Content   string            `json:"content"`
	Tags      []string          `json:"tags,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Score     float64           `json:"score"`
	Source    string            `json:"source"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Shared memory lives in the database, so these take no data directory: the
// markdown corpus is the only thing a caller can point somewhere else.
func RememberWithMetadata(scope, content string, tags []string, metadata map[string]string) (*MemoryEvent, error) {
	if err := ValidateScope(scope); err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("memory content must not be empty")
	}
	event := MemoryEvent{SchemaVersion: MemorySchemaVersion, ID: newID("memory"), Op: store.MemoryOpRemember, Scope: scope, Content: content, Tags: normalizeValues(tags), CreatedAt: time.Now().UTC(), Metadata: normalizeMetadata(metadata)}
	if err := store.AppendMemoryEvent(memoryRow(event)); err != nil {
		return nil, err
	}
	return &event, nil
}

func SupersedeWithMetadata(targetID, scope, content string, tags []string, metadata map[string]string) (*MemoryEvent, error) {
	if err := ValidateScope(scope); err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetID) == "" || strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("target id and content must not be empty")
	}
	event := MemoryEvent{SchemaVersion: MemorySchemaVersion, ID: newID("memory"), Op: store.MemoryOpSupersede, Scope: scope, Content: strings.TrimSpace(content), TargetID: targetID, Tags: normalizeValues(tags), CreatedAt: time.Now().UTC(), Metadata: normalizeMetadata(metadata)}
	if err := appendMemoryMutation(event); err != nil {
		return nil, err
	}
	return &event, nil
}

func ForgetWithMetadata(targetID, scope string, metadata map[string]string) (*MemoryEvent, error) {
	if err := ValidateScope(scope); err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetID) == "" {
		return nil, fmt.Errorf("target id must not be empty")
	}
	event := MemoryEvent{SchemaVersion: MemorySchemaVersion, ID: newID("memory"), Op: store.MemoryOpForget, Scope: scope, TargetID: targetID, CreatedAt: time.Now().UTC(), Metadata: normalizeMetadata(metadata)}
	if err := appendMemoryMutation(event); err != nil {
		return nil, err
	}
	return &event, nil
}

func Recall(query, scope string, limit int) ([]MemoryHit, error) {
	if scope != "" {
		if err := ValidateScope(scope); err != nil {
			return nil, err
		}
	}
	effective, err := store.EffectiveMemories()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	var hits []MemoryHit
	for _, row := range effective {
		event := memoryEvent(row)
		if scope != "" && event.Scope != scope && event.Scope != "global" {
			continue
		}
		score := memoryScore(event.Content, query, event.CreatedAt)
		if query != "" && score <= 0 {
			continue
		}
		hits = append(hits, MemoryHit{ID: event.ID, Scope: event.Scope, Content: event.Content, Tags: event.Tags, CreatedAt: event.CreatedAt, Score: score, Source: "memory", Metadata: event.Metadata})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].CreatedAt.After(hits[j].CreatedAt)
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func normalizeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			normalized[key] = value
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// memoryRow and memoryEvent map between the API shape (which carries the JSON
// contract's schemaVersion) and the stored row.
func memoryRow(event MemoryEvent) store.MemoryEvent {
	return store.MemoryEvent{
		ID: event.ID, Op: event.Op, Scope: event.Scope, Content: event.Content,
		TargetID: event.TargetID, Tags: event.Tags, Metadata: event.Metadata,
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func memoryEvent(row store.MemoryEvent) MemoryEvent {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	return MemoryEvent{
		SchemaVersion: MemorySchemaVersion,
		ID:            row.ID, Op: row.Op, Scope: row.Scope, Content: row.Content,
		TargetID: row.TargetID, Tags: row.Tags, Metadata: row.Metadata,
		CreatedAt: createdAt.UTC(),
	}
}

// appendMemoryMutation rejects a supersede or forget whose target is not
// currently in force. The foreign key only guarantees the target exists at all;
// acting on an already-forgotten memory is a domain error, not an integrity one.
// The check reads on one connection and writes on another,
// so two processes can both pass it for the same target; the unique index on
// target_id is what actually stops the second one, and its error is translated
// back into the same domain message.
func appendMemoryMutation(event MemoryEvent) error {
	target, err := store.EffectiveMemory(event.TargetID)
	if err != nil {
		return err
	}
	if target.Scope != event.Scope {
		return fmt.Errorf("memory scope mismatch: target uses %s", target.Scope)
	}
	if err := store.AppendMemoryEvent(memoryRow(event)); err != nil {
		if store.IsMemoryTargetTaken(err) {
			return fmt.Errorf("active memory not found: %s", event.TargetID)
		}
		return err
	}
	return nil
}

func memoryScore(content, query string, createdAt time.Time) float64 {
	if query == "" {
		return recencyScore(createdAt)
	}
	queryTokens := tokenize(query)
	tokens := tokenize(content)
	// Match on at least one query token, mirroring knowledge search: the old
	// all-tokens gate zeroed a memory whenever a single query character was
	// absent, which is too harsh under per-char Chinese tokenization. The
	// coverage factor below keeps complete matches ranked above partial ones.
	matched := matchedTokenCount(tokens, queryTokens)
	if matched == 0 {
		return 0
	}
	frequency := make(map[string]int)
	for _, token := range tokens {
		frequency[token] = 1
	}
	score := bm25(tokens, queryTokens, frequency, 1, float64(max(len(tokens), 1)))
	if strings.Contains(strings.ToLower(content), strings.ToLower(query)) {
		score += 2
	}
	if score <= 0 {
		return 0
	}
	score *= 0.5 + 0.5*float64(matched)/float64(len(queryTokens))
	return score + recencyScore(createdAt)
}

func recencyScore(createdAt time.Time) float64 {
	if createdAt.IsZero() {
		return 0
	}
	days := time.Since(createdAt).Hours() / 24
	if days < 0 {
		days = 0
	}
	return 0.25 / (1 + days/30)
}

func newID(prefix string) string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s:%d:%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(random))
}
