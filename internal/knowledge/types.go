package knowledge

import (
	"fmt"
	"strings"
	"time"
)

const (
	KnowledgeSchemaVersion = 1
	MemorySchemaVersion    = 1
	ArtifactSchemaVersion  = 1
	FeedbackSchemaVersion  = 1
)

type SourceInfo struct {
	Type       string    `json:"type" yaml:"type"`
	URI        string    `json:"uri" yaml:"uri"`
	Hash       string    `json:"hash,omitempty" yaml:"hash,omitempty"`
	ImportedAt time.Time `json:"importedAt,omitempty" yaml:"importedAt,omitempty"`
}

type DocumentMetadata struct {
	ID            string      `json:"id" yaml:"id"`
	SchemaVersion int         `json:"schemaVersion" yaml:"schemaVersion"`
	Title         string      `json:"title" yaml:"title"`
	Status        string      `json:"status" yaml:"status"`
	Domains       []string    `json:"domains,omitempty" yaml:"domains,omitempty"`
	Tags          []string    `json:"tags,omitempty" yaml:"tags,omitempty"`
	Projects      []string    `json:"projects,omitempty" yaml:"projects,omitempty"`
	Producer      string      `json:"producer" yaml:"producer"`
	CreatedAt     time.Time   `json:"createdAt" yaml:"createdAt"`
	UpdatedAt     time.Time   `json:"updatedAt" yaml:"updatedAt"`
	Source        *SourceInfo `json:"source,omitempty" yaml:"source,omitempty"`
}

type Document struct {
	Metadata   DocumentMetadata `json:"metadata"`
	Collection string           `json:"collection"`
	Path       string           `json:"-"`
	Content    string           `json:"content,omitempty"`
}

type DocumentSummary struct {
	DocumentID string    `json:"document_id"`
	Title      string    `json:"title"`
	Collection string    `json:"collection"`
	Status     string    `json:"status"`
	Domains    []string  `json:"domains,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Projects   []string  `json:"projects,omitempty"`
	Producer   string    `json:"producer"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SearchOptions struct {
	Limit       int      `json:"limit,omitempty"`
	Collections []string `json:"collections,omitempty"`
	Domains     []string `json:"domains,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Projects    []string `json:"projects,omitempty"`
	Statuses    []string `json:"statuses,omitempty"`
}

type SearchHit struct {
	DocumentID string   `json:"document_id"`
	Title      string   `json:"title"`
	Collection string   `json:"collection"`
	Status     string   `json:"status"`
	Domains    []string `json:"domains,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Projects   []string `json:"projects,omitempty"`
	Snippet    string   `json:"snippet"`
	LineStart  int      `json:"line_start"`
	LineEnd    int      `json:"line_end"`
	Score      float64  `json:"score"`
	Quality    float64  `json:"quality"`
}

type FeedbackEvent struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	DocumentID    string    `json:"documentId"`
	SessionID     string    `json:"sessionId"`
	Query         string    `json:"query,omitempty"`
	Outcome       string    `json:"outcome"`
	Note          string    `json:"note,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type FeedbackInput struct {
	DocumentID string
	SessionID  string
	Query      string
	Outcome    string
	Note       string
}

type KnowledgeQuality struct {
	DocumentID   string     `json:"document_id"`
	Title        string     `json:"title"`
	Collection   string     `json:"collection"`
	Retrievals   int        `json:"retrievals"`
	Adopted      int        `json:"adopted"`
	Corrected    int        `json:"corrected"`
	Rejected     int        `json:"rejected"`
	Score        float64    `json:"score"`
	LastFeedback *time.Time `json:"last_feedback,omitempty"`
}

type AuditOptions struct {
	StaleDays int
	Now       time.Time
}

type AuditIssue struct {
	Code            string   `json:"code"`
	Severity        string   `json:"severity"`
	DocumentIDs     []string `json:"document_ids"`
	Collection      string   `json:"collection,omitempty"`
	Title           string   `json:"title,omitempty"`
	Detail          string   `json:"detail"`
	SuggestedAction string   `json:"suggested_action"`
}

type AuditReport struct {
	GeneratedAt time.Time      `json:"generated_at"`
	StaleDays   int            `json:"stale_days"`
	Documents   int            `json:"documents"`
	Active      int            `json:"active"`
	Issues      []AuditIssue   `json:"issues"`
	Counts      map[string]int `json:"counts"`
}

type CollectionInfo struct {
	ID            string   `json:"id" yaml:"id"`
	Name          string   `json:"name" yaml:"name"`
	Role          string   `json:"role,omitempty" yaml:"role,omitempty"`
	Description   string   `json:"description" yaml:"description"`
	Topics        []string `json:"topics,omitempty" yaml:"topics,omitempty"`
	UseWhen       []string `json:"use_when,omitempty" yaml:"useWhen,omitempty"`
	AvoidWhen     []string `json:"avoid_when,omitempty" yaml:"avoidWhen,omitempty"`
	Instructions  []string `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	DocumentCount int      `json:"document_count" yaml:"-"`
}

type SourceRef struct {
	DocumentID string `json:"documentId" yaml:"documentId"`
	LineStart  int    `json:"lineStart,omitempty" yaml:"lineStart,omitempty"`
	LineEnd    int    `json:"lineEnd,omitempty" yaml:"lineEnd,omitempty"`
}

func ValidateScope(scope string) error {
	if scope == "global" {
		return nil
	}
	for _, prefix := range []string{"project:", "session:"} {
		if strings.HasPrefix(scope, prefix) && strings.TrimSpace(strings.TrimPrefix(scope, prefix)) != "" {
			return nil
		}
	}
	return fmt.Errorf("invalid scope %q: use global, project:<id>, or session:<id>", scope)
}
