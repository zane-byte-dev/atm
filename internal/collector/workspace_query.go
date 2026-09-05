package collector

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// QueryService owns the read-only collection ledger projection used by the
// resident workspace. Unlike Snapshot and History, these queries never open a
// migration-capable handle, call a connector, prune history, or invoke a model.
type QueryService struct{}

type WorkspaceCollectionSource struct {
	ID                  string `json:"id"`
	Connector           string `json:"connector"`
	Kind                string `json:"kind"`
	ExternalID          string `json:"external_id"`
	Name                string `json:"name,omitempty"`
	Project             string `json:"project,omitempty"`
	ExcludePattern      string `json:"exclude_pattern,omitempty"`
	Instruction         string `json:"instruction,omitempty"`
	KnowledgeCollection string `json:"knowledge_collection,omitempty"`
	Strategy            string `json:"strategy"`
	DecisionUnit        string `json:"decision_unit"`
	IntervalMinutes     int    `json:"interval_minutes"`
	Priority            string `json:"priority"`
	Enabled             bool   `json:"enabled"`
	Muted               bool   `json:"muted"`
	CreatedAt           int64  `json:"created_at"`
	UpdatedAt           int64  `json:"updated_at"`
}

type WorkspaceCollectionRun struct {
	ID            string `json:"id"`
	Connector     string `json:"connector"`
	SourceID      string `json:"source_id,omitempty"`
	Status        string `json:"status"`
	StartedAt     int64  `json:"started_at"`
	FinishedAt    int64  `json:"finished_at,omitempty"`
	FetchedCount  int    `json:"fetched_count"`
	AnalyzedCount int    `json:"analyzed_count"`
	CreatedCount  int    `json:"created_count"`
	AppendedCount int    `json:"appended_count"`
	InsightCount  int    `json:"insight_count"`
	IgnoredCount  int    `json:"ignored_count"`
	FailedCount   int    `json:"failed_count"`
	Error         string `json:"error,omitempty"`
}

type WorkspaceCollectionSummary struct {
	Sources         int `json:"sources"`
	Enabled         int `json:"enabled_sources"`
	Fetched         int `json:"fetched_today"`
	Created         int `json:"created_today"`
	Appended        int `json:"appended_today"`
	Insight         int `json:"insight_today"`
	Ignored         int `json:"ignored_today"`
	Failed          int `json:"failed_today"`
	Followups       int `json:"followups"`
	FollowupsClosed int `json:"followups_closed"`
	RetryStopped    int `json:"retry_stopped"`
	Unread          int `json:"unread_count"`
	Settleable      int `json:"settleable_count"`
}

type WorkspaceCollectionMessageStats struct {
	Total         int   `json:"total"`
	Conversations int   `json:"conversations"`
	Oldest        int64 `json:"oldest,omitempty"`
	Newest        int64 `json:"newest,omitempty"`
}

type WorkspaceCollectionOverview struct {
	Summary  WorkspaceCollectionSummary      `json:"summary"`
	Sources  []WorkspaceCollectionSource     `json:"sources"`
	Runs     []WorkspaceCollectionRun        `json:"runs"`
	Messages WorkspaceCollectionMessageStats `json:"messages"`
}

type WorkspaceCollectionListInput struct {
	SourceID string
	State    string
	Query    string
	Limit    int
	Offset   int
}

