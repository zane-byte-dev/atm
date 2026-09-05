package collector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Source is the connector-neutral source contract returned by the application
// service. The alias keeps the established JSON shape while preventing delivery
// adapters from reaching into persistence just to render a result.
type Source = store.CollectionSource

const (
	SourceStrategyTasks       = store.CollectionStrategyTasks
	SourceStrategyObserve     = store.CollectionStrategyObserve
	SourceDecisionUnitWindow  = store.CollectionDecisionUnitWindow
	SourceDecisionUnitMessage = store.CollectionDecisionUnitMessage
)

type ListSourcesInput struct {
	Connector   string `json:"connector,omitempty"`
	EnabledOnly bool   `json:"enabled_only,omitempty"`
	Sync        bool   `json:"sync,omitempty"`
}

type ListSourcesResult struct {
	Sources     []Source `json:"sources"`
	SyncedFiles int      `json:"-"`
}

type SearchSourcesInput struct {
	Connector string `json:"connector"`
	Kind      string `json:"kind,omitempty"`
	Keyword   string `json:"keyword"`
	Limit     int    `json:"limit,omitempty"`
}

type SearchSourcesResult struct {
	Candidates []Candidate `json:"candidates"`
}

type SaveSourceInput struct {
	Connector           string `json:"connector"`
	Kind                string `json:"kind"`
	ExternalID          string `json:"external_id"`
	Name                string `json:"name,omitempty"`
	Project             string `json:"project,omitempty"`
	ExcludePattern      string `json:"exclude_pattern,omitempty"`
	Instruction         string `json:"instruction,omitempty"`
	KnowledgeCollection string `json:"knowledge_collection,omitempty"`
	Strategy            string `json:"strategy,omitempty"`
	DecisionUnit        string `json:"decision_unit,omitempty"`
	IntervalMinutes     int    `json:"interval_minutes,omitempty"`
	Priority            string `json:"priority,omitempty"`
	Enabled             bool   `json:"enabled"`
}

type SaveSourceResult struct {
	Source Source `json:"source"`
}

type SetSourceEnabledInput struct {
	SourceID string `json:"source_id"`
	Enabled  bool   `json:"enabled"`
}

type SetSourceEnabledResult struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type SetSourceMutedInput struct {
	SourceID string `json:"source_id"`
	Muted    bool   `json:"muted"`
}

type SetSourceMutedResult struct {
	ID    string `json:"id"`
	Muted bool   `json:"muted"`
}

type DeleteSourceInput struct {
	SourceID  string `json:"source_id"`
	Confirmed bool   `json:"confirmed"`
}

type DeleteSourceResult struct {
	Source Source `json:"source"`
}

// ListSources is the persistence-owning read use case behind `collect source
// list`. Sync remains explicit input because the root CLI's --sync flag is an
// adapter concern, while actually opening and synchronizing the store is not.
func (service Service) ListSources(
	ctx context.Context,
	call application.Call,
	input ListSourcesInput,
) (ListSourcesResult, error) {
	_, err := validateSourceCall(ctx, call, false)
	if err != nil {
		return ListSourcesResult{}, err
	}

	connector := strings.ToLower(strings.TrimSpace(input.Connector))
	if connector != "" && !sourceTypePattern.MatchString(connector) {
		return ListSourcesResult{}, sourceInvalidArgument(
			"invalid collection connector", "connector", input.Connector,
		)
	}
	db, synced, err := openSourceReadStore(input.Sync)
	if err != nil {
		return ListSourcesResult{}, sourceUnavailable("list collection sources", err)
	}
	defer db.Close()
	sources, err := store.ListCollectionSources(db, connector, input.EnabledOnly)
	if err != nil {
		return ListSourcesResult{}, sourceUnavailable("list collection sources", err)
	}
	return ListSourcesResult{Sources: sources, SyncedFiles: synced}, nil
}

