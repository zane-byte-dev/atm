package collector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
)

// DigestInput is what a summariser gets: one source's insight decisions for one
// day. It carries the distilled titles and summaries rather than the chat they
// came from — that judgement was already made and paid for once, and re-reading
// the raw conversation would drag the noise back in.
type DigestInput struct {
	Source store.CollectionSource
	Date   string
	Items  []store.CollectionItem
}

type DigestContent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type Summarizer interface {
	Summarize(context.Context, DigestInput) (DigestContent, error)
}

type DigestOptions struct {
	// Date is the local day to summarise as YYYY-MM-DD; empty means today.
	Date string
	// DueOnly honours CollectionDigestIntervalMinutes so a background caller can
	// poll as often as it likes without paying for a model call every time.
	DueOnly bool
	// DryRun produces the digest and returns it without writing any knowledge.
	DryRun bool
}

type DigestResult struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name,omitempty"`
	Date       string `json:"date"`
	// Status is one of created, updated, skipped or failed.
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	DocumentID string `json:"document_id,omitempty"`
	Collection string `json:"collection,omitempty"`
	Title      string `json:"title,omitempty"`
	ItemCount  int    `json:"item_count"`
	// Body is filled on a dry run only, so the digest can be read before it is filed.
	Body  string `json:"body,omitempty"`
	Error string `json:"error,omitempty"`
}

type DigestReport struct {
	Results []DigestResult `json:"results"`
}

// Digest turns a source's insights for one day into a knowledge document. The
// digest for a day is a function of every insight that day, so running it again
// rewrites the same document instead of filing a second one — which is what makes
// it safe to call on a timer while the day is still going.
func (service Service) Digest(ctx context.Context, sourceID string, options DigestOptions) (DigestReport, error) {
	if service.Summarizer == nil {
		return DigestReport{}, fmt.Errorf("collector summarizer is required")
	}
	if service.Now == nil {
		service.Now = func() time.Time { return time.Now().In(config.Loc) }
	}
	now := service.Now()
	day, err := digestDay(options.Date, now)
	if err != nil {
		return DigestReport{}, err
	}
	date := day.Format("2006-01-02")
	db, err := store.Open()
	if err != nil {
		return DigestReport{}, err
	}
	defer db.Close()
	sources, err := digestSources(db, sourceID)
	if err != nil {
		return DigestReport{}, err
	}
	report := DigestReport{Results: []DigestResult{}}
	var failures []string
	for _, source := range sources {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		result := service.digestSource(ctx, db, source, date, day, now, options)
		report.Results = append(report.Results, result)
		if result.Status == "failed" {
			failures = append(failures, sourceDisplayName(source)+": "+result.Error)
		}
	}
	if len(failures) > 0 {
		return report, fmt.Errorf("digest failed for %d source(s): %s",
			len(failures), strings.Join(failures, "; "))
	}
	return report, nil
}

