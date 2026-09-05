package collector

import (
	"context"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type HistoryInput struct {
	Reference string `json:"reference"`
	Connector string `json:"connector,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SinceUnix int64  `json:"since_unix,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Local     bool   `json:"local,omitempty"`
	Sync      bool   `json:"sync,omitempty"`
}

type HistorySource struct {
	ID         string `json:"id,omitempty"`
	Connector  string `json:"connector"`
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name,omitempty"`
}

type HistoryResult struct {
	Source   HistorySource `json:"source"`
	Messages []Message     `json:"messages"`
	Synced   int           `json:"synced"`
	Stale    bool          `json:"stale,omitempty"`
	Error    string        `json:"error,omitempty"`
	// SyncedFiles belongs to the CLI root --sync presentation and is not part of
	// the browser contract.
	SyncedFiles int `json:"-"`
}

func (service Service) History(
	ctx context.Context,
	call application.Call,
	input HistoryInput,
) (HistoryResult, error) {
	ctx, err := validateSourceCall(ctx, call, false)
	if err != nil {
		return HistoryResult{}, err
	}
	reference := strings.TrimSpace(input.Reference)
	if reference == "" {
		return HistoryResult{}, sourceInvalidArgument(
			"collection history reference is required", "reference", input.Reference,
		)
	}
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 1_000 {
		return HistoryResult{}, sourceInvalidArgument(
			"collection history limit must be between 1 and 1000", "limit", input.Limit,
		)
	}
	if input.SinceUnix < 0 {
		return HistoryResult{}, sourceInvalidArgument(
			"collection history since timestamp cannot be negative", "since_unix", input.SinceUnix,
		)
	}

	source, syncedFiles, err := service.resolveHistorySource(ctx, call, reference, input)
	if err != nil {
		return HistoryResult{}, err
	}
	result := HistoryResult{Source: historySourceOf(source), Messages: []Message{}, SyncedFiles: syncedFiles}
	options := HistoryOptions{Limit: limit}
	if input.SinceUnix > 0 {
		options.Since = time.Unix(input.SinceUnix, 0)
	}
	if input.Local {
		result.Messages, err = historyMessagesFromStore(source, options)
	} else {
		result, err = service.syncHistory(ctx, source, options, result)
	}
	if err != nil {
		return HistoryResult{}, err
	}
	if err := pruneHistoryMessages(service.now()); err != nil {
		return HistoryResult{}, sourceUnavailable("prune collection message archive", err)
	}
	return result, nil
}

func (service Service) resolveHistorySource(
	ctx context.Context,
	call application.Call,
	reference string,
	input HistoryInput,
) (Source, int, error) {
	db, synced, openErr := openSourceReadStore(input.Sync)
	if openErr == nil {
		defer db.Close()
		if source, err := store.GetCollectionSource(db, reference); err == nil {
			return source, synced, nil
		}
		sources, err := store.ListCollectionSources(db, "", false)
		if err != nil {
			return Source{}, synced, sourceUnavailable("resolve collection history source", err)
		}
		for _, source := range sources {
			if strings.EqualFold(strings.TrimSpace(source.Name), reference) {
				return source, synced, nil
			}
		}
	}

	connectorID := strings.ToLower(strings.TrimSpace(input.Connector))
	if connectorID == "" {
		if openErr != nil {
			return Source{}, synced, sourceUnavailable("resolve collection history source", openErr)
		}
		notFound := application.NewError(
			application.CodeNotFound,
			"collection source is not configured: "+reference,
		)
		notFound.Details = map[string]any{"reference": reference}
		return Source{}, synced, notFound
	}
	search, err := service.SearchSources(ctx, call, SearchSourcesInput{
		Connector: connectorID,
		Kind:      input.Kind,
		Keyword:   reference,
		Limit:     10,
	})
	if err != nil {
		return Source{}, synced, err
	}
	candidates := search.Candidates
	if len(candidates) == 0 {
		notFound := application.NewError(application.CodeNotFound, "collection source search returned no matches")
		notFound.Details = map[string]any{"connector": connectorID, "reference": reference}
		return Source{}, synced, notFound
	}
	named := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if MatchesName(candidate, reference) {
			named = append(named, candidate)
		}
	}
	selected := Candidate{}
	switch {
	case len(named) == 1:
		selected = named[0]
	case len(candidates) == 1:
		selected = candidates[0]
	default:
		if len(named) > 1 {
			candidates = named
		}
		conflict := application.NewError(application.CodeConflict, "collection source reference is ambiguous")
		conflict.Details = map[string]any{
			"connector":  connectorID,
			"reference":  reference,
			"candidates": candidates,
		}
		return Source{}, synced, conflict
	}
	return Source{
		Connector:  connectorID,
		Kind:       selected.Kind,
		ExternalID: selected.ExternalID,
		Name:       selected.Name,
	}, synced, nil
}

