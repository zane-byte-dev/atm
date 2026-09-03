package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// TodoCreateRecord is the immutable response to a successful keyed Todo create.
// The application owns payload hashing and response encoding. Keeping the
// original response separately lets retries replay it after edits or deletion.
type TodoCreateRecord struct {
	Key         string
	PayloadHash string
	TodoID      string
	ResultJSON  string
	CreatedAt   int64
}

// FindTodoCreate reads a keyed creation under the same serialized transaction
// as its Todo write. An unseen key returns (nil, nil).
func (state *WorkStateTx) FindTodoCreate(key string) (*TodoCreateRecord, error) {
	var record TodoCreateRecord
	err := state.tx.QueryRow(`SELECT idempotency_key,payload_hash,todo_id,result_json,created_at
		FROM work_create_idempotency WHERE idempotency_key=?`, key).Scan(
		&record.Key, &record.PayloadHash, &record.TodoID, &record.ResultJSON, &record.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// RecordTodoCreate commits a creation response atomically with the Todo. The
// key is insert-only: retries must inspect the original record rather than
// replacing it, and failed Todo persistence rolls the record back as well.
func (state *WorkStateTx) RecordTodoCreate(record TodoCreateRecord) error {
	if record.Key == "" || record.PayloadHash == "" || record.TodoID == "" ||
		record.ResultJSON == "" || record.CreatedAt <= 0 {
		return fmt.Errorf("todo create key, payload hash, todo ID, result and creation time are required")
	}
	_, err := state.tx.Exec(`INSERT INTO work_create_idempotency
		(idempotency_key,payload_hash,todo_id,result_json,created_at)
		VALUES(?,?,?,?,?)`, record.Key, record.PayloadHash, record.TodoID, record.ResultJSON, record.CreatedAt)
	return err
}

// migrateV54ToV55 adds the durable create-response ledger without backfilling:
// older creates had no client key from which a retry identity could be inferred.
func migrateV54ToV55(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS work_create_idempotency (
			idempotency_key TEXT PRIMARY KEY,
			payload_hash    TEXT NOT NULL,
			todo_id         TEXT NOT NULL,
			result_json     TEXT NOT NULL,
			created_at      INTEGER NOT NULL
		)`,
		`UPDATE schema_version SET version = 55`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
