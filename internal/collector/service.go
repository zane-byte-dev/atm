package collector

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

type Service struct {
	Connectors    *Registry
	RegistryError error
	// Fetcher is retained as a narrow injection point for existing embedders and
	// tests. Production routing uses Connectors and selects by source.Connector.
	Fetcher    Fetcher
	Extractor  Extractor
	Summarizer Summarizer
	Now        func() time.Time
	// ApplyCollectionEnabled is the config persistence port behind the global
	// collection switch. Nil uses config.Default in production.
	ApplyCollectionEnabled func(bool) (bool, error)
}

type RunReport struct {
	Runs []store.CollectionRun `json:"runs"`
	// Blocked names the connectors this run deliberately left alone because their
	// login had already expired. Skipped sources write no run row, so without this
	// the report would read as "nothing was due".
	Blocked []BlockedConnector `json:"blocked,omitempty"`
}

// BlockedConnector is one connector whose login has expired, together with the
// sources that were not attempted because of it.
//
// Deliberately only the login. A missing permission classifies just as firmly and
// never recovers on its own either, but in practice it has been per-source — one
// group this account cannot read while every sibling works — so holding the
// connector back for it would silence four healthy sources over one broken.
type BlockedConnector struct {
	Connector      string `json:"connector"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	SkippedSources int    `json:"skipped_sources"`
	// RetryAt is when the background path probes again. Zero when the evidence is
	// this run's own failure, which the next run dates from the ledger.
	RetryAt int64 `json:"retry_at,omitempty"`
	// LoginCommand is the connector's declared way back in, carried so the CLI and
	// the desktop can offer it without either of them reading config themselves.
	LoginCommand string `json:"login_command,omitempty"`
}

// authBlockedRetryInterval is how long the background path leaves a connector
// alone after a failure only a person can clear. A login that needs a scan
// cannot come back faster than the human does, so retrying it every interval
// buys nothing: one morning of it left 90 identical failure rows behind and
// still waited for the same person.
const authBlockedRetryInterval = 30 * time.Minute

func DefaultService() Service {
	registry, registryErr := DefaultRegistry()
	return Service{
		Connectors: registry, RegistryError: registryErr,
		Extractor:  AutomaticExtractor{},
		Summarizer: AutomaticSummarizer{},
		Now:        func() time.Time { return time.Now().In(config.Loc) },
		ApplyCollectionEnabled: func(enabled bool) (bool, error) {
			settings, err := config.Default.Apply(config.SettingsPatch{CollectionEnabled: &enabled})
			return settings.CollectionEnabled, err
		},
	}
}

func (service Service) Run(ctx context.Context, sourceID string) (RunReport, error) {
	return service.run(ctx, sourceID, false)
}

// RunDue is the background path: it honours each source's own cadence. Manual
// Run calls remain forceful so “立即收集” always means now.
func (service Service) RunDue(ctx context.Context, sourceID string) (RunReport, error) {
	return service.run(ctx, sourceID, true)
}

func (service Service) run(ctx context.Context, sourceID string, dueOnly bool) (RunReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if service.Extractor == nil {
		return RunReport{}, fmt.Errorf("collector extractor is required")
	}
	if service.RegistryError != nil {
		return RunReport{}, service.RegistryError
	}
	lock, err := acquireCollectionLock(ctx)
	if err != nil {
		return RunReport{}, err
	}
	defer lock.Close()
	if service.Connectors == nil && service.Fetcher == nil {
		registry, err := DefaultRegistry()
		if err != nil {
			return RunReport{}, err
		}
		service.Connectors = registry
	}
	if service.Now == nil {
		service.Now = func() time.Time { return time.Now().In(config.Loc) }
	}
	db, err := store.Open()
	if err != nil {
		return RunReport{}, err
	}
	defer db.Close()
	// Holding the process-shared lock proves no healthy collector can still own
	// a running ledger row. Close crash residue before deciding what is due; this
	// changes only local audit state and never replays connector/model work.
	if _, err := store.ReconcileInterruptedCollectionRuns(db, service.Now().Unix()); err != nil {
		return RunReport{}, err
	}
	sources, err := store.ListCollectionSources(db, "", true)
	if err != nil {
		return RunReport{}, err
	}
	if sourceID != "" {
		filtered := sources[:0]
		for _, source := range sources {
			if source.ID == sourceID {
				filtered = append(filtered, source)
			}
		}
		sources = filtered
		if len(sources) == 0 {
			return RunReport{}, fmt.Errorf("enabled collection source not found: %s", sourceID)
		}
	}
	if dueOnly {
		due := sources[:0]
		for _, source := range sources {
			ready, err := store.CollectionSourceDue(db, source, service.Now())
			if err != nil {
				return RunReport{}, err
			}
			if ready {
				due = append(due, source)
			}
		}
		sources = due
	}
	report := RunReport{Runs: []store.CollectionRun{}}
	// Only the background path honours the ledger. A manual run is the way back
	// after logging in, so it always attempts.
	blocked := map[string]BlockedConnector{}
	if dueOnly {
		blocked, err = blockedConnectors(db, service.Now())
		if err != nil {
			return RunReport{}, err
		}
	}
	errors := []string{}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if block, ok := blocked[source.Connector]; ok {
			block.SkippedSources++
			blocked[source.Connector] = block
			continue
		}
		run := service.runSource(ctx, db, source)
		report.Runs = append(report.Runs, run)
		if run.Status == "failed" {
			errors = append(errors, sourceDisplayName(source)+": "+run.Error)
			// An expired login belongs to the connector, not to this source: its
			// siblings are about to fail identically against the same credential,
			// and five copies of one message is how a real outage became noise.
			if status := CollectionFailureStatus(run.Error); status == "auth_required" {
				blocked[source.Connector] = BlockedConnector{
					Connector: source.Connector, Status: status, Error: run.Error,
					LoginCommand: ConnectorLoginCommand(source.Connector),
				}
			}
		}
	}
	report.Blocked = blockedReport(blocked)
	// Once per run rather than once per source. A failure here loses nothing and
	// the next run retries it, so it does not fail the run; `atm doctor` reports
	// chat that outlived its retention window, which is what a stuck prune looks
	// like from outside.
	if cutoff := store.RetentionCutoff(config.CollectionMessageRetentionDays, service.Now()); cutoff > 0 {
		_, _ = store.PruneCollectionMessages(db, cutoff)
	}
	if len(errors) > 0 {
		return report, fmt.Errorf("collection failed for %d source(s): %s", len(errors), strings.Join(errors, "; "))
	}
	return report, nil
}

// blockedConnectors reads the run ledger for connectors whose latest attempt
// failed on an expired login, recently enough that attempting them again now
// would only repeat it.
func blockedConnectors(db *sql.DB, now time.Time) (map[string]BlockedConnector, error) {
	latest, err := store.ListLatestCollectionRunsBySource(db)
	if err != nil {
		return nil, err
	}
	blocked := map[string]BlockedConnector{}
	// Newest first, so the first finished row a connector contributes is its
	// current state. A run still in flight says nothing either way.
	decided := map[string]bool{}
	for _, run := range latest {
		if run.Status == "running" || decided[run.Connector] {
			continue
		}
		decided[run.Connector] = true
		if run.Status == "succeeded" {
			continue
		}
		status := CollectionFailureStatus(run.Error)
		if status != "auth_required" {
			continue
		}
		finished := run.FinishedAt
		if finished == 0 {
			finished = run.StartedAt
		}
		retryAt := finished + int64(authBlockedRetryInterval.Seconds())
		if now.Unix() >= retryAt {
			continue
		}
		blocked[run.Connector] = BlockedConnector{
			Connector: run.Connector, Status: status, Error: run.Error, RetryAt: retryAt,
			LoginCommand: ConnectorLoginCommand(run.Connector),
		}
	}
	return blocked, nil
}

// blockedReport keeps only the connectors that actually held work back. One that
// blocked nothing has its own failed run row to speak for it.
func blockedReport(blocked map[string]BlockedConnector) []BlockedConnector {
	report := []BlockedConnector{}
	for _, block := range blocked {
		if block.SkippedSources > 0 {
			report = append(report, block)
		}
	}
	sort.Slice(report, func(i, j int) bool { return report[i].Connector < report[j].Connector })
	if len(report) == 0 {
		return nil
	}
	return report
}

func (service Service) runSource(ctx context.Context, db *sql.DB, source store.CollectionSource) store.CollectionRun {
	now := service.Now()
	run := store.CollectionRun{
		ID: runID(source.ID, now), Connector: source.Connector, SourceID: source.ID,
		Status: "running", StartedAt: now.Unix(),
	}
	if err := store.SaveCollectionRun(db, run); err != nil {
		run.Status, run.Error = "failed", err.Error()
		return run
	}
	finish := func(run *store.CollectionRun) {
		run.FinishedAt = service.Now().Unix()
		if run.Status == "running" {
			run.Status = "succeeded"
		}
		_ = store.SaveCollectionRun(db, *run)
	}
	checkpoint, err := store.GetCollectionCheckpoint(db, source.ID)
	if err != nil {
		run.Status, run.Error, run.FailedCount = "failed", err.Error(), 1
		finish(&run)
		return run
	}
	since := checkpoint.CursorTime
	if since > 0 {
		since -= int64((20 * time.Minute).Seconds())
	}
	fetcher, err := service.fetcherFor(source)
	if err != nil {
		run.Status, run.Error, run.FailedCount = "failed", err.Error(), 1
		finish(&run)
		return run
	}
	messages, newest, err := fetcher.Fetch(ctx, source, since)
	if err != nil {
		run.Status, run.Error, run.FailedCount = "failed", err.Error(), 1
		finish(&run)
		return run
	}
	run.FetchedCount = len(messages)
	// Archive the chat before deciding anything about it. The classifier only
	// keeps the lines it acts on, so this is the one chance to hold the rest.
	if _, err := store.PutCollectionMessages(db, CollectionMessagesFor(source, messages)); err != nil {
		// Treated like every other store failure here: visible, and the
		// checkpoint stays put so the next run reads the same window again.
		run.Status, run.Error, run.FailedCount = "failed", err.Error(), 1
		finish(&run)
		return run
	}
	handledMessageIDs, err := store.HandledCollectionMessageIDs(db, source.ID)
	if err != nil {
		run.Status, run.Error, run.FailedCount = "failed", err.Error(), 1
		finish(&run)
		return run
	}
	batches := groupMessagesWithContext(source, messages, handledMessageIDs)
	batchFailed := false
	insights := []runInsight{}
	for _, batch := range batches {
		if ctx.Err() != nil {
			run.FailedCount++
			run.Error = ctx.Err().Error()
			batchFailed = true
			break
		}
		item := itemFromBatch(batch, now.Unix())
		stored, inserted, err := store.PutCollectionItem(db, item)
		if err != nil {
			run.FailedCount++
			run.Error = err.Error()
			batchFailed = true
			continue
		}
		if inserted {
			// This batch owns its messages from now on, so any failed batch made
			// only of them will never be rebuilt again. Retire it here rather than
			// leave it advertising a retry that cannot arrive.
			if _, err := store.RetireSupersededCollectionItems(db, source.ID, stored.ID, stored.MessageIDs); err != nil {
				run.Error = err.Error()
			}
		}
		// Already decided, or held by an on-demand analysis for someone to
		// confirm. A proposal must not be carried out behind their back.
		if !inserted && (stored.Status == "processed" || stored.ProposedAction != "") {
			continue
		}
		// Out of retries: failing the same way every run is not something waiting
		// changes, and each attempt costs a model call. The item stays visible and
		// `atm collect item reprocess` still works, but the run must not be marked
		// failed for it — that is what would keep the checkpoint from advancing.
		if !inserted && store.CollectionRetriesExhausted(stored) {
			continue
		}
		item = stored
		item, err = service.processBatch(ctx, batch, item)
		if err != nil {
			markItemFailed(db, &item, err)
			run.FailedCount++
			if run.Error == "" {
				run.Error = compactError(err)
			}
			batchFailed = true
			continue
		}
		run.AnalyzedCount++
		switch item.Action {
		case "ignore":
			run.IgnoredCount++
		case "insight":
			run.InsightCount++
		case "append":
			run.AppendedCount++
		case "create":
			run.CreatedCount++
		default:
			markItemFailed(db, &item, fmt.Errorf("unsupported collection decision: %s", item.Action))
			run.FailedCount++
			batchFailed = true
			continue
		}
		if err := store.UpdateCollectionItem(db, item); err != nil {
			run.FailedCount++
			run.Error = err.Error()
			batchFailed = true
			continue
		}
		if item.Action == "insight" {
			insights = append(insights, runInsight{item: item, messages: batch.Messages})
		}
	}
	// Topics are how this run has to think — one batch, one decision, so an hour
	// holding one thing worth remembering and fifty jokes can answer differently
	// about each. They are not how a person reads the result: six cards land at
	// once and bury each other. So the round's insights are collapsed into the one
	// record it leaves behind, and the count follows the cards rather than the
	// topics. Todos are untouched — a piece of work is still one item.
	if service.mergeRunInsights(ctx, db, source, insights, now) {
		run.InsightCount = 1
	}
	if batchFailed {
		run.Status = "failed"
		if run.Error == "" {
			run.Error = "one or more message batches failed; checkpoint was not advanced"
		}
	} else if newest > checkpoint.CursorTime {
		if err := store.SaveCollectionCheckpoint(db, store.CollectionCheckpoint{
			SourceID: source.ID, CursorTime: newest,
		}); err != nil {
			run.Status, run.Error, run.FailedCount = "failed", err.Error(), run.FailedCount+1
		}
	}
	finish(&run)
	return run
}

// runInsight is one topic a run filed as an insight, held with its messages until
// the run can collapse them all into the single record it leaves behind.
type runInsight struct {
	item     store.CollectionItem
	messages []Message
}

// mergeRunInsights replaces this round's per-topic insight records with one
// record covering all of them, and reports whether it did. A single insight is
// left alone: it is already the one record, and merging it would cost a model
// call to rewrite text that is fine.
//
// Failing to merge is not a failed run. Nothing was lost — the per-topic records
// are still there, having been written and marked handled before this ran — and
// failing the run would hold the checkpoint back over messages that are already
// processed, so the next run would rebuild a window with nothing left to decide.
func (service Service) mergeRunInsights(ctx context.Context, db *sql.DB,
	source store.CollectionSource, insights []runInsight, now time.Time) bool {
	if len(insights) < 2 {
		return false
	}
	sort.SliceStable(insights, func(i, j int) bool {
		return insights[i].item.OccurredAt < insights[j].item.OccurredAt
	})
	batch := mergedInsightBatch(source, insights)
	item, err := applyDecision(batch, itemFromBatch(batch, now.Unix()),
		service.mergedInsightDecision(ctx, source, insights, now))
	memberIDs := make([]string, 0, len(insights))
	for _, insight := range insights {
		memberIDs = append(memberIDs, insight.item.ID)
	}
	if err == nil {
		_, err = store.MergeCollectionInsights(db, item, memberIDs)
	}
	if err != nil {
		logging.Failure("collection_insights_not_merged", source.ID, err, map[string]any{
			"source": source.ID,
			"items":  memberIDs,
		})
		return false
	}
	return true
}

// mergedInsightDecision writes the merged record's title and body. The model is
// asked first; a local join of what the per-topic records already say stands in
// when it cannot answer. That fallback is not the guessing this package refuses
// elsewhere — every line of it was already judged and written by an earlier model
// call, and nothing new is being claimed. It exists because the invariant this
// path is for, one round leaves one record, must not depend on the endpoint being
// up, and because the per-topic records are deleted either way. Which one
// happened is recorded in reason, where the App shows it.
func (service Service) mergedInsightDecision(ctx context.Context, source store.CollectionSource,
	insights []runInsight, now time.Time) Decision {
	items := make([]store.CollectionItem, 0, len(insights))
	confidence := float64(0)
	for _, insight := range insights {
		items = append(items, insight.item)
		confidence += insight.item.Confidence
	}
	// The merge itself judges nothing, so the record inherits what the per-topic
	// decisions claimed rather than announcing certainty of its own.
	confidence /= float64(len(items))
	reason := fmt.Sprintf("合并本轮 %d 条结论。", len(items))
	if service.Summarizer != nil {
		content, err := service.Summarizer.Summarize(ctx, DigestInput{Source: source,
			Date: now.Format("2006-01-02"), Items: items, Scope: DigestScopeRun})
		if err == nil {
			title := strings.TrimSpace(content.Title)
			if title == "" {
				title = mergedInsightTitle(source, len(items))
			}
			return normalizeDecision(Decision{Action: "insight", Title: title,
				Summary: strings.TrimSpace(content.Body), ItemType: "insight",
				Reason: reason, Confidence: confidence}, source)
		}
		reason += "内置模型不可用（" + compactError(err) + "），正文按各条原文拼接。"
	} else {
		reason += "没有配置摘要模型，正文按各条原文拼接。"
	}
	return normalizeDecision(Decision{Action: "insight", Title: mergedInsightTitle(source, len(items)),
		Summary: joinedInsightSummaries(items), ItemType: "insight",
		Reason: reason, Confidence: confidence}, source)
}

// mergedInsightBatch is the merged record's own batch: every message the merged
// topics owned, in time order. It carries the messages so the record answers for
// them — the union is what marks them handled — and the raw context so the chat
// behind the summary is still readable on the card.
func mergedInsightBatch(source store.CollectionSource, insights []runInsight) MessageBatch {
	messages := make([]Message, 0)
	seen := map[string]struct{}{}
	for _, insight := range insights {
		for _, message := range insight.messages {
			if _, ok := seen[message.ID]; ok {
				continue
			}
			seen[message.ID] = struct{}{}
			messages = append(messages, message)
		}
	}
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].CreatedAt != messages[j].CreatedAt {
			return messages[i].CreatedAt < messages[j].CreatedAt
		}
		return messages[i].ID < messages[j].ID
	})
	return MessageBatch{Source: source, Messages: messages,
		Fingerprint: messageBatchFingerprint(source.ID, messages),
		RawContext:  formatMessageContext(messages, nil)}
}

func mergedInsightTitle(source store.CollectionSource, count int) string {
	return fmt.Sprintf("%s 本轮 %d 条结论", sourceDisplayName(source), count)
}

func joinedInsightSummaries(items []store.CollectionItem) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		title, summary := strings.TrimSpace(item.Title), strings.TrimSpace(item.Summary)
		switch {
		case title == "":
			lines = append(lines, "- "+summary)
		case summary == "":
			lines = append(lines, "- "+title)
		default:
			lines = append(lines, "- "+title+"："+summary)
		}
	}
	return strings.Join(lines, "\n")
}

func (service Service) fetcherFor(source store.CollectionSource) (Fetcher, error) {
	if service.Connectors != nil {
		connector, err := service.Connectors.Resolve(source.Connector)
		if err != nil {
			return nil, err
		}
		return connector, nil
	}
	if service.Fetcher != nil {
		return service.Fetcher, nil
	}
	return nil, fmt.Errorf("collection connector is not configured: %s", source.Connector)
}

func unhandledMessages(messages []Message, handled map[string]struct{}) []Message {
	if len(handled) == 0 {
		return messages
	}
	result := make([]Message, 0, len(messages))
	for _, message := range messages {
		if _, alreadyHandled := handled[message.ID]; !alreadyHandled {
			result = append(result, message)
		}
	}
	return result
}

// groupMessagesWithContext splits a fetched window into one batch per topic —
// "same conversation, gaps under 15 minutes" — for every strategy. Observation
// sources used to collapse the whole window into a single batch, which was fine
// when the only outcome was one discardable blurb; it is wrong now that a batch
// is either kept as an insight or dropped as noise. An hour holding one decision
// worth remembering and fifty jokes has to be able to answer differently about
// each, which costs a model call per topic instead of one per run.
func groupMessagesWithContext(source store.CollectionSource, messages []Message,
	handled map[string]struct{}) []MessageBatch {
	groups := groupMessages(messages)
	result := make([]MessageBatch, 0, len(groups))
	for _, contextMessages := range groups {
		fresh := unhandledMessages(contextMessages, handled)
		if len(fresh) == 0 {
			continue
		}
		for _, unit := range decisionUnits(source, fresh) {
			result = append(result, messageBatchWithContext(source, unit, contextMessages))
		}
	}
	return result
}

// decisionUnits splits a window's fresh messages into the pieces that each get
// their own decision. Grouping is right for chat — a request and the two lines
// clarifying it are one piece of work — but a batch yields exactly one decision,
// so for a notification feed, where every push is a separate event, grouping
// silently keeps one and loses the rest. Either way the whole window still
// travels with each unit as context, so a reply that only makes sense next to
// the message above it still reads correctly.
func decisionUnits(source store.CollectionSource, fresh []Message) [][]Message {
	if source.DecisionUnit != store.CollectionDecisionUnitMessage {
		return [][]Message{fresh}
	}
	units := make([][]Message, 0, len(fresh))
	for _, message := range fresh {
		units = append(units, []Message{message})
	}
	return units
}

func messageBatchWithContext(source store.CollectionSource, fresh, contextMessages []Message) MessageBatch {
	actionable, keyword := actionableMessages(source, fresh)
	actionableIDs := map[string]struct{}{}
	for _, message := range actionable {
		actionableIDs[message.ID] = struct{}{}
	}
	return MessageBatch{Source: source, Messages: fresh,
		Fingerprint:     messageBatchFingerprint(source.ID, fresh),
		ActionContext:   formatMessageContext(actionable, nil),
		RawContext:      formatMessageContext(contextMessages, actionableIDs),
		ExcludedKeyword: keyword}
}

// actionableMessages drops the individual messages a source's keywords match,
// rather than the batch that happens to contain them. Both the CLI flag and the
// App call this a message filter, and batching is purely time-based: one 构建成功
// used to take every real request within fifteen minutes of it down as well.
// The dropped lines stay available as continuity — a status broadcast right
// after the request it belongs to is exactly the context that resolves it.
func actionableMessages(source store.CollectionSource, messages []Message) ([]Message, string) {
	if strings.TrimSpace(source.ExcludePattern) == "" {
		return messages, ""
	}
	actionable := make([]Message, 0, len(messages))
	keyword := ""
	for _, message := range messages {
		// Matched against the rendered line, not the bare content, so a keyword
		// can still name a sender the way it could when this ran on the batch.
		matched, excluded := collectionExclusion(source, formatMessageContext([]Message{message}, nil))
		if !excluded {
			actionable = append(actionable, message)
			continue
		}
		if keyword == "" {
			keyword = matched
		}
	}
	if keyword == "" {
		return messages, ""
	}
	return actionable, keyword
}

func (service Service) processBatch(ctx context.Context, batch MessageBatch, item store.CollectionItem) (store.CollectionItem, error) {
	decision, err := service.decideBatch(ctx, batch)
	if err != nil {
		return item, err
	}
	return applyDecision(batch, item, decision)
}

// decideBatch classifies one batch without touching any Todo, so on-demand
// analysis can hold the decision for a person to confirm while automatic
// collection carries it out immediately.
func (service Service) decideBatch(ctx context.Context, batch MessageBatch) (Decision, error) {
	// Filtering already happened per message while the batch was built, so an
	// empty action context means every fresh line was noise. A batch rebuilt
	// from a stored item carries no keyword at all: someone asking for another
	// look is not asking the same keyword to answer again.
	if batch.ExcludedKeyword != "" && strings.TrimSpace(batch.ActionContext) == "" {
		return normalizeDecision(Decision{Action: "ignore", ItemType: "conversation",
			Reason: "命中来源排除规则：" + batch.ExcludedKeyword, Confidence: 1}, batch.Source), nil
	}
	if decision, settled, err := externalStateDecision(batch); err != nil {
		return Decision{}, err
	} else if settled {
		return normalizeDecision(decision, batch.Source), nil
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return Decision{}, err
	}
	decision, err := service.Extractor.Extract(ctx, batch, todos.Items)
	if err != nil {
		return Decision{}, err
	}
	decision = clampToStrategy(normalizeDecision(decision, batch.Source), batch.Source)
	return decision, nil
}

func collectionExclusion(source store.CollectionSource, context string) (string, bool) {
	keywords := strings.FieldsFunc(source.ExcludePattern, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r':
			return true
		default:
			return false
		}
	})
	lowerContext := strings.ToLower(context)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword != "" && strings.Contains(lowerContext, strings.ToLower(keyword)) {
			return keyword, true
		}
	}
	return "", false
}

func applyDecision(batch MessageBatch, item store.CollectionItem, decision Decision) (store.CollectionItem, error) {
	if err := validateDecision(decision); err != nil {
		return item, err
	}
	applyDecisionToItem(&item, decision)
	switch decision.Action {
	case "ignore":
		item.TodoID, item.Status = "", "processed"
	case "insight":
		// Nothing to carry out here: the item itself is the record, and the
		// source's daily digest turns it into knowledge. No Todo is touched, so
		// no [钉钉采集:] marker is written anywhere.
		item.TodoID, item.Status = "", "processed"
	case "create":
		todoID, err := createDecision(batch, decision)
		if err != nil {
			return item, err
		}
		item.TodoID, item.Status = todoID, "processed"
	case "append":
		todoID, err := appendDecision(batch, decision)
		if err != nil {
			return item, err
		}
		// The target was closed, deleted, or does not belong to this conversation.
		// Filing the batch as its own Todo is the wrong answer the classifier was
		// trying to avoid, but it is still better than marking the batch handled
		// with nothing written anywhere.
		if todoID == "" {
			// The only place an append's title is unavoidable. It is normally the
			// target's own, borrowed at classification time, but a target that was
			// already inactive then was never in the candidate list to borrow from.
			// An untitled Todo is worse than a retry.
			if strings.TrimSpace(decision.Title) == "" {
				return item, fmt.Errorf("collection model returned append without a title and target %s is not active",
					decision.RelatedTodoID)
			}
			todoID, err = createDecision(batch, decision)
			if err != nil {
				return item, err
			}
			item.Action = "create"
		}
		item.TodoID, item.Status = todoID, "processed"
	default:
		return item, fmt.Errorf("unsupported collection decision: %s", decision.Action)
	}
	return item, nil
}

type ItemCorrection struct {
	Title    *string `json:"title,omitempty"`
	Project  *string `json:"project,omitempty"`
	Priority *string `json:"priority,omitempty"`
}

// reprocessItem retries a failed or ignored audit item without advancing the
// source checkpoint. Processed writes must be explicitly reverted first so a
// reclassification cannot silently orphan an earlier Todo side effect.
func (service Service) reprocessItem(ctx context.Context, itemID string) (store.CollectionItem, error) {
	if service.Extractor == nil {
		return store.CollectionItem{}, application.NewError(application.CodeUnavailable, "collector extractor is required")
	}
	lock, err := acquireCollectionLock(ctx)
	if err != nil {
		return store.CollectionItem{}, err
	}
	defer lock.Close()
	db, err := store.Open()
	if err != nil {
		return store.CollectionItem{}, err
	}
	defer db.Close()
	item, err := getItemForUseCase(db, itemID)
	if err != nil {
		return item, err
	}
	if item.Action == "create" || item.Action == "append" {
		return item, itemConflict(
			fmt.Sprintf("collection item %s already changed Todo %s; revert it before reprocessing", item.ID, item.TodoID),
			item.ID,
		)
	}
	_, batch, err := loadItemBatch(db, item)
	if err != nil {
		return item, err
	}
	// An explicit reprocess is a fresh start, not a fourth attempt: someone asked
	// for this after fixing whatever broke, so the automatic retry gets its full
	// budget back and a batch that still fails will stop on its own again.
	item.Attempts = 0
	item.RetryStopped = false
	item, err = service.processBatch(ctx, batch, item)
	if err != nil {
		markItemFailed(db, &item, err)
		return item, err
	}
	if err := store.UpdateCollectionItem(db, item); err != nil {
		return item, err
	}
	return item, nil
}

// promoteItem turns an ignored or failed item into a concrete Todo using explicit
// user intent while retaining normal task-level deduplication.
func (service Service) promoteItem(itemID string, correction ItemCorrection) (store.CollectionItem, error) {
	db, err := store.Open()
	if err != nil {
		return store.CollectionItem{}, err
	}
	defer db.Close()
	item, err := getItemForUseCase(db, itemID)
	if err != nil {
		return item, err
	}
	if item.Action == "create" || item.Action == "append" {
		return item, itemConflict(fmt.Sprintf("collection item %s already has Todo %s", item.ID, item.TodoID), item.ID)
	}
	source, batch, err := loadItemBatch(db, item)
	if err != nil {
		return item, err
	}
	title := strings.TrimSpace(item.Title)
	if correction.Title != nil {
		title = strings.TrimSpace(*correction.Title)
	}
	if title == "" {
		title = collectionTitleFromContext(item.RawContext)
	}
	project := source.Project
	if item.Project != "" {
		project = item.Project
	}
	if correction.Project != nil {
		project = strings.TrimSpace(*correction.Project)
	}
	priority := empty(item.Priority, source.Priority)
	if correction.Priority != nil {
		priority = strings.ToUpper(strings.TrimSpace(*correction.Priority))
	}
	itemType := item.ItemType
	if itemType == "" || itemType == "conversation" {
		itemType = "follow_up"
	}
	// Confirming what an analysis proposed is a different act from rescuing
	// something the collector ignored, and the audit trail should say which.
	reason := "用户从收集处理记录手动转成 Todo"
	if item.ProposedAction != "" {
		reason = "用户确认按需分析的建议"
	}
	// Confirming a proposed append has to append. Promoting it into a new Todo
	// would produce exactly the duplicate the proposal was avoiding, and the
	// target's own guardrails still get to refuse — applyDecision falls back to
	// creating if the Todo has since been closed or belongs to another thread.
	action, relatedTodoID := "create", ""
	if item.ProposedAction == "append" && item.TodoID != "" {
		action, relatedTodoID = "append", item.TodoID
	}
	decision := normalizeDecision(Decision{Action: action, Title: title,
		Summary:  empty(strings.TrimSpace(item.Summary), strings.TrimSpace(item.RawContext)),
		ItemType: itemType, Project: project, Priority: priority,
		RelatedTodoID: relatedTodoID, Reason: reason, Confidence: 1}, source)
	item, err = applyDecision(batch, item, decision)
	if err != nil {
		markItemFailed(db, &item, err)
		return item, err
	}
	// The proposal has been carried out; nothing is waiting on a person now.
	item.ProposedAction = ""
	return item, store.UpdateCollectionItem(db, item)
}

// correctItem keeps the audit decision and its Todo metadata in sync. Nil fields
// mean unchanged; a non-nil empty project intentionally clears the mapping.
func (service Service) correctItem(itemID string, correction ItemCorrection) (store.CollectionItem, error) {
	db, err := store.Open()
	if err != nil {
		return store.CollectionItem{}, err
	}
	defer db.Close()
	item, err := getItemForUseCase(db, itemID)
	if err != nil {
		return item, err
	}
	if correction.Title != nil {
		item.Title = strings.TrimSpace(*correction.Title)
	}
	if correction.Project != nil {
		item.Project = strings.TrimSpace(*correction.Project)
	}
	if correction.Priority != nil {
		item.Priority = strings.ToUpper(strings.TrimSpace(*correction.Priority))
	}
	if item.TodoID != "" {
		var corrected store.Todo
		err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
			todo, err := transaction.Todo(item.TodoID)
			if err != nil {
				return linkedTodoConflict(
					fmt.Sprintf("collection item %s references unavailable Todo %s", item.ID, item.TodoID),
					item.ID,
					item.TodoID,
					err,
				)
			}
			if correction.Title != nil {
				todo.Title = item.Title
			}
			if correction.Project != nil {
				todo.Project = item.Project
			}
			if correction.Priority != nil {
				todo.Priority = item.Priority
			}
			corrected = *todo
			return nil
		})
		if err != nil {
			return item, err
		}
		if store.TodoDocExists(corrected.ID) {
			if err := store.SyncTodoDocMetadata(&corrected); err != nil {
				return item, err
			}
		}
	}
	return item, store.UpdateCollectionItem(db, item)
}

// revertItem preserves history: a newly created Todo is dropped, while an append
// gets an explicit compensating note instead of destructive document surgery.
func (service Service) revertItem(itemID string) (store.CollectionItem, error) {
	db, err := store.Open()
	if err != nil {
		return store.CollectionItem{}, err
	}
	defer db.Close()
	item, err := getItemForUseCase(db, itemID)
	if err != nil {
		return item, err
	}
	if item.TodoID == "" || (item.Action != "create" && item.Action != "append") {
		return item, itemConflict(fmt.Sprintf("collection item %s has no reversible Todo write", item.ID), item.ID)
	}
	if item.Action == "create" {
		source, batch, err := loadItemBatch(db, item)
		if err != nil {
			return item, err
		}
		_ = source
		var reverted store.Todo
		err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
			todo, err := transaction.Todo(item.TodoID)
			if err != nil {
				return linkedTodoConflict(
					fmt.Sprintf("collection item %s references unavailable Todo %s", item.ID, item.TodoID),
					item.ID,
					item.TodoID,
					err,
				)
			}
			if todo.Source != connectorSource(batch) {
				return itemConflict(
					fmt.Sprintf("refusing to archive Todo %s because its source no longer matches this collection item", todo.ID),
					item.ID,
				)
			}
			reverted = *todo
			if _, err := transaction.ArchiveTodos([]string{todo.ID}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return item, err
		}
		if store.TodoDocExists(reverted.ID) {
			if err := store.SyncTodoDocMetadata(&reverted); err != nil {
				return item, err
			}
		}
	} else {
		todos, err := store.LoadTodosReadOnly()
		if err != nil {
			return item, err
		}
		todo := store.FindTodo(todos, item.TodoID)
		if todo == nil {
			cause := store.TodoNotFoundError(todos, item.TodoID)
			return item, linkedTodoConflict(
				fmt.Sprintf("collection item %s references unavailable Todo %s", item.ID, item.TodoID),
				item.ID,
				item.TodoID,
				cause,
			)
		}
		fingerprintMarker := collectionRevertMarker(item.Fingerprint)
		note := fingerprintMarker + " 此前自动补充被用户标记为误判；原记录保留供审计。"
		if err := appendTodoLogOnce(todo, note, "补充", fingerprintMarker); err != nil {
			return item, err
		}
	}
	item.Action, item.Status, item.Reason = "reverted", "processed", "用户撤销了自动收集结果"
	return item, store.UpdateCollectionItem(db, item)
}

func loadItemBatch(db *sql.DB, item store.CollectionItem) (store.CollectionSource, MessageBatch, error) {
	source, err := store.GetCollectionSource(db, item.SourceID)
	if err != nil {
		return source, MessageBatch{}, err
	}
	messages := make([]Message, 0, len(item.MessageIDs))
	for index, id := range item.MessageIDs {
		message := Message{ID: id, ConversationID: item.ConversationID, Sender: item.Sender,
			CreatedAt: item.OccurredAt}
		if index == 0 {
			message.Content = item.RawContext
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		messages = append(messages, Message{ID: item.ID, ConversationID: item.ConversationID,
			Sender: item.Sender, CreatedAt: item.OccurredAt, Content: item.RawContext})
	}
	return source, MessageBatch{Source: source, Messages: messages,
		Fingerprint: item.Fingerprint, RawContext: item.RawContext}, nil
}

func collectionTitleFromContext(context string) string {
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(context), "\n")[0])
	if index := strings.Index(line, "] "); index >= 0 {
		line = strings.TrimSpace(line[index+2:])
	}
	if runes := []rune(line); len(runes) > 60 {
		line = string(runes[:60])
	}
	return empty(line, "处理收集到的事项")
}

// groupMessages splits a fetched window into topics — "same conversation, gaps
// under fifteen minutes" — and returns the messages of each. Turning a topic
// into batches belongs to the callers: automatic collection has to tell fresh
// messages from context, and both of them apply the source's decision unit.
func groupMessages(messages []Message) [][]Message {
	if len(messages) == 0 {
		return nil
	}
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].CreatedAt != messages[j].CreatedAt {
			return messages[i].CreatedAt < messages[j].CreatedAt
		}
		return messages[i].ID < messages[j].ID
	})
	groups := [][]Message{{messages[0]}}
	for _, message := range messages[1:] {
		current := &groups[len(groups)-1]
		previous := (*current)[len(*current)-1]
		if message.ConversationID == previous.ConversationID && message.CreatedAt-previous.CreatedAt <= int64((15*time.Minute).Seconds()) {
			*current = append(*current, message)
			continue
		}
		groups = append(groups, []Message{message})
	}
	return groups
}

// analysisBatches is the on-demand counterpart of groupMessagesWithContext: an
// explicit window has no checkpoint, so every message is eligible, but the
// source's decision unit and keyword filtering apply exactly as they do to an
// automatic run — otherwise "分析这段" would answer differently from collection.
func analysisBatches(source store.CollectionSource, messages []Message) []MessageBatch {
	batches := make([]MessageBatch, 0)
	for _, group := range groupMessages(messages) {
		for _, unit := range decisionUnits(source, group) {
			batches = append(batches, messageBatchWithContext(source, unit, group))
		}
	}
	return batches
}

func messageBatchFingerprint(sourceID string, messages []Message) string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	sort.Strings(ids)
	hash := sha256.Sum256([]byte(sourceID + "\x00" + strings.Join(ids, "\x00")))
	return hex.EncodeToString(hash[:])
}

func formatMessageContext(messages []Message, freshIDs map[string]struct{}) string {
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		prefix := ""
		if freshIDs != nil {
			if _, fresh := freshIDs[message.ID]; fresh {
				prefix = "[新消息] "
			} else {
				prefix = "[上下文] "
			}
		}
		stamp := time.Unix(message.CreatedAt, 0).In(config.Loc).Format("2006-01-02 15:04:05")
		lines = append(lines, fmt.Sprintf("%s%s [%s] %s", prefix, stamp,
			empty(message.Sender, "未知发送者"), message.Content))
	}
	return strings.Join(lines, "\n")
}

func itemFromBatch(batch MessageBatch, now int64) store.CollectionItem {
	messageIDs := make([]string, 0, len(batch.Messages))
	senderSet := map[string]struct{}{}
	latest := int64(0)
	conversation := batch.Source.ExternalID
	for _, message := range batch.Messages {
		messageIDs = append(messageIDs, message.ID)
		if message.Sender != "" {
			senderSet[message.Sender] = struct{}{}
		}
		if message.CreatedAt > latest {
			latest = message.CreatedAt
		}
		if message.ConversationID != "" {
			conversation = message.ConversationID
		}
	}
	senders := make([]string, 0, len(senderSet))
	for sender := range senderSet {
		senders = append(senders, sender)
	}
	sort.Strings(senders)
	return store.CollectionItem{SourceID: batch.Source.ID, Connector: batch.Source.Connector,
		ConversationID: conversation, Fingerprint: batch.Fingerprint, MessageIDs: messageIDs,
		Sender: strings.Join(senders, "、"), OccurredAt: latest, RawContext: batch.RawContext,
		Action: "pending", Status: "pending", CreatedAt: now, UpdatedAt: now}
}

func applyDecisionToItem(item *store.CollectionItem, decision Decision) {
	item.Action, item.Title, item.Summary, item.ItemType = decision.Action, decision.Title, decision.Summary, decision.ItemType
	item.Project, item.Priority, item.Reason = decision.Project, decision.Priority, decision.Reason
	item.Confidence, item.Error = decision.Confidence, ""
}

// markItemFailed records a failed attempt and spends one of the item's retries.
// The count is what later runs read to decide whether trying again is still
// worth a model call.
func markItemFailed(db *sql.DB, item *store.CollectionItem, err error) {
	item.Action, item.Status, item.Error = "failed", "failed", compactError(err)
	item.Attempts++
	item.RetryStopped = store.CollectionRetriesExhausted(*item)
	// The ceiling only holds if this write lands: an attempt that fails to record
	// itself leaves the count where it was, and the batch goes back to costing a
	// model call every run — the exact behaviour Attempts exists to stop. Still
	// not returned, because the caller's job is to report the failure that got us
	// here, but no longer silent either.
	if writeErr := store.UpdateCollectionItem(db, *item); writeErr != nil {
		logging.Failure("collection_item_failure_not_recorded", "", writeErr, map[string]any{
			"item":     item.ID,
			"attempts": item.Attempts,
		})
	}
}

func createDecision(batch MessageBatch, decision Decision) (string, error) {
	var relatedTodoID string
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return "", err
	}
	// Exact source markers win over semantic matching after a crash between Todo
	// creation and collection-item persistence.
	sourcePrefix := connectorSource(batch)
	for _, todo := range todos.Items {
		if store.TodoIsActive(todo) && todo.Source == sourcePrefix {
			return todo.ID, nil
		}
	}
	if related := store.FindTodo(todos, decision.RelatedTodoID); related != nil && store.TodoIsActive(*related) {
		relatedTodoID = related.ID
	}
	if relatedTodoID == "" {
		matches := store.MatchTodosWithOptions(todos, store.TodoMatchOptions{
			Project: decision.Project, Query: decision.Title + " " + decision.Summary,
			Limit: 1, MinQueryScore: 65, AllProjects: true,
		})
		if len(matches) > 0 {
			relatedTodoID = matches[0].ID
		}
	}
	var created store.Todo
	err = workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		todos := transaction.Todos()
		created = store.Todo{ID: store.NextTodoID(todos), Title: decision.Title,
			Description: todoDescription(decision, relatedTodoID), Priority: decision.Priority,
			Status: store.TodoStatusOpen, Project: decision.Project,
			Created: store.Today(), Source: connectorSource(batch),
			Creator: store.TodoCreatorCollect}
		todos.Items = append(todos.Items, created)
		return nil
	})
	if err != nil {
		return "", err
	}
	if _, err := store.EnsureTodoDoc(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// collectionSupplementMarker tags a 补充 that collection wrote, so the note can
// be tied back to the item that wrote it and written again idempotently.
//
// Named rather than inlined because it is a contract with a second codebase: the
// App matches this exact literal to keep the marker out of the task timeline
// (ATMTodoProgressEntry.displayText in
// app/macos/Sources/ATMMenuBarApp/Models.swift), and Todo documents on disk
// already carry it. It says 钉钉 even though nothing here is DingTalk-specific —
// that is the cost of a literal shared with documents already written, so a
// second connector's supplements will read 钉钉采集 until both sides learn a new
// tag and the old one stays understood.
const collectionSupplementMarker = "钉钉采集"

// appendDecision records what a batch adds to work an existing Todo already
// tracks, and reports which Todo it wrote to. An empty ID means the append could
// not be carried out and the caller has to fall back to creating.
//
// The target must be a Todo this same conversation filed. The classifier reads
// untrusted chat, so the one write it can direct at an existing record is held to
// the thread that produced it: a hand-written Todo, or one belonging to another
// group, cannot be edited by whatever a message claims to relate to.
func appendDecision(batch MessageBatch, decision Decision) (string, error) {
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return "", err
	}
	target := store.FindTodo(todos, decision.RelatedTodoID)
	if target == nil || !store.TodoIsActive(*target) {
		return "", nil
	}
	prefix := conversationSourcePrefix(batch)
	if prefix == "" || !strings.HasPrefix(target.Source, prefix) {
		return "", nil
	}
	// 补充 rather than 进展: this is context the chat added, not a milestone the
	// person working the Todo reached. It is also where Revert writes its
	// compensating note, so an append and its undo read as one thread.
	//
	// The fingerprint goes in an HTML comment because the App strips exactly this
	// shape out of the task timeline: the entry reads as the one sentence the chat
	// added, and the marker stays on disk to tie it back to the collection item.
	fingerprintMarker := collectionAppendMarker(batch.Fingerprint)
	note := strings.TrimSpace(decision.Summary) + "\n\n" + fingerprintMarker
	if err := appendTodoLogOnce(target, note, "补充", fingerprintMarker); err != nil {
		return "", err
	}
	return target.ID, nil
}

func collectionAppendMarker(fingerprint string) string {
	return "<!-- [" + collectionSupplementMarker + ":" + shortFingerprint(fingerprint) + "] -->"
}

func collectionRevertMarker(fingerprint string) string {
	return "[撤销" + collectionSupplementMarker + ":" + shortFingerprint(fingerprint) + "]"
}

// appendTodoLogOnce closes the retry window between a Todo document write and
// the collection item's SQLite update. A retry is keyed only by the stable
// fingerprint marker, never by model-produced summary text: two batches may add
// the same sentence, while one batch must never add its supplement twice.
//
// A missing card is normal because AppendTodoLog creates it. Other read errors
// are surfaced so a permission or I/O failure cannot be mistaken for absence
// and followed by another write attempt.
func appendTodoLogOnce(todo *store.Todo, note, section, fingerprintMarker string) error {
	return withTodoMarkerLock(todo.ID, func() error {
		content, err := store.ReadTodoDoc(todo.ID)
		switch {
		case err == nil:
			if strings.Contains(content, fingerprintMarker) {
				return nil
			}
		case errors.Is(err, fs.ErrNotExist):
			// AppendTodoLog initializes a missing Todo document.
		default:
			return fmt.Errorf("read Todo %s before appending collection marker: %w", todo.ID, err)
		}
		_, err = store.AppendTodoLog(todo, note, section)
		return err
	})
}

func todoDescription(decision Decision, relatedTodoID string) string {
	description := strings.TrimSpace(decision.Summary)
	if relatedTodoID == "" {
		return description
	}
	relation := "相关历史 Todo：" + relatedTodoID + "（仅作上下文关联，不合并事项）"
	if description == "" {
		return relation
	}
	return description + "\n\n" + relation
}

// conversationSourcePrefix is the Todo source marker every Todo filed from this
// batch's conversation starts with. connectorSource pins the message too, which
// identifies one batch; this identifies the thread.
func conversationSourcePrefix(batch MessageBatch) string {
	conversation := batch.Source.ExternalID
	if len(batch.Messages) > 0 && batch.Messages[0].ConversationID != "" {
		conversation = batch.Messages[0].ConversationID
	}
	if batch.Source.Connector == "" || conversation == "" {
		return ""
	}
	return batch.Source.Connector + ":" + conversation + ":"
}

func connectorSource(batch MessageBatch) string {
	messageID := ""
	if len(batch.Messages) > 0 {
		messageID = batch.Messages[0].ID
	}
	conversation := batch.Source.ExternalID
	if len(batch.Messages) > 0 && batch.Messages[0].ConversationID != "" {
		conversation = batch.Messages[0].ConversationID
	}
	return batch.Source.Connector + ":" + conversation + ":" + messageID
}

func runID(sourceID string, now time.Time) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", sourceID, now.UnixNano())))
	return "cr_" + hex.EncodeToString(hash[:8])
}

func sourceDisplayName(source store.CollectionSource) string {
	return empty(source.Name, source.ExternalID)
}

func empty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func shortFingerprint(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}
