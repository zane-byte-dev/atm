package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// CollectionMessage is one synced connector message, kept verbatim so a
// conversation can be read and searched without calling its connector.
type CollectionMessage struct {
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

// CollectionMessageQuery filters the archive. A zero field is not a filter.
type CollectionMessageQuery struct {
	Connector      string
	ConversationID string
	Sender         string
	SinceTS        int64
	Limit          int
}

type CollectionMessageStats struct {
	Total         int   `json:"total"`
	Conversations int   `json:"conversations"`
	Oldest        int64 `json:"oldest,omitempty"`
	Newest        int64 `json:"newest,omitempty"`
}

const collectionMessageDefaultLimit = 50

// PutCollectionMessages stores messages and reports how many were new. A chat
// message never changes, so a message already held is left exactly as it was
// synced: re-reading a conversation cannot rewrite its history.
func PutCollectionMessages(db *sql.DB, messages []CollectionMessage) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}
	now := time.Now().In(config.Loc).Unix()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	statement, err := tx.Prepare(`INSERT INTO collection_messages
		(connector,conversation_id,message_id,source_id,conversation_name,sender,created_at,content,synced_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(connector,conversation_id,message_id) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer statement.Close()
	inserted := 0
	for _, message := range messages {
		message.Connector = strings.TrimSpace(message.Connector)
		message.ConversationID = strings.TrimSpace(message.ConversationID)
		message.MessageID = strings.TrimSpace(message.MessageID)
		if message.Connector == "" || message.ConversationID == "" || message.MessageID == "" {
			continue
		}
		if message.SyncedAt == 0 {
			message.SyncedAt = now
		}
		result, err := statement.Exec(message.Connector, message.ConversationID, message.MessageID,
			message.SourceID, message.ConversationName, message.Sender, message.CreatedAt,
			message.Content, message.SyncedAt)
		if err != nil {
			return 0, err
		}
		if count, err := result.RowsAffected(); err == nil {
			inserted += int(count)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// ListCollectionMessages reads one conversation oldest first, the order a person
// reads a chat in. A limit keeps the newest end of the window, since that is
// what "the recent messages" means.
func ListCollectionMessages(db *sql.DB, query CollectionMessageQuery) ([]CollectionMessage, error) {
	sqlText := collectionMessageSelect + ` WHERE (?='' OR connector=?)
		AND (?='' OR conversation_id=?) AND created_at>=?
		ORDER BY created_at DESC, message_id DESC LIMIT ?`
	rows, err := db.Query(sqlText, query.Connector, query.Connector,
		query.ConversationID, query.ConversationID,
		query.SinceTS, collectionMessageLimit(query.Limit))
	if err != nil {
		return nil, err
	}
	messages, err := scanCollectionMessages(rows)
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}

// SearchCollectionMessages matches content the same way session search does —
// a case-insensitive substring — so a keyword behaves identically across ATM.
func SearchCollectionMessages(db *sql.DB, keyword string, query CollectionMessageQuery) ([]CollectionMessage, error) {
	sqlText := collectionMessageSelect + ` WHERE instr(lower(content), lower(?)) > 0
		AND (?='' OR connector=?) AND (?='' OR conversation_id=?)
		AND (?='' OR instr(lower(sender), lower(?)) > 0)
		AND created_at>=? ORDER BY created_at DESC, message_id DESC LIMIT ?`
	rows, err := db.Query(sqlText, keyword, query.Connector, query.Connector,
		query.ConversationID, query.ConversationID,
		query.Sender, query.Sender, query.SinceTS, collectionMessageLimit(query.Limit))
	if err != nil {
		return nil, err
	}
	return scanCollectionMessages(rows)
}

// PruneCollectionMessages drops everything older than cutoff and reports how
// many rows went. A cutoff of zero keeps everything.
func PruneCollectionMessages(db *sql.DB, cutoff int64) (int, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	result, err := db.Exec(`DELETE FROM collection_messages WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

// RetentionCutoff turns a retention window in days into the timestamp before
// which messages are dropped. Zero days means keep everything, so it has no
// cutoff.
func RetentionCutoff(days int, now time.Time) int64 {
	if days < 1 {
		return 0
	}
	return now.In(config.Loc).AddDate(0, 0, -days).Unix()
}

func CollectionMessageStatsFor(db *sql.DB) (CollectionMessageStats, error) {
	var stats CollectionMessageStats
	var oldest, newest sql.NullInt64
	err := db.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT connector || char(0) || conversation_id),MIN(created_at),MAX(created_at)
		FROM collection_messages`).Scan(&stats.Total, &stats.Conversations, &oldest, &newest)
	stats.Oldest, stats.Newest = oldest.Int64, newest.Int64
	return stats, err
}

const collectionMessageSelect = `SELECT connector,conversation_id,message_id,source_id,
	conversation_name,sender,created_at,content,synced_at FROM collection_messages`

func collectionMessageLimit(limit int) int {
	if limit < 1 || limit > 2000 {
		return collectionMessageDefaultLimit
	}
	return limit
}

func scanCollectionMessages(rows *sql.Rows) ([]CollectionMessage, error) {
	defer rows.Close()
	messages := []CollectionMessage{}
	for rows.Next() {
		var message CollectionMessage
		if err := rows.Scan(&message.Connector, &message.ConversationID, &message.MessageID,
			&message.SourceID, &message.ConversationName, &message.Sender, &message.CreatedAt,
			&message.Content, &message.SyncedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