func (service Service) digestSource(ctx context.Context, db *sql.DB, source store.CollectionSource,
	date string, day, now time.Time, options DigestOptions) DigestResult {
	result := DigestResult{SourceID: source.ID, SourceName: sourceDisplayName(source), Date: date}
	dayStart, dayEnd := store.CollectionDayBounds(day)
	items, err := store.ListCollectionInsights(db, source.ID, dayStart, dayEnd)
	if err != nil {
		result.Status, result.Error = "failed", compactError(err)
		return result
	}
	result.ItemCount = len(items)
	if len(items) == 0 {
		result.Status, result.Reason = "skipped", "当天没有可沉淀的内容"
		return result
	}
	existing, err := store.GetCollectionDigest(db, source.ID, date)
	if err != nil {
		result.Status, result.Error = "failed", compactError(err)
		return result
	}
	result.DocumentID = existing.DocumentID
	newest := int64(0)
	for _, item := range items {
		if watermark := store.CollectionInsightWatermark(item); watermark > newest {
			newest = watermark
		}
	}
	if existing.DocumentID != "" && newest <= existing.CoveredThrough {
		result.Status, result.Reason = "skipped", "已包含当天全部沉淀内容"
		result.Collection, result.Title = existing.Collection, existing.Title
		return result
	}
	if options.DueOnly && existing.UpdatedAt > 0 {
		wait := time.Duration(config.CollectionDigestIntervalMinutes) * time.Minute
		if now.Unix()-existing.UpdatedAt < int64(wait.Seconds()) {
			result.Status = "skipped"
			result.Reason = fmt.Sprintf("距上次沉淀不足 %d 分钟，等新内容攒够再写",
				config.CollectionDigestIntervalMinutes)
			result.Collection, result.Title = existing.Collection, existing.Title
			return result
		}
	}
	content, err := service.Summarizer.Summarize(ctx, DigestInput{Source: source, Date: date, Items: items})
	if err != nil {
		result.Status, result.Error = "failed", compactError(err)
		return result
	}
	title := strings.TrimSpace(content.Title)
	if title == "" {
		title = defaultDigestTitle(source, date)
	}
	result.Title = title
	result.Collection = digestCollection(source)
	body := digestBody(content.Body, source, date, items)
	if options.DryRun {
		result.Status, result.Reason, result.Body = "skipped", "dry run：未写入知识库", body
		return result
	}
	document, created, err := writeDigestDocument(existing.DocumentID, title, body, result.Collection, source)
	if err != nil {
		result.Status, result.Error = "failed", compactError(err)
		return result
	}
	result.DocumentID = document.Metadata.ID
	result.Collection = document.Collection
	result.Status = "updated"
	if created {
		result.Status = "created"
	}
	if err := store.SaveCollectionDigest(db, store.CollectionDigest{
		SourceID: source.ID, DigestDate: date, DocumentID: document.Metadata.ID,
		Collection: document.Collection, Title: title, ItemCount: len(items),
		CoveredThrough: newest, CreatedAt: existing.CreatedAt,
	}); err != nil {
		result.Status, result.Error = "failed", compactError(err)
	}
	return result
}

// writeDigestDocument rewrites the day's existing document, or files a new one.
// A document deleted by hand is filed again rather than turning into a permanent
// failure: the ledger row is ATM's own bookkeeping, and the knowledge base is the
// user's to prune.
func writeDigestDocument(documentID, title, body, collection string,
	source store.CollectionSource) (*knowledge.Document, bool, error) {
	if documentID != "" {
		document, err := knowledge.Update(config.AtmDir, documentID, body)
		if err == nil {
			return document, false, nil
		}
		if !strings.Contains(err.Error(), "not found") {
			return nil, false, err
		}
	}
	// No SourceInfo on purpose: it marks a document as an imported copy of a file
	// ATM does not own, which makes it read-only. A digest is ATM's own writing and
	// has to stay rewritable. Where it came from lives in the tags, the 来源
	// section of the body, and the collection_digests ledger.
	document, err := knowledge.Add(config.AtmDir, knowledge.AddDocumentInput{
		Title: title, Content: body, Collection: collection,
		Tags:     []string{"钉钉动态", sourceDisplayName(source)},
		Projects: digestProjects(source),
		Producer: "atm-collector",
	})
	if err != nil {
		return nil, false, err
	}
	return document, true, nil
}

