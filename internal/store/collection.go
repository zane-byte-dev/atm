package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

const (
	CollectionStrategyTasks   = "tasks"
	CollectionStrategyObserve = "observe"

	// CollectionDecisionUnitWindow groups a source's messages by conversation and
	// a fifteen-minute gap before deciding; CollectionDecisionUnitMessage decides
	// on each message, reading the rest of its window as context.
	CollectionDecisionUnitWindow  = "window"
	CollectionDecisionUnitMessage = "message"
)

var collectionSourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type CollectionSource struct {
	ID             string `json:"id"`
	Connector      string `json:"connector"`
	Kind           string `json:"kind"`
	ExternalID     string `json:"external_id"`
	Name           string `json:"name,omitempty"`
	Project        string `json:"project,omitempty"`
	ExcludePattern string `json:"exclude_pattern,omitempty"`
	// Instruction is what this source should be watched for, in the user's own
	// words. It reaches the classifier as trusted instruction; the chat does not.
	Instruction string `json:"instruction,omitempty"`
	// KnowledgeCollection is where this source's daily digest is filed. Empty
	// means config.CollectionDigestCollection.
	KnowledgeCollection string `json:"knowledge_collection,omitempty"`
	Strategy            string `json:"strategy"`
	// DecisionUnit is how much of a fetched window one decision covers:
	// CollectionDecisionUnitWindow for chat, where consecutive messages are one
	// piece of work, or CollectionDecisionUnitMessage for a notification feed,
	// where each push is its own event. A batch produces exactly one decision,
	// so this is what keeps a feed's events from swallowing each other.
	DecisionUnit    string `json:"decision_unit"`
	IntervalMinutes int    `json:"interval_minutes"`
	Priority        string `json:"priority"`
	AutoDispatch    bool   `json:"auto_dispatch"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

// CollectionDigest records the knowledge document one source's insights for one
// day were distilled into. CoveredThrough is the newest insight the current body
// accounts for: anything past it makes the digest due again.
type CollectionDigest struct {
	SourceID       string `json:"source_id"`
	DigestDate     string `json:"digest_date"`
	DocumentID     string `json:"document_id"`
	Collection     string `json:"collection,omitempty"`
	Title          string `json:"title,omitempty"`
	ItemCount      int    `json:"item_count"`
	CoveredThrough int64  `json:"covered_through"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type CollectionCheckpoint struct {
	SourceID   string `json:"source_id"`
	CursorTime int64  `json:"cursor_time"`
	Cursor     string `json:"cursor,omitempty"`
	UpdatedAt  int64  `json:"updated_at"`
}

// MaxCollectionAttempts is how many times automatic collection will try one
// batch before it stops on its own. Three covers what the retry is actually for
// — a connector or model that was unavailable for a minute — while a batch that
// fails deterministically costs three model calls instead of one per run until
// someone notices. There is no backoff on purpose: the schedule already spaces
// attempts a run apart, and a ceiling this low makes a growing delay pointless.
const MaxCollectionAttempts = 3

// CollectionRetriesExhausted reports whether an item has stopped being retried
// automatically and is now waiting for someone to reprocess it.
func CollectionRetriesExhausted(item CollectionItem) bool {
	return item.Status == "failed" && item.Attempts >= MaxCollectionAttempts
}

type CollectionRun struct {
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

type CollectionItem struct {
	ID             string   `json:"id"`
	SourceID       string   `json:"source_id"`
	Connector      string   `json:"connector"`
	ConversationID string   `json:"conversation_id,omitempty"`
	Fingerprint    string   `json:"fingerprint"`
	MessageIDs     []string `json:"message_ids"`
	Sender         string   `json:"sender,omitempty"`
	OccurredAt     int64    `json:"occurred_at,omitempty"`
	RawContext     string   `json:"raw_context,omitempty"`
	Action         string   `json:"action"`
	// ProposedAction is what an on-demand analysis decided but has not carried
	// out: empty unless a person still has to confirm it. See Service.Analyze.
	ProposedAction string  `json:"proposed_action,omitempty"`
	Title          string  `json:"title,omitempty"`
	Summary        string  `json:"summary,omitempty"`
	ItemType       string  `json:"item_type,omitempty"`
	Project        string  `json:"project,omitempty"`
	Priority       string  `json:"priority,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
	TodoID         string  `json:"todo_id,omitempty"`
	Status         string  `json:"status"`
	// Attempts counts how many times processing this batch has been tried.
	// Automatic retries stop at MaxCollectionAttempts; see
	// CollectionRetriesExhausted.
	Attempts int `json:"attempts,omitempty"`
	// DispatchStatus describes only the optional Todo -> Agent handoff. The
	// collection decision remains processed even if Codex cannot be launched.
	DispatchStatus string `json:"dispatch_status,omitempty"`
	DispatchError  string `json:"dispatch_error,omitempty"`
	// RetryStopped is Attempts read against the ceiling, derived on every query
	// so that what "failed" means for a reader — comes back on its own, or waits
	// for a person — is decided here instead of by every caller keeping its own
	// copy of the limit.
	RetryStopped bool   `json:"retry_stopped,omitempty"`
	Error        string `json:"error,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	// TodoStatus and TodoArchived are the linked Todo's current lifecycle state,
	// read back on every query rather than stored. A Todo can be finished,
	// dropped or archived from the CLI, the App or any agent, and none of those
	// routes should have to remember to write into this ledger — nor could a
	// stored copy explain the records that predate the feedback. Empty and false
	// when the item never touched a Todo.
	TodoStatus   string `json:"todo_status,omitempty"`
	TodoArchived bool   `json:"todo_archived,omitempty"`
}

type CollectionSummary struct {
	Sources  int `json:"sources"`
	Enabled  int `json:"enabled_sources"`
	Fetched  int `json:"fetched_today"`
	Created  int `json:"created_today"`
	Appended int `json:"appended_today"`
	Insight  int `json:"insight_today"`
	Ignored  int `json:"ignored_today"`
	Failed   int `json:"failed_today"`
	// Followups counts every record that wrote to a Todo, and FollowupsClosed how
	// many of those Todos have since been finished or dropped. Unlike the counts
	// above these are not scoped to today — the point is what collection is still
	// on the hook for, which has no reason to have started this morning. Records
	// are the unit, not Todos: two records may legitimately name the same Todo.
	Followups       int `json:"followups"`
	FollowupsClosed int `json:"followups_closed"`
	// RetryStopped counts the items whose automatic retry has run out, over the
	// whole ledger rather than today. Bounding the retry means a broken batch now
	// goes quiet instead of failing loudly every run, so something has to say it
	// is still there — otherwise the fix trades a noisy problem for a silent one.
	RetryStopped int `json:"retry_stopped"`
}

type CollectionOverview struct {
	Summary CollectionSummary  `json:"summary"`
	Sources []CollectionSource `json:"sources"`
	Runs    []CollectionRun    `json:"runs"`
	Items   []CollectionItem   `json:"items"`
	Digests []CollectionDigest `json:"digests"`
}

func CollectionSourceID(connector, kind, externalID string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{connector, kind, externalID}, "\x00")))
	return "cs_" + hex.EncodeToString(hash[:8])
}

