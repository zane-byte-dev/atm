package collector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// These aliases are the established collection read model exposed by the
// application service. Delivery adapters may render them, but never open the
// store that produced them.
type Item = store.CollectionItem
type Digest = store.CollectionDigest
type MessageStats = store.CollectionMessageStats
type Overview = store.CollectionOverview

const MaxAttempts = store.MaxCollectionAttempts

func RetriesExhausted(item Item) bool { return store.CollectionRetriesExhausted(item) }

type SetEnabledInput struct {
	Enabled bool `json:"enabled"`
}

type SetEnabledResult struct {
	Enabled bool `json:"enabled"`
}

// SetEnabled owns the global collection switch. The caller supplies intent;
// config persistence and its normalized result stay behind the service edge.
func (service Service) SetEnabled(
	ctx context.Context,
	call application.Call,
	input SetEnabledInput,
) (SetEnabledResult, error) {
	if _, err := validateSourceCall(ctx, call, false); err != nil {
		return SetEnabledResult{}, err
	}
	apply := service.ApplyCollectionEnabled
	if apply == nil {
		apply = func(enabled bool) (bool, error) {
			settings, err := config.Default.Apply(config.SettingsPatch{CollectionEnabled: &enabled})
			return settings.CollectionEnabled, err
		}
	}
	enabled, err := apply(input.Enabled)
	if err != nil {
		var appErr *application.Error
		if errors.As(err, &appErr) {
			return SetEnabledResult{}, appErr
		}
		return SetEnabledResult{}, sourceUnavailable("change automatic collection state", err)
	}
	return SetEnabledResult{Enabled: enabled}, nil
}

type DigestCollectionInput struct {
	SourceID string `json:"source_id,omitempty"`
	Date     string `json:"date,omitempty"`
	DueOnly  bool   `json:"due_only,omitempty"`
	DryRun   bool   `json:"dry_run,omitempty"`
}

// DigestCollection is the application entry point for the older digest
// primitive, giving adapters the same validated Call boundary as run/history.
func (service Service) DigestCollection(
	ctx context.Context,
	call application.Call,
	input DigestCollectionInput,
) (DigestReport, error) {
	ctx, err := validateSourceCall(ctx, call, false)
	if err != nil {
		return DigestReport{}, err
	}
	report, err := service.Digest(ctx, strings.TrimSpace(input.SourceID), DigestOptions{
		Date: strings.TrimSpace(input.Date), DueOnly: input.DueOnly, DryRun: input.DryRun,
	})
	if err == nil {
		return report, nil
	}
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return report, appErr
	}
	return report, sourceUnavailable("digest collection results", err)
}

type SearchMessagesInput struct {
	Keyword   string `json:"keyword"`
	Source    string `json:"source,omitempty"`
	Sender    string `json:"sender,omitempty"`
	SinceUnix int64  `json:"since_unix,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Sync      bool   `json:"sync,omitempty"`
}

type SearchMessage struct {
	Connector        string `json:"connector"`
	ConversationID   string `json:"conversation_id"`
	MessageID        string `json:"message_id"`
	SourceID         string `json:"source_id,omitempty"`
	ConversationName string `json:"conversation_name,omitempty"`
	Sender           string `json:"sender,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	Content          string `json:"content"`
	SyncedAt         int64  `json:"synced_at,omitempty"`
}

type SearchMessagesResult struct {
	Keyword     string          `json:"keyword"`
	Matches     []SearchMessage `json:"matches"`
	Returned    int             `json:"returned"`
	SyncedFiles int             `json:"-"`
}

