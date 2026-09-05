package knowledge

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type chunk struct {
	document Document
	text     string
	index    string
	start    int
	end      int
	tokens   []string
}

func searchDocuments(documents []Document, query string, options SearchOptions, qualityIndex map[string]float64) []SearchHit {
	if options.Limit <= 0 {
		options.Limit = 10
	}
	allChunks := chunkDocuments(documents)
	var chunks []chunk
	for _, candidate := range allChunks {
		if !matchesSearchOptions(candidate.document, options) {
			continue
		}
		chunks = append(chunks, candidate)
	}
	if len(chunks) == 0 {
		return []SearchHit{}
	}

	queryTokens := tokenize(query)
	documentFrequency := make(map[string]int)
	totalTokens := 0
	for i := range chunks {
		if chunks[i].tokens == nil {
			chunks[i].tokens = tokenize(chunks[i].index)
		}
		totalTokens += len(chunks[i].tokens)
		seen := make(map[string]bool)
		for _, token := range chunks[i].tokens {
			if !seen[token] {
				documentFrequency[token]++
				seen[token] = true
			}
		}
	}
	averageLength := float64(totalTokens) / float64(len(chunks))
	queryLower := strings.ToLower(query)
	var hits []SearchHit
	for _, candidate := range chunks {
		// A query may contain alternative terms ("Skill 统计" should find a
		// Skill-only document and a 统计-only document), but a term itself is
		// indivisible. Requiring all tokens from at least one whitespace-delimited
		// term prevents a search for "搜索" from matching every occurrence of
		// "探索" merely because both contain 索.
		matched := matchedTokenCount(candidate.tokens, queryTokens)
		if matched == 0 || !matchesAnyQueryTerm(candidate.index, query) {
			continue
		}
		score := bm25(candidate.tokens, queryTokens, documentFrequency, len(chunks), averageLength)
		if strings.Contains(strings.ToLower(candidate.index), queryLower) {
			score += 2
		}
		titleTokens := tokenize(candidate.document.Metadata.Title)
		if containsAllTokens(titleTokens, queryTokens) {
			score += 2
		}
		if strings.Contains(strings.ToLower(candidate.document.Metadata.Title), queryLower) {
			score += 4
		}
		if score <= 0 {
			continue
		}
		// Prefer chunks that cover more of the query. Full coverage preserves the
		// old exact-match ranking on top; partial matches rank below but survive.
		score *= 0.5 + 0.5*float64(matched)/float64(len(queryTokens))
		metadata := candidate.document.Metadata
		quality := qualityIndex[metadata.ID]
		if quality == 0 {
			quality = 0.5
		}
		score *= 0.85 + 0.3*quality
		hits = append(hits, SearchHit{
			DocumentID: metadata.ID,
			Title:      metadata.Title,
			Collection: candidate.document.Collection,
			Status:     metadata.Status,
			Domains:    metadata.Domains,
			Tags:       metadata.Tags,
			Projects:   metadata.Projects,
			Snippet:    compactSnippet(candidate.text, 500),
			LineStart:  candidate.start,
			LineEnd:    candidate.end,
			Score:      score,
			Quality:    quality,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			if hits[i].DocumentID == hits[j].DocumentID {
				return hits[i].LineStart < hits[j].LineStart
			}
			return hits[i].DocumentID < hits[j].DocumentID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > options.Limit {
		hits = hits[:options.Limit]
	}
	return hits
}

func chunkDocuments(documents []Document) []chunk {
	chunks := []chunk{}
	for _, document := range documents {
		values := chunkDocument(document)
		for index := range values {
			values[index].tokens = tokenize(values[index].index)
		}
		chunks = append(chunks, values...)
	}
	return chunks
}

func Discover(dataDir string) ([]Document, error) {
	dir := knowledgeRoot(dataDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return []Document{}, nil
	} else if err != nil {
		return nil, err
	}
	var documents []Document
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != dir && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "_collection.md" {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		document, err := readDocument(path)
		if err != nil {
			return err
		}
		document.Collection = collectionFromPath(dir, path)
		documents = append(documents, *document)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan central knowledge: %w", err)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Metadata.ID < documents[j].Metadata.ID })
	return documents, nil
}

func List(dataDir string, collections []string) ([]DocumentSummary, error) {
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(collections))
	for _, collection := range collections {
		if value := strings.TrimSpace(collection); value != "" {
			allowed[value] = true
		}
	}
	summaries := make([]DocumentSummary, 0, len(documents))
	for _, document := range documents {
		if len(allowed) > 0 && !allowed[document.Collection] {
			continue
		}
		metadata := document.Metadata
		summaries = append(summaries, DocumentSummary{
			DocumentID: metadata.ID,
			Title:      metadata.Title,
			Collection: document.Collection,
			Status:     metadata.Status,
			Domains:    metadata.Domains,
			Tags:       metadata.Tags,
			Projects:   metadata.Projects,
			Producer:   metadata.Producer,
			CreatedAt:  metadata.CreatedAt,
			UpdatedAt:  metadata.UpdatedAt,
		})
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if !summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
		}
		if summaries[i].Title != summaries[j].Title {
			return summaries[i].Title < summaries[j].Title
		}
		return summaries[i].DocumentID < summaries[j].DocumentID
	})
	return summaries, nil
}

func Get(dataDir, documentID string) (*Document, error) {
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	for i := range documents {
		if documents[i].Metadata.ID == documentID {
			return &documents[i], nil
		}
	}
	return nil, documentNotFoundError{DocumentID: documentID}
}

type documentNotFoundError struct {
	DocumentID string
}

func (err documentNotFoundError) Error() string {
	return fmt.Sprintf("knowledge document not found: %s", err.DocumentID)
}

func chunkDocument(document Document) []chunk {
	lines := strings.Split(document.Content, "\n")
	var chunks []chunk
	start := 0
	var current []string
	metadataText := strings.Join(append(append(append([]string{document.Metadata.Title}, document.Metadata.Domains...), document.Metadata.Tags...), document.Metadata.Projects...), " ")
	flush := func(end int) {
		text := strings.TrimSpace(strings.Join(current, "\n"))
		if text != "" {
			chunks = append(chunks, chunk{document: document, text: text, index: metadataText + "\n" + text, start: start + 1, end: end})
		}
		current = nil
	}
	currentLength := 0
	for i, line := range lines {
		if currentLength+len(line) > 1600 && len(current) > 0 {
			flush(i)
			start = i
			currentLength = 0
		}
		if len(current) == 0 && strings.TrimSpace(line) == "" {
			start = i + 1
			continue
		}
		current = append(current, line)
		currentLength += len(line) + 1
		if strings.TrimSpace(line) == "" && currentLength >= 400 {
			flush(i + 1)
			start = i + 1
			currentLength = 0
		}
	}
	flush(len(lines))
	return chunks
}

func matchesSearchOptions(document Document, options SearchOptions) bool {
	metadata := document.Metadata
	return matchesAny([]string{document.Collection}, options.Collections) &&
		matchesAny(metadata.Domains, options.Domains) &&
		matchesAny(metadata.Tags, options.Tags) &&
		matchesAny(metadata.Projects, options.Projects) &&
		matchesAny([]string{metadata.Status}, options.Statuses)
}

func collectionFromPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == "" || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "inbox"
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		return "inbox"
	}
	return parts[0]
}

