package collector

import (
	"context"
	"strings"
)

// Candidate is one discoverable connector resource that can become a
// collection source. It carries the identifier the connector needs plus enough
// detail for a human to tell same-named results apart.
type Candidate struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Detail     string `json:"detail,omitempty"`
}

type Directory interface {
	Search(ctx context.Context, kind, keyword string, limit int) ([]Candidate, error)
}

const (
	DirectoryKindGroup = "group"
	DirectoryKindUser  = "user"
	DirectoryKindAll   = "all"
)

// MatchesName reports whether the candidate's own name carries the keyword.
// Search results may match on metadata, so callers use this to prefer the one
// exact display-name match without guessing between several candidates.
func MatchesName(candidate Candidate, keyword string) bool {
	return strings.Contains(strings.ToLower(candidate.Name), strings.ToLower(strings.TrimSpace(keyword)))
}