// SearchMessages resolves an optional configured source and searches the local
// archive. Both operations are one use case so Cobra never owns a DB handle.
func (service Service) SearchMessages(
	ctx context.Context,
	call application.Call,
	input SearchMessagesInput,
) (SearchMessagesResult, error) {
	if _, err := validateSourceCall(ctx, call, false); err != nil {
		return SearchMessagesResult{}, err
	}
	keyword := strings.TrimSpace(input.Keyword)
	if keyword == "" {
		return SearchMessagesResult{}, sourceInvalidArgument(
			"collection search keyword is required", "keyword", input.Keyword,
		)
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 1_000 {
		return SearchMessagesResult{}, sourceInvalidArgument(
			"collection search limit must be between 1 and 1000", "limit", input.Limit,
		)
	}
	if input.SinceUnix < 0 {
		return SearchMessagesResult{}, sourceInvalidArgument(
			"collection search timestamp cannot be negative", "since_unix", input.SinceUnix,
		)
	}
	db, syncedFiles, err := openSourceReadStore(input.Sync)
	if err != nil {
		return SearchMessagesResult{}, sourceUnavailable("search collection messages", err)
	}
	defer db.Close()
	query := store.CollectionMessageQuery{
		Sender: strings.TrimSpace(input.Sender), SinceTS: input.SinceUnix, Limit: limit,
	}
	if reference := strings.TrimSpace(input.Source); reference != "" {
		source, err := storedSource(db, reference)
		if err != nil {
			return SearchMessagesResult{}, err
		}
		query.Connector, query.ConversationID = source.Connector, source.ExternalID
	}
	stored, err := store.SearchCollectionMessages(db, keyword, query)
	if err != nil {
		return SearchMessagesResult{}, sourceUnavailable("search collection messages", err)
	}
	matches := make([]SearchMessage, 0, len(stored))
	for _, message := range stored {
		matches = append(matches, SearchMessage{
			Connector: message.Connector, ConversationID: message.ConversationID,
			MessageID: message.MessageID, SourceID: message.SourceID,
			ConversationName: message.ConversationName, Sender: message.Sender,
			CreatedAt: message.CreatedAt, Content: message.Content, SyncedAt: message.SyncedAt,
		})
	}
	return SearchMessagesResult{
		Keyword: keyword, Matches: matches, Returned: len(matches), SyncedFiles: syncedFiles,
	}, nil
}

type AnalyzeCollectionInput struct {
	Reference  string `json:"reference"`
	SinceUnix  int64  `json:"since_unix,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	MaxBatches int    `json:"max_batches,omitempty"`
	Local      bool   `json:"local,omitempty"`
	Apply      bool   `json:"apply,omitempty"`
}

// AnalyzeCollection resolves the configured source before invoking the domain
// primitive. A name and an ID therefore behave identically without the adapter
// reconstructing storage lookup rules.
func (service Service) AnalyzeCollection(
	ctx context.Context,
	call application.Call,
	input AnalyzeCollectionInput,
) (AnalyzeReport, error) {
	ctx, err := validateSourceCall(ctx, call, false)
	if err != nil {
		return AnalyzeReport{}, err
	}
	reference := strings.TrimSpace(input.Reference)
	if reference == "" {
		return AnalyzeReport{}, sourceInvalidArgument(
			"collection analysis source is required", "reference", input.Reference,
		)
	}
	if input.SinceUnix < 0 {
		return AnalyzeReport{}, sourceInvalidArgument(
			"collection analysis timestamp cannot be negative", "since_unix", input.SinceUnix,
		)
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return AnalyzeReport{}, sourceUnavailable("resolve collection analysis source", err)
	}
	source, err := storedSource(db, reference)
	db.Close()
	if err != nil {
		return AnalyzeReport{}, err
	}
	options := AnalyzeOptions{
		Limit: input.Limit, MaxBatches: input.MaxBatches, Local: input.Local, Apply: input.Apply,
	}
	if input.SinceUnix > 0 {
		options.Since = time.Unix(input.SinceUnix, 0)
	}
	report, err := service.Analyze(ctx, source.ID, options)
	if err == nil {
		return report, nil
	}
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return report, appErr
	}
	return report, sourceUnavailable("analyze collection messages", err)
}

func storedSource(db *sql.DB, reference string) (Source, error) {
	if source, err := store.GetCollectionSource(db, reference); err == nil {
		return source, nil
	}
	sources, err := store.ListCollectionSources(db, "", false)
	if err != nil {
		return Source{}, sourceUnavailable("resolve configured collection source", err)
	}
	for _, source := range sources {
		if strings.EqualFold(strings.TrimSpace(source.Name), reference) || source.ExternalID == reference {
			return source, nil
		}
	}
	notFound := application.NewError(
		application.CodeNotFound,
		fmt.Sprintf("没有这个来源：%s。用 atm collect source list 看已添加的来源", reference),
	)
	notFound.Details = map[string]any{"reference": reference}
	return Source{}, notFound
}
