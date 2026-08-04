package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func Catalog(dataDir string) ([]CollectionInfo, error) {
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	type collectionState struct {
		info      CollectionInfo
		topicUses map[string]int
	}
	states := make(map[string]*collectionState)
	for _, document := range documents {
		id := document.Collection
		if id == "" {
			id = "inbox"
		}
		state := states[id]
		if state == nil {
			state = &collectionState{info: CollectionInfo{ID: id, Name: id}, topicUses: make(map[string]int)}
			states[id] = state
		}
		if document.Metadata.Status != "archived" {
			state.info.DocumentCount++
			for _, topic := range append(append([]string{}, document.Metadata.Domains...), document.Metadata.Tags...) {
				state.topicUses[topic]++
			}
		}
	}

	root := knowledgeRoot(dataDir)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []CollectionInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		manifestPath := filepath.Join(root, entry.Name(), "_collection.md")
		info, err := readCollectionManifest(manifestPath, entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		state := states[entry.Name()]
		if state == nil {
			state = &collectionState{topicUses: make(map[string]int)}
			states[entry.Name()] = state
		}
		info.DocumentCount = state.info.DocumentCount
		state.info = *info
	}

	result := make([]CollectionInfo, 0, len(states))
	for _, state := range states {
		if state.info.Name == "" {
			state.info.Name = state.info.ID
		}
		if len(state.info.Topics) == 0 {
			state.info.Topics = popularTopics(state.topicUses, 8)
		}
		result = append(result, state.info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func readCollectionManifest(path, directoryID string) (*CollectionInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	frontmatter, body, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("read knowledge collection %s: %w", path, err)
	}
	var info CollectionInfo
	if err := yaml.Unmarshal([]byte(frontmatter), &info); err != nil {
		return nil, fmt.Errorf("read knowledge collection %s: %w", path, err)
	}
	if info.ID != "" && info.ID != directoryID {
		return nil, fmt.Errorf("knowledge collection id %q does not match directory %q", info.ID, directoryID)
	}
	info.ID = directoryID
	if info.Name == "" {
		info.Name = directoryID
	}
	if info.Description == "" {
		info.Description = strings.TrimSpace(body)
	}
	info.Role = strings.TrimSpace(info.Role)
	info.Topics = normalizeValues(info.Topics)
	info.UseWhen = normalizeOrderedValues(info.UseWhen)
	info.AvoidWhen = normalizeOrderedValues(info.AvoidWhen)
	info.Instructions = normalizeOrderedValues(info.Instructions)
	return &info, nil
}

func normalizeOrderedValues(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func popularTopics(counts map[string]int, limit int) []string {
	topics := make([]string, 0, len(counts))
	for topic := range counts {
		topics = append(topics, topic)
	}
	sort.Slice(topics, func(i, j int) bool {
		if counts[topics[i]] == counts[topics[j]] {
			return topics[i] < topics[j]
		}
		return counts[topics[i]] > counts[topics[j]]
	})
	if len(topics) > limit {
		topics = topics[:limit]
	}
	return topics
}
