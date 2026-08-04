package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ArtifactMetadata struct {
	ID            string      `json:"id" yaml:"id"`
	SchemaVersion int         `json:"schemaVersion" yaml:"schemaVersion"`
	Title         string      `json:"title" yaml:"title"`
	CreatedAt     time.Time   `json:"createdAt" yaml:"createdAt"`
	Producer      string      `json:"producer" yaml:"producer"`
	RunID         string      `json:"runId,omitempty" yaml:"runId,omitempty"`
	Sources       []SourceRef `json:"sources,omitempty" yaml:"sources,omitempty"`
}

type Artifact struct {
	Metadata ArtifactMetadata `json:"metadata"`
	Path     string           `json:"path"`
}

func SaveArtifact(dataDir, title, body, producer, runID string, sources []SourceRef) (*Artifact, error) {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("artifact title and body must not be empty")
	}
	if producer == "" {
		producer = "atm-cli"
	}
	id := newID("artifact")
	metadata := ArtifactMetadata{
		ID:            id,
		SchemaVersion: ArtifactSchemaVersion,
		Title:         strings.TrimSpace(title),
		CreatedAt:     time.Now().UTC(),
		Producer:      producer,
		RunID:         runID,
		Sources:       sources,
	}
	frontmatter, err := yaml.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, "artifacts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	name := strings.ReplaceAll(id, ":", "_") + ".md"
	path := filepath.Join(dir, name)
	content := append([]byte("---\n"), frontmatter...)
	content = append(content, []byte("---\n\n"+strings.TrimSpace(body)+"\n")...)
	if err := atomicWrite(path, content, 0600); err != nil {
		return nil, err
	}
	return &Artifact{Metadata: metadata, Path: path}, nil
}