type WorkspaceCollectionItemSummary struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	Connector      string `json:"connector"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Sender         string `json:"sender"`
	Action         string `json:"action"`
	Status         string `json:"status"`
	Project        string `json:"project"`
	TodoID         string `json:"todo_id"`
	ReadAt         int64  `json:"read_at"`
	ArchivedAt     int64  `json:"archived_at"`
	OccurredAt     int64  `json:"occurred_at"`
	UpdatedAt      int64  `json:"updated_at"`
	ProposedAction string `json:"proposed_action"`
}

type WorkspaceCollectionList struct {
	Items  []WorkspaceCollectionItemSummary `json:"items"`
	Total  int                              `json:"total"`
	Limit  int                              `json:"limit"`
	Offset int                              `json:"offset"`
}

type WorkspaceCollectionItem struct {
	ID                  string   `json:"id"`
	SourceID            string   `json:"source_id"`
	Connector           string   `json:"connector"`
	ConversationID      string   `json:"conversation_id,omitempty"`
	Fingerprint         string   `json:"fingerprint"`
	MessageIDs          []string `json:"message_ids"`
	Sender              string   `json:"sender,omitempty"`
	OccurredAt          int64    `json:"occurred_at,omitempty"`
	RawContext          string   `json:"raw_context,omitempty"`
	Action              string   `json:"action"`
	ProposedAction      string   `json:"proposed_action,omitempty"`
	Title               string   `json:"title,omitempty"`
	Summary             string   `json:"summary,omitempty"`
	ItemType            string   `json:"item_type,omitempty"`
	Project             string   `json:"project,omitempty"`
	Priority            string   `json:"priority,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	Confidence          float64  `json:"confidence,omitempty"`
	KnowledgeDocumentID string   `json:"knowledge_document_id,omitempty"`
	KnowledgeCollection string   `json:"knowledge_collection,omitempty"`
	TodoID              string   `json:"todo_id,omitempty"`
	Status              string   `json:"status"`
	ReadAt              int64    `json:"read_at"`
	ArchivedAt          int64    `json:"archived_at"`
	Attempts            int      `json:"attempts,omitempty"`
	RetryStopped        bool     `json:"retry_stopped,omitempty"`
	Error               string   `json:"error,omitempty"`
	CreatedAt           int64    `json:"created_at"`
	UpdatedAt           int64    `json:"updated_at"`
	TodoStatus          string   `json:"todo_status,omitempty"`
	TodoArchived        bool     `json:"todo_archived,omitempty"`
}