// SearchSources owns connector lookup, capability checking and invocation. A
// delivery adapter supplies typed search intent; it never chooses or calls an
// executable connector itself.
func (service Service) SearchSources(
	ctx context.Context,
	call application.Call,
	input SearchSourcesInput,
) (SearchSourcesResult, error) {
	ctx, err := validateSourceCall(ctx, call, false)
	if err != nil {
		return SearchSourcesResult{}, err
	}
	connectorID := strings.ToLower(strings.TrimSpace(input.Connector))
	if connectorID == "" {
		return SearchSourcesResult{}, sourceInvalidArgument(
			"collection connector is required", "connector", input.Connector,
		)
	}
	if !sourceTypePattern.MatchString(connectorID) {
		return SearchSourcesResult{}, sourceInvalidArgument(
			"invalid collection connector", "connector", input.Connector,
		)
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = DirectoryKindAll
	}
	if kind != DirectoryKindAll && kind != DirectoryKindGroup && kind != DirectoryKindUser && kind != DirectoryKindBot {
		return SearchSourcesResult{}, sourceInvalidArgument(
			"search kind must be group, user, bot, or all", "kind", input.Kind,
		)
	}
	keyword := strings.TrimSpace(input.Keyword)
	if keyword == "" {
		return SearchSourcesResult{}, sourceInvalidArgument(
			"source search keyword is required", "keyword", input.Keyword,
		)
	}
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 {
		return SearchSourcesResult{}, sourceInvalidArgument(
			"source search limit must be positive", "limit", input.Limit,
		)
	}

	registry, err := service.sourceRegistry()
	if err != nil {
		return SearchSourcesResult{}, err
	}
	connector, err := registry.Resolve(connectorID)
	if err != nil {
		return SearchSourcesResult{}, sourceConnectorNotFound(connectorID, err)
	}
	searcher, ok := connector.(SearchConnector)
	if !ok {
		appErr := application.NewError(
			application.CodeConflict,
			fmt.Sprintf("collection connector %s does not support source search", connectorID),
		)
		appErr.Details = map[string]any{"connector": connectorID, "capability": "search"}
		return SearchSourcesResult{}, appErr
	}
	candidates, err := searcher.Search(ctx, kind, keyword, limit)
	if err != nil {
		appErr := sourceUnavailable("search collection sources", err)
		appErr.Details = map[string]any{"connector": connectorID}
		return SearchSourcesResult{}, appErr
	}
	if candidates == nil {
		candidates = []Candidate{}
	}
	return SearchSourcesResult{Candidates: candidates}, nil
}

// SaveSource adds or edits user-authored collection configuration. Source
// management is control-plane state, so Agents and controllers may inspect and
// use sources but cannot silently reconfigure them.
func (service Service) SaveSource(
	ctx context.Context,
	call application.Call,
	input SaveSourceInput,
) (SaveSourceResult, error) {
	_, err := validateSourceCall(ctx, call, true)
	if err != nil {
		return SaveSourceResult{}, err
	}
	source, err := validateSaveSourceInput(input)
	if err != nil {
		return SaveSourceResult{}, err
	}
	registry, err := service.sourceRegistry()
	if err != nil {
		return SaveSourceResult{}, err
	}
	if _, err := registry.Resolve(source.Connector); err != nil {
		return SaveSourceResult{}, sourceConnectorNotFound(source.Connector, err)
	}
	db, err := store.Open()
	if err != nil {
		return SaveSourceResult{}, sourceUnavailable("save collection source", err)
	}
	defer db.Close()
	saved, err := store.UpsertCollectionSource(db, source)
	if err != nil {
		return SaveSourceResult{}, sourceUnavailable("save collection source", err)
	}
	return SaveSourceResult{Source: saved}, nil
}

func (service Service) SetSourceEnabled(
	ctx context.Context,
	call application.Call,
	input SetSourceEnabledInput,
) (SetSourceEnabledResult, error) {
	_, sourceID, err := validateSourceMutation(ctx, call, input.SourceID)
	if err != nil {
		return SetSourceEnabledResult{}, err
	}
	db, source, err := openExistingSource(sourceID, "change collection source state")
	if err != nil {
		return SetSourceEnabledResult{}, err
	}
	defer db.Close()
	if err := store.SetCollectionSourceEnabled(db, source.ID, input.Enabled); err != nil {
		return SetSourceEnabledResult{}, sourceStoreError("change collection source state", source.ID, err)
	}
	return SetSourceEnabledResult{ID: source.ID, Enabled: input.Enabled}, nil
}