func matchesAny(values, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, value := range values {
		for _, filter := range filters {
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(filter)) {
				return true
			}
		}
	}
	return false
}

func tokenize(text string) []string {
	var tokens []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			tokens = append(tokens, strings.ToLower(string(word)))
			word = nil
		}
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			flush()
			tokens = append(tokens, string(r))
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			word = append(word, unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func bm25(tokens, query []string, frequency map[string]int, documentCount int, averageLength float64) float64 {
	if len(tokens) == 0 || len(query) == 0 || averageLength == 0 {
		return 0
	}
	counts := make(map[string]int)
	for _, token := range tokens {
		counts[token]++
	}
	const k1, b = 1.2, 0.75
	var score float64
	seen := make(map[string]bool)
	for _, token := range query {
		if seen[token] || counts[token] == 0 {
			continue
		}
		seen[token] = true
		df := frequency[token]
		idf := math.Log(1 + (float64(documentCount-df)+0.5)/(float64(df)+0.5))
		tf := float64(counts[token])
		denominator := tf + k1*(1-b+b*float64(len(tokens))/averageLength)
		score += idf * (tf * (k1 + 1)) / denominator
	}
	return score
}

// matchedTokenCount returns how many distinct query tokens appear in tokens.
func matchedTokenCount(tokens, query []string) int {
	if len(query) == 0 {
		return 0
	}
	available := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		available[token] = struct{}{}
	}
	matched := 0
	seen := make(map[string]struct{}, len(query))
	for _, token := range query {
		if _, dup := seen[token]; dup {
			continue
		}
		seen[token] = struct{}{}
		if _, ok := available[token]; ok {
			matched++
		}
	}
	return matched
}

// matchesAnyQueryTerm implements OR between user-entered terms while keeping
// each term contiguous. Joining its normalized tokens removes punctuation but
// preserves the user's word/character order, so "搜索" cannot match a chunk
// where 搜 and 索 occur far apart (or only as part of unrelated words).
func matchesAnyQueryTerm(text, query string) bool {
	text = strings.ToLower(text)
	for _, term := range strings.Fields(query) {
		termTokens := tokenize(term)
		normalizedTerm := strings.Join(termTokens, "")
		if normalizedTerm != "" && strings.Contains(text, normalizedTerm) {
			return true
		}
	}
	return false
}

func containsAllTokens(tokens, query []string) bool {
	if len(query) == 0 {
		return false
	}
	available := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		available[token] = struct{}{}
	}
	for _, token := range query {
		if _, ok := available[token]; !ok {
			return false
		}
	}
	return true
}

func markdownTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return fallback
}

func compactSnippet(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}
