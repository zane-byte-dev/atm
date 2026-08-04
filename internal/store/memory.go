package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MemoryEvent is one entry in the shared-memory log. Op is remember, supersede,
// or forget; the latter two carry the TargetID of the memory they act on.
// CreatedAt is RFC3339 with nanoseconds, matching the log this replaced.
type MemoryEvent struct {
	ID        string
	Op        string
	Scope     string
	Content   string
	TargetID  string
	Tags      []string
	Metadata  map[string]string
	CreatedAt string
}

// The op values a memory row can carry. EffectiveMemories switches on these, so
// the writer in internal/knowledge has to spell them the same way; it used to
// hardcode the literals and only the tests referenced these names.
const (
	MemoryOpRemember  = "remember"
	MemoryOpSupersede = "supersede"
	MemoryOpForget    = "forget"
)

// AppendMemoryEvent records one event. The write lock is the same one work state
// uses, so a memory write cannot interleave with a concurrent one.
func AppendMemoryEvent(event MemoryEvent) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := acquireWorkWriteLock(tx); err != nil {
		return err
	}

	var targetID any
	if event.TargetID != "" {
		targetID = event.TargetID
	}
	if _, err := tx.Exec(`INSERT INTO memory_events(id,op,scope,content,target_id,created_at)
		VALUES(?,?,?,?,?,?)`,
		event.ID, event.Op, event.Scope, event.Content, targetID, event.CreatedAt); err != nil {
		return err
	}
	for position, tag := range event.Tags {
		if _, err := tx.Exec(`INSERT INTO memory_event_tags(event_id,position,tag) VALUES(?,?,?)`,
			event.ID, position, tag); err != nil {
			return err
		}
	}
	for key, value := range event.Metadata {
		if _, err := tx.Exec(`INSERT INTO memory_event_metadata(event_id,key,value) VALUES(?,?,?)`,
			event.ID, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EffectiveMemories returns the memories that are currently in force: every
// remember or supersede event that no later event targets. Replaying the log to
// work this out is unnecessary — an event's ID is never reused, so being
// targeted is the only way to leave the effective set, regardless of order.
func EffectiveMemories() ([]MemoryEvent, error) {
	db, err := OpenReadOnly()
	// A database that does not exist yet holds no records. Unlike work state,
	// where a missing database means the user should run sync, the empty answer is
	// the right one here.
	if errors.Is(err, ErrDatabaseMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return effectiveMemories(db)
}

func effectiveMemories(q sqlQueryer) ([]MemoryEvent, error) {
	rows, err := q.Query(`SELECT id,op,scope,content,COALESCE(target_id,''),created_at
		FROM memory_events
		WHERE op IN ('remember','supersede')
		  AND id NOT IN (SELECT target_id FROM memory_events WHERE target_id IS NOT NULL)
		ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []MemoryEvent{}
	for rows.Next() {
		var event MemoryEvent
		if err := rows.Scan(&event.ID, &event.Op, &event.Scope, &event.Content,
			&event.TargetID, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tags, err := loadGroupedStrings(q, `SELECT event_id,tag FROM memory_event_tags ORDER BY event_id,position,tag`)
	if err != nil {
		return nil, err
	}
	metadata, err := loadMemoryMetadata(q)
	if err != nil {
		return nil, err
	}
	for index := range events {
		events[index].Tags = tags[events[index].ID]
		events[index].Metadata = metadata[events[index].ID]
	}
	return events, nil
}

func loadMemoryMetadata(q sqlQueryer) (map[string]map[string]string, error) {
	rows, err := q.Query(`SELECT event_id,key,value FROM memory_event_metadata`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string]map[string]string{}
	for rows.Next() {
		var eventID, key, value string
		if err := rows.Scan(&eventID, &key, &value); err != nil {
			return nil, err
		}
		if grouped[eventID] == nil {
			grouped[eventID] = map[string]string{}
		}
		grouped[eventID][key] = value
	}
	return grouped, rows.Err()
}

// EffectiveMemory looks up a single memory that is currently in force.
func EffectiveMemory(id string) (*MemoryEvent, error) {
	memories, err := EffectiveMemories()
	if err != nil {
		return nil, err
	}
	for index := range memories {
		if memories[index].ID == id {
			return &memories[index], nil
		}
	}
	return nil, fmt.Errorf("active memory not found: %s", id)
}

// CountMemoryEvents reports the size of the log, including forgotten memories.
func CountMemoryEvents() (int, error) {
	db, err := OpenReadOnly()
	if errors.Is(err, ErrDatabaseMissing) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM memory_events`).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}

// IsMemoryTargetTaken reports whether an error is the unique index on
// memory_events.target_id refusing a second event against the same memory. The
// caller knows what that means in domain terms; the database only knows it is a
// constraint.
func IsMemoryTargetTaken(err error) bool {
	return err != nil && strings.Contains(err.Error(), "memory_events.target_id")
}