func (service Service) SetSourceMuted(
	ctx context.Context,
	call application.Call,
	input SetSourceMutedInput,
) (SetSourceMutedResult, error) {
	_, sourceID, err := validateSourceMutation(ctx, call, input.SourceID)
	if err != nil {
		return SetSourceMutedResult{}, err
	}
	db, source, err := openExistingSource(sourceID, "change collection source notification state")
	if err != nil {
		return SetSourceMutedResult{}, err
	}
	defer db.Close()
	if err := store.SetCollectionSourceMuted(db, source.ID, input.Muted); err != nil {
		return SetSourceMutedResult{}, sourceStoreError(
			"change collection source notification state", source.ID, err,
		)
	}
	return SetSourceMutedResult{ID: source.ID, Muted: input.Muted}, nil
}

func (service Service) DeleteSource(
	ctx context.Context,
	call application.Call,
	input DeleteSourceInput,
) (DeleteSourceResult, error) {
	ctx, sourceID, err := validateSourceMutation(ctx, call, input.SourceID)
	if err != nil {
		return DeleteSourceResult{}, err
	}
	if !input.Confirmed {
		return DeleteSourceResult{}, sourceInvalidArgument(
			"deleting a collection source requires confirmation", "confirmed", false,
		)
	}
	lock, err := acquireCollectionLock(ctx)
	if err != nil {
		return DeleteSourceResult{}, sourceUnavailable("delete collection source", err)
	}
	defer lock.Close()
	db, source, err := openExistingSource(sourceID, "delete collection source")
	if err != nil {
		return DeleteSourceResult{}, err
	}
	defer db.Close()
	if err := store.DeleteCollectionSource(db, source.ID); err != nil {
		return DeleteSourceResult{}, sourceStoreError("delete collection source", source.ID, err)
	}
	return DeleteSourceResult{Source: source}, nil
}

var sourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func validateSaveSourceInput(input SaveSourceInput) (Source, error) {
	source := Source{
		Connector:           strings.ToLower(strings.TrimSpace(input.Connector)),
		Kind:                strings.ToLower(strings.TrimSpace(input.Kind)),
		ExternalID:          strings.TrimSpace(input.ExternalID),
		Name:                input.Name,
		Project:             input.Project,
		ExcludePattern:      input.ExcludePattern,
		Instruction:         input.Instruction,
		KnowledgeCollection: input.KnowledgeCollection,
		Strategy:            strings.ToLower(strings.TrimSpace(input.Strategy)),
		DecisionUnit:        strings.ToLower(strings.TrimSpace(input.DecisionUnit)),
		IntervalMinutes:     input.IntervalMinutes,
		Priority:            strings.ToUpper(strings.TrimSpace(input.Priority)),
		Enabled:             input.Enabled,
	}
	if source.Connector == "" {
		return Source{}, sourceInvalidArgument("collection connector is required", "connector", input.Connector)
	}
	if !sourceTypePattern.MatchString(source.Connector) {
		return Source{}, sourceInvalidArgument("invalid collection connector", "connector", input.Connector)
	}
	if source.Kind == "" {
		return Source{}, sourceInvalidArgument("collection source kind is required", "kind", input.Kind)
	}
	if !sourceTypePattern.MatchString(source.Kind) {
		return Source{}, sourceInvalidArgument("invalid collection source kind", "kind", input.Kind)
	}
	if source.ExternalID == "" {
		return Source{}, sourceInvalidArgument("collection source external ID is required", "external_id", input.ExternalID)
	}
	if source.Priority == "" {
		source.Priority = "P2"
	}
	if source.Priority != "P0" && source.Priority != "P1" && source.Priority != "P2" && source.Priority != "P3" {
		return Source{}, sourceInvalidArgument("invalid collection source priority", "priority", input.Priority)
	}
	if source.Strategy == "" {
		source.Strategy = SourceStrategyTasks
	}
	if source.Strategy != SourceStrategyTasks && source.Strategy != SourceStrategyObserve {
		return Source{}, sourceInvalidArgument("invalid collection source strategy", "strategy", input.Strategy)
	}
	if source.DecisionUnit == "" {
		source.DecisionUnit = SourceDecisionUnitWindow
	}
	if source.DecisionUnit != SourceDecisionUnitWindow && source.DecisionUnit != SourceDecisionUnitMessage {
		return Source{}, sourceInvalidArgument("invalid collection decision unit", "decision_unit", input.DecisionUnit)
	}
	if source.IntervalMinutes < 0 || source.IntervalMinutes > 1440 {
		return Source{}, sourceInvalidArgument(
			"collection interval must be zero for the strategy default or between 1 and 1440 minutes",
			"interval_minutes", input.IntervalMinutes,
		)
	}
	return source, nil
}