type WorkspaceCollectionMessage struct {
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

type WorkspaceCollectionHistoryInput struct {
	SourceID string
	Query    string
	Limit    int
}

type WorkspaceCollectionHistory struct {
	Source   WorkspaceCollectionSource    `json:"source"`
	Messages []WorkspaceCollectionMessage `json:"messages"`
	Local    bool                         `json:"local"`
	Limit    int                          `json:"limit"`
}

func (QueryService) Overview(ctx context.Context) (WorkspaceCollectionOverview, error) {
	result := WorkspaceCollectionOverview{Sources: []WorkspaceCollectionSource{}, Runs: []WorkspaceCollectionRun{}}
	if err := validateWorkspaceCollectionQueryContext(ctx); err != nil {
		return result, err
	}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	defer db.Close()
	overview, err := store.LoadCollectionOverview(db, 1)
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	result.Summary = workspaceCollectionSummary(overview.Summary)
	for _, source := range overview.Sources {
		result.Sources = append(result.Sources, workspaceCollectionSource(source))
	}
	for _, run := range overview.Runs {
		result.Runs = append(result.Runs, workspaceCollectionRun(run))
	}
	stats, err := store.CollectionMessageStatsFor(db)
	result.Messages = workspaceCollectionMessageStats(stats)
	return result, workspaceCollectionQueryError(err)
}

func (QueryService) Items(ctx context.Context, input WorkspaceCollectionListInput) (WorkspaceCollectionList, error) {
	if err := validateWorkspaceCollectionQueryContext(ctx); err != nil {
		return WorkspaceCollectionList{}, err
	}
	where := " WHERE (?='' OR source_id=?)"
	args := []any{input.SourceID, input.SourceID}
	switch input.State {
	case "", "active":
		where += " AND archived_at=0 AND action<>'ignore'"
	case "unread":
		where += " AND archived_at=0 AND read_at=0"
	case "read":
		where += " AND archived_at=0 AND read_at>0"
	case "archived":
		where += " AND archived_at>0"
	case "all":
	default:
		return WorkspaceCollectionList{}, application.NewError(application.CodeInvalidArgument, "invalid collection state")
	}
	if query := strings.TrimSpace(input.Query); query != "" {
		where += " AND instr(lower(title || char(10) || summary || char(10) || raw_context || char(10) || sender || char(10) || project),lower(?))>0"
		args = append(args, query)
	}
	result := WorkspaceCollectionList{Items: []WorkspaceCollectionItemSummary{}, Limit: input.Limit, Offset: input.Offset}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM collection_items"+where, args...).Scan(&result.Total); err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,source_id,connector,title,substr(summary,1,480),sender,action,status,
		project,COALESCE(todo_id,''),read_at,archived_at,occurred_at,updated_at,proposed_action
		FROM collection_items`+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, append(args, input.Limit, input.Offset)...)
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item WorkspaceCollectionItemSummary
		if err := rows.Scan(&item.ID, &item.SourceID, &item.Connector, &item.Title, &item.Summary, &item.Sender,
			&item.Action, &item.Status, &item.Project, &item.TodoID, &item.ReadAt, &item.ArchivedAt,
			&item.OccurredAt, &item.UpdatedAt, &item.ProposedAction); err != nil {
			return result, workspaceCollectionQueryError(err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	return result, workspaceCollectionQueryError(tx.Commit())
}

func (QueryService) Item(ctx context.Context, id string) (WorkspaceCollectionItem, error) {
	if err := validateWorkspaceCollectionQueryContext(ctx); err != nil {
		return WorkspaceCollectionItem{}, err
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return WorkspaceCollectionItem{}, workspaceCollectionQueryError(err)
	}
	defer db.Close()
	item, err := store.GetCollectionItem(db, id)
	return workspaceCollectionItem(item), workspaceCollectionQueryError(err)
}

func (QueryService) History(ctx context.Context, input WorkspaceCollectionHistoryInput) (WorkspaceCollectionHistory, error) {
	result := WorkspaceCollectionHistory{Messages: []WorkspaceCollectionMessage{}, Local: true, Limit: input.Limit}
	if err := validateWorkspaceCollectionQueryContext(ctx); err != nil {
		return result, err
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	defer db.Close()
	source, err := store.GetCollectionSource(db, input.SourceID)
	if err != nil {
		return result, workspaceCollectionQueryError(err)
	}
	result.Source = workspaceCollectionSource(source)
	query := store.CollectionMessageQuery{Connector: source.Connector, ConversationID: source.ExternalID, Limit: input.Limit}
	var messages []store.CollectionMessage
	if strings.TrimSpace(input.Query) != "" {
		messages, err = store.SearchCollectionMessages(db, strings.TrimSpace(input.Query), query)
	} else {
		messages, err = store.ListCollectionMessages(db, query)
	}
	for _, message := range messages {
		result.Messages = append(result.Messages, workspaceCollectionMessage(message))
	}
	return result, workspaceCollectionQueryError(err)
}

func (QueryService) Source(ctx context.Context, id string) (WorkspaceCollectionSource, error) {
	if err := validateWorkspaceCollectionQueryContext(ctx); err != nil {
		return WorkspaceCollectionSource{}, err
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return WorkspaceCollectionSource{}, workspaceCollectionQueryError(err)
	}
	defer db.Close()
	source, err := store.GetCollectionSource(db, id)
	return workspaceCollectionSource(source), workspaceCollectionQueryError(err)
}

// VerifyCurrentSchema is a read-only precondition. Older accounts stay fully
// read-only instead of being migrated as a side effect of a browser mutation.
func (QueryService) VerifyCurrentSchema(ctx context.Context) error {
	if err := validateWorkspaceCollectionQueryContext(ctx); err != nil {
		return err
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return workspaceCollectionQueryError(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRowContext(ctx, "SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		return workspaceCollectionQueryError(err)
	}
	if version != store.SchemaVersion {
		return application.NewError(application.CodeForbidden, "collection changes require the current database schema; this account remains read-only")
	}
	return nil
}

func validateWorkspaceCollectionQueryContext(ctx context.Context) error {
	if ctx == nil {
		return application.NewError(application.CodeInvalidArgument, "context is required")
	}
	return ctx.Err()
}

func workspaceCollectionQueryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrDatabaseMissing) {
		return application.NewError(application.CodeNotFound, "collection record not found")
	}
	return application.WrapError(application.CodeUnavailable, "local data is temporarily unavailable", err)
}

func workspaceCollectionSource(value store.CollectionSource) WorkspaceCollectionSource {
	return WorkspaceCollectionSource{
		ID: value.ID, Connector: value.Connector, Kind: value.Kind, ExternalID: value.ExternalID,
		Name: value.Name, Project: value.Project, ExcludePattern: value.ExcludePattern,
		Instruction: value.Instruction, KnowledgeCollection: value.KnowledgeCollection,
		Strategy: value.Strategy, DecisionUnit: value.DecisionUnit, IntervalMinutes: value.IntervalMinutes,
		Priority: value.Priority, Enabled: value.Enabled, Muted: value.Muted,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func workspaceCollectionRun(value store.CollectionRun) WorkspaceCollectionRun {
	return WorkspaceCollectionRun{
		ID: value.ID, Connector: value.Connector, SourceID: value.SourceID, Status: value.Status,
		StartedAt: value.StartedAt, FinishedAt: value.FinishedAt, FetchedCount: value.FetchedCount,
		AnalyzedCount: value.AnalyzedCount, CreatedCount: value.CreatedCount, AppendedCount: value.AppendedCount,
		InsightCount: value.InsightCount, IgnoredCount: value.IgnoredCount, FailedCount: value.FailedCount, Error: value.Error,
	}
}

func workspaceCollectionSummary(value store.CollectionSummary) WorkspaceCollectionSummary {
	return WorkspaceCollectionSummary{
		Sources: value.Sources, Enabled: value.Enabled, Fetched: value.Fetched, Created: value.Created,
		Appended: value.Appended, Insight: value.Insight, Ignored: value.Ignored, Failed: value.Failed,
		Followups: value.Followups, FollowupsClosed: value.FollowupsClosed, RetryStopped: value.RetryStopped,
		Unread: value.Unread, Settleable: value.Settleable,
	}
}

func workspaceCollectionMessageStats(value store.CollectionMessageStats) WorkspaceCollectionMessageStats {
	return WorkspaceCollectionMessageStats{Total: value.Total, Conversations: value.Conversations, Oldest: value.Oldest, Newest: value.Newest}
}

func workspaceCollectionItem(value store.CollectionItem) WorkspaceCollectionItem {
	return WorkspaceCollectionItem{
		ID: value.ID, SourceID: value.SourceID, Connector: value.Connector, ConversationID: value.ConversationID,
		Fingerprint: value.Fingerprint, MessageIDs: append([]string(nil), value.MessageIDs...), Sender: value.Sender,
		OccurredAt: value.OccurredAt, RawContext: value.RawContext, Action: value.Action, ProposedAction: value.ProposedAction,
		Title: value.Title, Summary: value.Summary, ItemType: value.ItemType, Project: value.Project, Priority: value.Priority,
		Reason: value.Reason, Confidence: value.Confidence, KnowledgeDocumentID: value.KnowledgeDocumentID,
		KnowledgeCollection: value.KnowledgeCollection, TodoID: value.TodoID, Status: value.Status,
		ReadAt: value.ReadAt, ArchivedAt: value.ArchivedAt, Attempts: value.Attempts, RetryStopped: value.RetryStopped,
		Error: value.Error, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		TodoStatus: value.TodoStatus, TodoArchived: value.TodoArchived,
	}
}

func workspaceCollectionMessage(value store.CollectionMessage) WorkspaceCollectionMessage {
	return WorkspaceCollectionMessage{
		Connector: value.Connector, ConversationID: value.ConversationID, MessageID: value.MessageID,
		SourceID: value.SourceID, ConversationName: value.ConversationName, Sender: value.Sender,
		CreatedAt: value.CreatedAt, Content: value.Content, SyncedAt: value.SyncedAt,
	}
}