// digestBody keeps the model's prose and appends the decisions it was built from.
// Without that list a digest cannot be checked against what was actually said,
// and the insight items it came from would only be reachable through the database.
func digestBody(body string, source store.CollectionSource, date string, items []store.CollectionItem) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(body))
	builder.WriteString("\n\n---\n\n## 来源\n\n")
	builder.WriteString(fmt.Sprintf("%s · %s · 共 %d 条沉淀\n\n",
		sourceDisplayName(source), date, len(items)))
	for _, item := range items {
		occurred := time.Unix(store.CollectionInsightWatermark(item), 0).In(config.Loc).Format("15:04")
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = "（无标题）"
		}
		builder.WriteString("- " + occurred + " " + title)
		if sender := strings.TrimSpace(item.Sender); sender != "" {
			builder.WriteString("（" + sender + "）")
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func digestSources(db *sql.DB, sourceID string) ([]store.CollectionSource, error) {
	if strings.TrimSpace(sourceID) != "" {
		source, err := store.GetCollectionSource(db, strings.TrimSpace(sourceID))
		if err != nil {
			return nil, err
		}
		return []store.CollectionSource{source}, nil
	}
	return store.ListCollectionSources(db, "", true)
}

func digestDay(date string, now time.Time) (time.Time, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return now.In(config.Loc), nil
	}
	day, err := time.ParseInLocation("2006-01-02", date, config.Loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("digest date must be YYYY-MM-DD: %s", date)
	}
	return day, nil
}

func digestCollection(source store.CollectionSource) string {
	if collection := strings.TrimSpace(source.KnowledgeCollection); collection != "" {
		return collection
	}
	return config.CollectionDigestCollection
}

func digestProjects(source store.CollectionSource) []string {
	if project := strings.TrimSpace(source.Project); project != "" {
		return []string{project}
	}
	return nil
}

func defaultDigestTitle(source store.CollectionSource, date string) string {
	return sourceDisplayName(source) + " " + date + " 动态"
}

// AutomaticSummarizer distils a day of insights with the same model command and
// sandbox as classification.
type AutomaticSummarizer struct {
	ModelCommand string
	Timeout      time.Duration
}

func (summarizer AutomaticSummarizer) Summarize(ctx context.Context, input DigestInput) (DigestContent, error) {
	if len(input.Items) == 0 {
		return DigestContent{}, fmt.Errorf("digest needs at least one insight")
	}
	data, err := runCollectionModel(ctx, summarizer.ModelCommand, summarizer.Timeout,
		"digest", digestJSONSchema, digestPrompt(input))
	if err != nil {
		return DigestContent{}, err
	}
	var content DigestContent
	if err := json.Unmarshal(data, &content); err != nil {
		return DigestContent{}, fmt.Errorf("decode collection digest: %w", err)
	}
	if strings.TrimSpace(content.Body) == "" {
		return DigestContent{}, fmt.Errorf("collection digest came back empty")
	}
	return content, nil
}

func digestPrompt(input DigestInput) string {
	notes := make([]map[string]any, 0, len(input.Items))
	for _, item := range input.Items {
		notes = append(notes, map[string]any{
			"time":    time.Unix(store.CollectionInsightWatermark(item), 0).In(config.Loc).Format("15:04"),
			"sender":  item.Sender,
			"title":   item.Title,
			"summary": item.Summary,
			"type":    item.ItemType,
		})
	}
	notesJSON, _ := json.MarshalIndent(notes, "", "  ")
	return `You write one day's digest for a chat source that ATM watches. The input is
already-distilled notes, not raw chat: each one was judged worth remembering.
Return exactly one JSON object matching the supplied schema. Do not call tools.

Write in Chinese, as Markdown, for someone reading this months from now who was
not in the chat:
- Group related notes under "## " headings by topic. Do not produce one heading per note.
- State facts, decisions and their reasons, and constraints. Keep concrete names,
  numbers, links and identifiers verbatim — they are why this is worth keeping.
- Merge notes that say the same thing. Say who is doing what only when it matters
  later; this is a knowledge document, not a status report about people.
- Drop anything that reads as chatter now that it sits next to everything else.
- No preamble, no "本文档" framing, no restating the title. Do not invent anything
  that is not in the notes.

title: a specific one-line Chinese title naming the source, the date and what the
day was actually about — not a generic "群聊动态".

Source name: ` + sourceDisplayName(input.Source) + `
Source project: ` + input.Source.Project + `
Date: ` + input.Date + `

Notes:
` + string(notesJSON)
}

const digestJSONSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "body"],
  "properties": {
    "title": {"type": "string"},
    "body": {"type": "string"}
  }
}`