func validateSourceMutation(
	ctx context.Context,
	call application.Call,
	sourceID string,
) (context.Context, string, error) {
	ctx, err := validateSourceCall(ctx, call, true)
	if err != nil {
		return ctx, "", err
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return ctx, "", sourceInvalidArgument("collection source ID is required", "source_id", sourceID)
	}
	return ctx, sourceID, nil
}

func validateSourceCall(
	ctx context.Context,
	call application.Call,
	mutation bool,
) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ctx, sourceUnavailable("serve collection source request", err)
	}
	if err := call.Validate(); err != nil {
		return ctx, err
	}
	if mutation && (call.Actor.Kind != application.ActorHuman ||
		(call.Actor.Origin != application.OriginCLI && call.Actor.Origin != application.OriginWeb)) {
		err := application.NewError(
			application.CodeForbidden,
			"only a human through the CLI or Web workspace may change collection sources",
		)
		err.Details = map[string]any{
			"actor_kind": call.Actor.Kind,
			"origin":     call.Actor.Origin,
		}
		return ctx, err
	}
	return ctx, nil
}

func (service Service) sourceRegistry() (*Registry, error) {
	if service.RegistryError != nil {
		return nil, sourceUnavailable("load collection connector registry", service.RegistryError)
	}
	if service.Connectors != nil {
		return service.Connectors, nil
	}
	registry, err := DefaultRegistry()
	if err != nil {
		return nil, sourceUnavailable("load collection connector registry", err)
	}
	return registry, nil
}

func openSourceReadStore(syncBeforeRead bool) (*sql.DB, int, error) {
	if !syncBeforeRead {
		db, err := store.OpenReadOnly()
		return db, 0, err
	}
	db, err := store.Open()
	if err != nil {
		return nil, 0, err
	}
	synced, err := store.SyncAll(db)
	if err != nil {
		db.Close()
		return nil, 0, err
	}
	return db, synced, nil
}

func openExistingSource(sourceID, operation string) (*sql.DB, Source, error) {
	db, err := store.Open()
	if err != nil {
		return nil, Source{}, sourceUnavailable(operation, err)
	}
	source, err := store.GetCollectionSource(db, sourceID)
	if err == nil {
		return db, source, nil
	}
	db.Close()
	return nil, Source{}, sourceStoreError(operation, sourceID, err)
}

func sourceStoreError(operation, sourceID string, cause error) error {
	var appErr *application.Error
	if errors.As(cause, &appErr) {
		return appErr
	}
	if errors.Is(cause, sql.ErrNoRows) || strings.Contains(cause.Error(), "collection source not found") {
		err := application.WrapError(
			application.CodeNotFound,
			fmt.Sprintf("collection source not found: %s", sourceID),
			cause,
		)
		err.Details = map[string]any{"source_id": sourceID}
		return err
	}
	err := sourceUnavailable(operation, cause)
	err.Details = map[string]any{"source_id": sourceID}
	return err
}

func sourceConnectorNotFound(connectorID string, cause error) *application.Error {
	err := application.WrapError(
		application.CodeNotFound,
		fmt.Sprintf("collection connector is not registered: %s", connectorID),
		cause,
	)
	err.Details = map[string]any{"connector": connectorID}
	return err
}

func sourceInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func sourceUnavailable(operation string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, operation, cause)
	err.Retryable = true
	return err
}
