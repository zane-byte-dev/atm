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