func CollectionItemID(connector, fingerprint string) string {
	hash := sha256.Sum256([]byte(connector + "\x00" + fingerprint))
	return "ci_" + hex.EncodeToString(hash[:8])
}

func UpsertCollectionSource(db *sql.DB, source CollectionSource) (CollectionSource, error) {
	source.Connector = strings.ToLower(strings.TrimSpace(source.Connector))
	source.Kind = strings.ToLower(strings.TrimSpace(source.Kind))
	source.ExternalID = strings.TrimSpace(source.ExternalID)
	source.Name = strings.TrimSpace(source.Name)
	source.Project = strings.TrimSpace(source.Project)
	source.ExcludePattern = strings.TrimSpace(source.ExcludePattern)
	source.Instruction = strings.TrimSpace(source.Instruction)
	source.KnowledgeCollection = strings.ToLower(strings.TrimSpace(source.KnowledgeCollection))
	source.Strategy = strings.ToLower(strings.TrimSpace(source.Strategy))
	source.DecisionUnit = strings.ToLower(strings.TrimSpace(source.DecisionUnit))
	if source.Connector == "" || source.ExternalID == "" {
		return CollectionSource{}, fmt.Errorf("connector and external ID are required")
	}
	if !collectionSourceTypePattern.MatchString(source.Connector) {
		return CollectionSource{}, fmt.Errorf("invalid collection connector: %s", source.Connector)
	}
	if !collectionSourceTypePattern.MatchString(source.Kind) {
		return CollectionSource{}, fmt.Errorf("invalid collection source kind: %s", source.Kind)
	}
	if source.Priority == "" {
		source.Priority = "P2"
	}
	if !validCollectionPriority(source.Priority) {
		return CollectionSource{}, fmt.Errorf("invalid priority: %s", source.Priority)
	}
	if source.Strategy == "" {
		source.Strategy = CollectionStrategyTasks
	}
	if source.Strategy != CollectionStrategyTasks && source.Strategy != CollectionStrategyObserve {
		return CollectionSource{}, fmt.Errorf("invalid collection strategy: %s", source.Strategy)
	}
	if source.Strategy == CollectionStrategyObserve {
		source.AutoDispatch = false
	}
	if source.AutoDispatch && source.Project == "" {
		return CollectionSource{}, fmt.Errorf("automatic dispatch requires a source project")
	}
	if source.DecisionUnit == "" {
		source.DecisionUnit = CollectionDecisionUnitWindow
	}
	if source.DecisionUnit != CollectionDecisionUnitWindow && source.DecisionUnit != CollectionDecisionUnitMessage {
		return CollectionSource{}, fmt.Errorf("invalid collection decision unit: %s", source.DecisionUnit)
	}
	if source.IntervalMinutes == 0 {
		if source.Strategy == CollectionStrategyObserve {
			source.IntervalMinutes = 60
		} else {
			source.IntervalMinutes = 5
		}
	}
	if source.IntervalMinutes < 1 || source.IntervalMinutes > 1440 {
		return CollectionSource{}, fmt.Errorf("collection interval must be between 1 and 1440 minutes")
	}
	now := time.Now().In(config.Loc).Unix()
	if source.ID == "" {
		source.ID = CollectionSourceID(source.Connector, source.Kind, source.ExternalID)
	}
	if source.CreatedAt == 0 {
		source.CreatedAt = now
	}
	source.UpdatedAt = now
	_, err := db.Exec(`INSERT INTO collection_sources
		(id,connector,kind,external_id,name,project,exclude_pattern,instruction,knowledge_collection,
		 strategy,decision_unit,interval_minutes,priority,auto_dispatch,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(connector,kind,external_id) DO UPDATE SET
			name=excluded.name,project=excluded.project,exclude_pattern=excluded.exclude_pattern,
			instruction=excluded.instruction,knowledge_collection=excluded.knowledge_collection,
			strategy=excluded.strategy,decision_unit=excluded.decision_unit,
			interval_minutes=excluded.interval_minutes,priority=excluded.priority,
			auto_dispatch=excluded.auto_dispatch,
			enabled=excluded.enabled,updated_at=excluded.updated_at`,
		source.ID, source.Connector, source.Kind, source.ExternalID, source.Name,
		source.Project, source.ExcludePattern, source.Instruction, source.KnowledgeCollection,
		source.Strategy, source.DecisionUnit, source.IntervalMinutes, source.Priority,
		boolInt(source.AutoDispatch),
		boolInt(source.Enabled), source.CreatedAt, source.UpdatedAt)
	if err != nil {
		return CollectionSource{}, err
	}
	return FindCollectionSource(db, source.Connector, source.Kind, source.ExternalID)
}