func (service Service) syncHistory(
	ctx context.Context,
	source Source,
	options HistoryOptions,
	result HistoryResult,
) (HistoryResult, error) {
	registry, err := service.sourceRegistry()
	if err != nil {
		return HistoryResult{}, err
	}
	connector, err := registry.Resolve(source.Connector)
	if err != nil {
		return HistoryResult{}, sourceConnectorNotFound(source.Connector, err)
	}
	historian, ok := connector.(HistoryConnector)
	if !ok {
		conflict := application.NewError(
			application.CodeConflict,
			"collection connector does not support history: "+source.Connector,
		)
		conflict.Details = map[string]any{"connector": source.Connector, "capability": "history"}
		return HistoryResult{}, conflict
	}
	messages, historyErr := historian.History(ctx, source, options)
	if historyErr != nil {
		local, localErr := historyMessagesFromStore(source, options)
		if localErr != nil || len(local) == 0 {
			return HistoryResult{}, sourceUnavailable("read collection history", historyErr)
		}
		result.Messages = local
		result.Stale = true
		result.Error = compactHistoryError(historyErr)
		return result, nil
	}
	if messages == nil {
		messages = []Message{}
	}
	result.Messages = messages
	db, err := store.Open()
	if err != nil {
		return HistoryResult{}, sourceUnavailable("save collection history", err)
	}
	defer db.Close()
	result.Synced, err = store.PutCollectionMessages(db, CollectionMessagesFor(source, messages))
	if err != nil {
		return HistoryResult{}, sourceUnavailable("save collection history", err)
	}
	return result, nil
}

func historyMessagesFromStore(source Source, options HistoryOptions) ([]Message, error) {
	db, err := store.OpenReadOnly()
	if err != nil {
		return nil, sourceUnavailable("read local collection history", err)
	}
	defer db.Close()
	query := store.CollectionMessageQuery{
		Connector: source.Connector, ConversationID: source.ExternalID, Limit: options.Limit,
	}
	if !options.Since.IsZero() {
		query.SinceTS = options.Since.Unix()
	}
	stored, err := store.ListCollectionMessages(db, query)
	if err != nil {
		return nil, sourceUnavailable("read local collection history", err)
	}
	messages := make([]Message, 0, len(stored))
	for _, message := range stored {
		messages = append(messages, Message{
			ID: message.MessageID, ConversationID: message.ConversationID,
			Sender: message.Sender, CreatedAt: message.CreatedAt, Content: message.Content,
		})
	}
	return messages, nil
}

func pruneHistoryMessages(now time.Time) error {
	cutoff := store.RetentionCutoff(config.CollectionMessageRetentionDays, now)
	if cutoff <= 0 {
		return nil
	}
	db, err := store.Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = store.PruneCollectionMessages(db, cutoff)
	return err
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now().In(config.Loc)
}

func historySourceOf(source Source) HistorySource {
	return HistorySource{
		ID: source.ID, Connector: source.Connector, Kind: source.Kind,
		ExternalID: source.ExternalID, Name: source.Name,
	}
}

func compactHistoryError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[0])
	if len(message) > 160 {
		return message[:160] + "…"
	}
	return message
}