func FindCollectionSource(db *sql.DB, connector, kind, externalID string) (CollectionSource, error) {
	return scanCollectionSource(db.QueryRow(collectionSourceSelect+
		` WHERE connector=? AND kind=? AND external_id=?`, connector, kind, externalID))
}

func GetCollectionSource(db *sql.DB, id string) (CollectionSource, error) {
	return scanCollectionSource(db.QueryRow(collectionSourceSelect+` WHERE id=?`, id))
}

func ListCollectionSources(db *sql.DB, connector string, enabledOnly bool) ([]CollectionSource, error) {
	query := collectionSourceSelect + ` WHERE (?='' OR connector=?)`
	if enabledOnly {
		query += ` AND enabled=1`
	}
	query += ` ORDER BY connector,name,external_id`
	rows, err := db.Query(query, connector, connector)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []CollectionSource{}
	for rows.Next() {
		source, err := scanCollectionSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func SetCollectionSourceEnabled(db *sql.DB, id string, enabled bool) error {
	result, err := db.Exec(`UPDATE collection_sources SET enabled=?,updated_at=? WHERE id=?`,
		boolInt(enabled), time.Now().In(config.Loc).Unix(), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return fmt.Errorf("collection source not found: %s", id)
	}
	return err
}

func DeleteCollectionSource(db *sql.DB, id string) error {
	result, err := db.Exec(`DELETE FROM collection_sources WHERE id=?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return fmt.Errorf("collection source not found: %s", id)
	}
	return err
}

// DeleteCollectionItem drops one processing record and returns what went, so a
// caller can report it without reading the row again.
//
// The Todo the record wrote to is left alone. The record is collection's own
// note about a decision, not the work itself: someone may have been acting on
// that Todo for days, and tidying away the note that explains where it came from
// must not take the work with it. Revert is what says the write was wrong.
//
// Deleting also releases the record's messages from the handled set, so a record
// whose messages still fall inside the next run's re-read window can be rebuilt
// by that run. Anything older than the window is gone for good.
func DeleteCollectionItem(db *sql.DB, id string) (CollectionItem, error) {
	items, err := DeleteCollectionItems(db, []string{id})
	if err != nil {
		return CollectionItem{}, err
	}
	return items[0], nil
}

// DeleteCollectionItems drops several records in one transaction and returns
// them in the order they were named, which is what clearing a whole group needs:
// either the group empties or nothing moves. A half-cleared group would leave
// someone re-reading the list to work out which rows they still have to chase,
// and an unknown id is a stale snapshot rather than a reason to delete part of
// the batch.
//
// Same contract as one record at a time (see DeleteCollectionItem): the Todos
// stay, and the released messages let the next run rebuild anything still inside
// its re-read window.
//
// A repeated id is deleted once, and appears once in the result. Naming a record
// twice asks for the same end state as naming it once; without this the second
// pass would find the row it just deleted missing and fail the whole batch as
// "not found". `atm collect item delete` already uniques its arguments, so this
// is what keeps that from being the only thing standing between a duplicated id
// and a group that refuses to clear for no visible reason.
func DeleteCollectionItems(db *sql.DB, ids []string) ([]CollectionItem, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no collection item given")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	deleted := make([]CollectionItem, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		item, err := scanCollectionItem(tx.QueryRow(collectionItemSelect+` WHERE i.id=?`, id))
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("collection item not found: %s", id)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM collection_items WHERE id=?`, id); err != nil {
			return nil, err
		}
		deleted = append(deleted, item)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return deleted, nil
}

func GetCollectionCheckpoint(db *sql.DB, sourceID string) (CollectionCheckpoint, error) {
	checkpoint := CollectionCheckpoint{SourceID: sourceID}
	err := db.QueryRow(`SELECT cursor_time,cursor,updated_at FROM collection_checkpoints WHERE source_id=?`, sourceID).
		Scan(&checkpoint.CursorTime, &checkpoint.Cursor, &checkpoint.UpdatedAt)
	if err == sql.ErrNoRows {
		return checkpoint, nil
	}
	return checkpoint, err
}

func SaveCollectionCheckpoint(db *sql.DB, checkpoint CollectionCheckpoint) error {
	checkpoint.UpdatedAt = time.Now().In(config.Loc).Unix()
	_, err := db.Exec(`INSERT INTO collection_checkpoints(source_id,cursor_time,cursor,updated_at)
		VALUES(?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET
		cursor_time=excluded.cursor_time,cursor=excluded.cursor,updated_at=excluded.updated_at`,
		checkpoint.SourceID, checkpoint.CursorTime, checkpoint.Cursor, checkpoint.UpdatedAt)
	return err
}

func SaveCollectionRun(db *sql.DB, run CollectionRun) error {
	_, err := db.Exec(`INSERT INTO collection_runs
		(id,connector,source_id,status,started_at,finished_at,fetched_count,analyzed_count,
		 created_count,appended_count,insight_count,ignored_count,failed_count,error)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,finished_at=excluded.finished_at,
		fetched_count=excluded.fetched_count,analyzed_count=excluded.analyzed_count,
		created_count=excluded.created_count,appended_count=excluded.appended_count,
		insight_count=excluded.insight_count,
		ignored_count=excluded.ignored_count,failed_count=excluded.failed_count,error=excluded.error`,
		run.ID, run.Connector, run.SourceID, run.Status, run.StartedAt, run.FinishedAt,
		run.FetchedCount, run.AnalyzedCount, run.CreatedCount, run.AppendedCount,
		run.InsightCount, run.IgnoredCount, run.FailedCount, run.Error)
	return err
}

func PutCollectionItem(db *sql.DB, item CollectionItem) (CollectionItem, bool, error) {
	now := time.Now().In(config.Loc).Unix()
	if item.ID == "" {
		item.ID = CollectionItemID(item.Connector, item.Fingerprint)
	}
	if item.Action == "" {
		item.Action = "pending"
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	messageIDs, err := json.Marshal(item.MessageIDs)
	if err != nil {
		return CollectionItem{}, false, err
	}
	result, err := db.Exec(`INSERT INTO collection_items
		(id,source_id,connector,conversation_id,fingerprint,message_ids,sender,occurred_at,
		 raw_context,action,proposed_action,title,summary,item_type,project,priority,reason,confidence,todo_id,
		 status,attempts,dispatch_status,dispatch_error,error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(connector,fingerprint) DO NOTHING`,
		item.ID, item.SourceID, item.Connector, item.ConversationID, item.Fingerprint,
		string(messageIDs), item.Sender, item.OccurredAt, item.RawContext, item.Action,
		item.ProposedAction, item.Title, item.Summary, item.ItemType, item.Project, item.Priority, item.Reason,
		item.Confidence, nullableString(item.TodoID), item.Status, item.Attempts,
		item.DispatchStatus, item.DispatchError, item.Error, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return CollectionItem{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return CollectionItem{}, false, err
	}
	stored, err := GetCollectionItemByFingerprint(db, item.Connector, item.Fingerprint)
	return stored, count > 0, err
}

func UpdateCollectionItem(db *sql.DB, item CollectionItem) error {
	messageIDs, err := json.Marshal(item.MessageIDs)
	if err != nil {
		return err
	}
	item.UpdatedAt = time.Now().In(config.Loc).Unix()
	_, err = db.Exec(`UPDATE collection_items SET conversation_id=?,message_ids=?,sender=?,occurred_at=?,
		raw_context=?,action=?,proposed_action=?,title=?,summary=?,item_type=?,project=?,priority=?,reason=?,confidence=?,
		todo_id=?,status=?,attempts=?,dispatch_status=?,dispatch_error=?,error=?,updated_at=? WHERE id=?`, item.ConversationID, string(messageIDs),
		item.Sender, item.OccurredAt, item.RawContext, item.Action, item.ProposedAction, item.Title, item.Summary,
		item.ItemType, item.Project, item.Priority, item.Reason, item.Confidence,
		nullableString(item.TodoID), item.Status, item.Attempts, item.DispatchStatus,
		item.DispatchError, item.Error, item.UpdatedAt, item.ID)
	return err
}

func GetCollectionItemByFingerprint(db *sql.DB, connector, fingerprint string) (CollectionItem, error) {
	row := db.QueryRow(collectionItemSelect+` WHERE i.connector=? AND i.fingerprint=?`, connector, fingerprint)
	return scanCollectionItem(row)
}

func GetCollectionItem(db *sql.DB, id string) (CollectionItem, error) {
	return scanCollectionItem(db.QueryRow(collectionItemSelect+` WHERE i.id=?`, id))
}

// HandledCollectionMessageIDs returns the exact source messages that already
// reached a final decision, or are waiting for a person to confirm a proposed
// decision. Collection fetches deliberately overlap their checkpoint window;
// filtering by these message IDs before regrouping is what keeps an expanding
// conversation from becoming a brand-new batch on every run.
//
// A batch that used up its retry budget counts as handled too. Not because it
// succeeded, but because leaving it out means every later run rebuilds the same
// batch and refuses it again, which is what kept the checkpoint pinned and the
// re-read window growing. Its messages are archived and the item is still there
// to reprocess.
func HandledCollectionMessageIDs(db *sql.DB, sourceID string) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT message_ids FROM collection_items
		WHERE source_id=? AND (status='processed' OR proposed_action<>''
			OR (status='failed' AND attempts>=?))`, sourceID, MaxCollectionAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	handled := map[string]struct{}{}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		var messageIDs []string
		if err := json.Unmarshal([]byte(encoded), &messageIDs); err != nil {
			return nil, err
		}
		for _, messageID := range messageIDs {
			if messageID != "" {
				handled[messageID] = struct{}{}
			}
		}
	}
	return handled, rows.Err()
}

// RetireSupersededCollectionItems stops the automatic retry of failed items whose
// messages have all been taken over by a newer batch, and reports how many it
// retired.
//
// A failed batch is retried by being rebuilt from the same messages, so its
// identity is the message set. One more message in the same conversation makes a
// different set, hence a different fingerprint and a different item: the newer
// item carries the old messages forward while the old one is left behind, never
// rebuilt again and still promising a retry that cannot happen. Retiring it says
// so, and keeps a manual reprocess available for the narrower batch.
func RetireSupersededCollectionItems(db *sql.DB, sourceID, keepID string, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	covered := make(map[string]struct{}, len(messageIDs))
	for _, messageID := range messageIDs {
		covered[messageID] = struct{}{}
	}
	rows, err := db.Query(`SELECT id,message_ids FROM collection_items
		WHERE source_id=? AND id<>? AND status='failed' AND attempts<?`,
		sourceID, keepID, MaxCollectionAttempts)
	if err != nil {
		return 0, err
	}
	superseded := []string{}
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			rows.Close()
			return 0, err
		}
		var owned []string
		if err := json.Unmarshal([]byte(encoded), &owned); err != nil {
			rows.Close()
			return 0, err
		}
		if len(owned) == 0 {
			continue
		}
		absorbed := true
		for _, messageID := range owned {
			if _, ok := covered[messageID]; !ok {
				absorbed = false
				break
			}
		}
		if absorbed {
			superseded = append(superseded, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	now := time.Now().In(config.Loc).Unix()
	for _, id := range superseded {
		if _, err := db.Exec(`UPDATE collection_items SET attempts=?,updated_at=? WHERE id=?`,
			MaxCollectionAttempts, now, id); err != nil {
			return 0, err
		}
	}
	return len(superseded), nil
}

func ListCollectionItems(db *sql.DB, sourceID string, limit int) ([]CollectionItem, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := db.Query(collectionItemSelect+` WHERE (?='' OR i.source_id=?)
		ORDER BY i.updated_at DESC,i.id DESC LIMIT ?`, sourceID, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CollectionItem{}
	for rows.Next() {
		item, err := scanCollectionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func ListCollectionRuns(db *sql.DB, limit int) ([]CollectionRun, error) {
	if limit < 1 || limit > 200 {
		limit = 20
	}
	rows, err := db.Query(`SELECT id,connector,source_id,status,started_at,finished_at,fetched_count,
		analyzed_count,created_count,appended_count,insight_count,ignored_count,failed_count,error
		FROM collection_runs ORDER BY started_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []CollectionRun{}
	for rows.Next() {
		var run CollectionRun
		if err := rows.Scan(&run.ID, &run.Connector, &run.SourceID, &run.Status, &run.StartedAt,
			&run.FinishedAt, &run.FetchedCount, &run.AnalyzedCount, &run.CreatedCount,
			&run.AppendedCount, &run.InsightCount, &run.IgnoredCount, &run.FailedCount,
			&run.Error); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// CollectionDayBounds returns the half-open Unix range [start, end) of the local
// day containing the given time. Digests are per local day, so every query that
// scopes insights to "that day" has to agree on where the day starts.
func CollectionDayBounds(when time.Time) (int64, int64) {
	local := when.In(config.Loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, config.Loc)
	return start.Unix(), start.AddDate(0, 0, 1).Unix()
}

// ListCollectionInsights returns one source's insight decisions inside a day,
// oldest first — the order a digest should read them in. Items whose occurred_at
// is unset fall back to when the decision was recorded, so a batch missing
// message timestamps still lands in a day rather than in 1970.
func ListCollectionInsights(db *sql.DB, sourceID string, dayStart, dayEnd int64) ([]CollectionItem, error) {
	rows, err := db.Query(collectionItemSelect+` WHERE i.source_id=? AND i.action='insight'
		AND (CASE WHEN i.occurred_at>0 THEN i.occurred_at ELSE i.created_at END) >= ?
		AND (CASE WHEN i.occurred_at>0 THEN i.occurred_at ELSE i.created_at END) < ?
		ORDER BY (CASE WHEN i.occurred_at>0 THEN i.occurred_at ELSE i.created_at END) ASC,i.id ASC`,
		sourceID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CollectionItem{}
	for rows.Next() {
		item, err := scanCollectionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CollectionInsightWatermark is the effective timestamp ListCollectionInsights
// orders by, so callers can compare an item against a digest's CoveredThrough
// without re-deriving the fallback.
func CollectionInsightWatermark(item CollectionItem) int64 {
	if item.OccurredAt > 0 {
		return item.OccurredAt
	}
	return item.CreatedAt
}

// CollectionItemTodoClosed reports whether the Todo this record filed has since
// been finished or dropped. That settles the record too: the source's request
// was answered, wherever the answering happened. Archiving is only ever allowed
// after a close, so the status column is the whole answer — TodoArchived only
// explains why the Todo is no longer in the working set.
func CollectionItemTodoClosed(item CollectionItem) bool {
	return item.TodoStatus == TodoStatusDone || item.TodoStatus == TodoStatusDropped
}

func GetCollectionDigest(db *sql.DB, sourceID, digestDate string) (CollectionDigest, error) {
	digest := CollectionDigest{SourceID: sourceID, DigestDate: digestDate}
	err := db.QueryRow(`SELECT document_id,collection,title,item_count,covered_through,created_at,updated_at
		FROM collection_digests WHERE source_id=? AND digest_date=?`, sourceID, digestDate).
		Scan(&digest.DocumentID, &digest.Collection, &digest.Title, &digest.ItemCount,
			&digest.CoveredThrough, &digest.CreatedAt, &digest.UpdatedAt)
	if err == sql.ErrNoRows {
		return digest, nil
	}
	return digest, err
}

func SaveCollectionDigest(db *sql.DB, digest CollectionDigest) error {
	now := time.Now().In(config.Loc).Unix()
	if digest.CreatedAt == 0 {
		digest.CreatedAt = now
	}
	digest.UpdatedAt = now
	_, err := db.Exec(`INSERT INTO collection_digests
		(source_id,digest_date,document_id,collection,title,item_count,covered_through,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(source_id,digest_date) DO UPDATE SET
			document_id=excluded.document_id,collection=excluded.collection,title=excluded.title,
			item_count=excluded.item_count,covered_through=excluded.covered_through,
			updated_at=excluded.updated_at`,
		digest.SourceID, digest.DigestDate, digest.DocumentID, digest.Collection, digest.Title,
		digest.ItemCount, digest.CoveredThrough, digest.CreatedAt, digest.UpdatedAt)
	return err
}

func ListCollectionDigests(db *sql.DB, limit int) ([]CollectionDigest, error) {
	if limit < 1 || limit > 200 {
		limit = 20
	}
	rows, err := db.Query(`SELECT source_id,digest_date,document_id,collection,title,item_count,
		covered_through,created_at,updated_at FROM collection_digests
		ORDER BY digest_date DESC,updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	digests := []CollectionDigest{}
	for rows.Next() {
		var digest CollectionDigest
		if err := rows.Scan(&digest.SourceID, &digest.DigestDate, &digest.DocumentID,
			&digest.Collection, &digest.Title, &digest.ItemCount, &digest.CoveredThrough,
			&digest.CreatedAt, &digest.UpdatedAt); err != nil {
			return nil, err
		}
		digests = append(digests, digest)
	}
	return digests, rows.Err()
}

// CollectionSourceDue reports whether a background run should visit a source.
// Manual runs bypass this check. Failed runs do not postpone retries: only the
// latest successful run satisfies the source cadence.
func CollectionSourceDue(db *sql.DB, source CollectionSource, now time.Time) (bool, error) {
	var latest sql.NullInt64
	err := db.QueryRow(`SELECT MAX(started_at) FROM collection_runs
		WHERE source_id=? AND status='succeeded'`, source.ID).Scan(&latest)
	if err != nil {
		return false, err
	}
	if !latest.Valid {
		return true, nil
	}
	interval := source.IntervalMinutes
	if interval < 1 {
		interval = 5
	}
	return now.Unix()-latest.Int64 >= int64((time.Duration(interval) * time.Minute).Seconds()), nil
}

func LoadCollectionOverview(db *sql.DB, itemLimit int) (CollectionOverview, error) {
	sources, err := ListCollectionSources(db, "", false)
	if err != nil {
		return CollectionOverview{}, err
	}
	items, err := ListCollectionItems(db, "", itemLimit)
	if err != nil {
		return CollectionOverview{}, err
	}
	runs, err := ListCollectionRuns(db, 20)
	if err != nil {
		return CollectionOverview{}, err
	}
	digests, err := ListCollectionDigests(db, 20)
	if err != nil {
		return CollectionOverview{}, err
	}
	summary := CollectionSummary{Sources: len(sources)}
	for _, source := range sources {
		if source.Enabled {
			summary.Enabled++
		}
	}
	start, _ := CollectionDayBounds(time.Now().In(config.Loc))
	err = db.QueryRow(`SELECT
		COALESCE(SUM(fetched_count),0),COALESCE(SUM(created_count),0),
		COALESCE(SUM(appended_count),0),COALESCE(SUM(insight_count),0),
		COALESCE(SUM(ignored_count),0),
		COALESCE(SUM(failed_count),0) FROM collection_runs WHERE started_at>=?`, start).
		Scan(&summary.Fetched, &summary.Created, &summary.Appended, &summary.Insight,
			&summary.Ignored, &summary.Failed)
	if err != nil {
		return CollectionOverview{}, err
	}
	// Counted over the whole ledger rather than the returned page: itemLimit is a
	// display budget, and "how many filed Todos are still open" must not shrink
	// because the caller asked for fewer rows.
	err = db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN t.status IN ('done','dropped') THEN 1 ELSE 0 END),0)
		FROM collection_items i JOIN todos t ON t.id=i.todo_id
		WHERE i.action IN ('create','append')`).
		Scan(&summary.Followups, &summary.FollowupsClosed)
	if err != nil {
		return CollectionOverview{}, err
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM collection_items
		WHERE status='failed' AND attempts>=?`, MaxCollectionAttempts).Scan(&summary.RetryStopped)
	return CollectionOverview{Summary: summary, Sources: sources, Runs: runs,
		Items: items, Digests: digests}, err
}

// collectionItemSelect reads a record together with the current state of the
// Todo it wrote to. Every column is table-qualified because id, status,
// created_at and updated_at exist on both sides of the join.
const collectionItemSelect = `SELECT i.id,i.source_id,i.connector,i.conversation_id,i.fingerprint,
	i.message_ids,i.sender,i.occurred_at,i.raw_context,i.action,i.proposed_action,i.title,i.summary,
	i.item_type,i.project,i.priority,i.reason,i.confidence,COALESCE(i.todo_id,''),i.status,i.attempts,
	i.dispatch_status,i.dispatch_error,i.error,
	i.created_at,i.updated_at,COALESCE(t.status,''),t.archived_at IS NOT NULL
	FROM collection_items i LEFT JOIN todos t ON t.id=i.todo_id`

const collectionSourceSelect = `SELECT id,connector,kind,external_id,name,project,exclude_pattern,
	instruction,knowledge_collection,strategy,decision_unit,interval_minutes,priority,auto_dispatch,enabled,
	created_at,updated_at
	FROM collection_sources`

type collectionScanner interface {
	Scan(dest ...any) error
}

func scanCollectionSource(scanner collectionScanner) (CollectionSource, error) {
	var source CollectionSource
	var autoDispatch, enabled int
	err := scanner.Scan(&source.ID, &source.Connector, &source.Kind, &source.ExternalID,
		&source.Name, &source.Project, &source.ExcludePattern, &source.Instruction,
		&source.KnowledgeCollection, &source.Strategy, &source.DecisionUnit,
		&source.IntervalMinutes, &source.Priority, &autoDispatch, &enabled, &source.CreatedAt, &source.UpdatedAt)
	source.AutoDispatch = autoDispatch != 0
	source.Enabled = enabled != 0
	return source, err
}

func scanCollectionItem(scanner collectionScanner) (CollectionItem, error) {
	var item CollectionItem
	var messageIDs string
	var todoArchived int
	err := scanner.Scan(&item.ID, &item.SourceID, &item.Connector, &item.ConversationID,
		&item.Fingerprint, &messageIDs, &item.Sender, &item.OccurredAt, &item.RawContext,
		&item.Action, &item.ProposedAction, &item.Title, &item.Summary, &item.ItemType, &item.Project,
		&item.Priority, &item.Reason, &item.Confidence, &item.TodoID, &item.Status, &item.Attempts,
		&item.DispatchStatus, &item.DispatchError, &item.Error, &item.CreatedAt, &item.UpdatedAt,
		&item.TodoStatus, &todoArchived)
	item.TodoArchived = todoArchived != 0
	item.RetryStopped = CollectionRetriesExhausted(item)
	if err == nil {
		err = json.Unmarshal([]byte(messageIDs), &item.MessageIDs)
	}
	return item, err
}

func validCollectionPriority(priority string) bool {
	return priority == "P0" || priority == "P1" || priority == "P2" || priority == "P3"
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
