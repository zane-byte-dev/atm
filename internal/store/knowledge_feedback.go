package store

import "errors"

// KnowledgeFeedbackEvent records that a knowledge document was retrieved for a
// session, or how it turned out. CreatedAt is RFC3339 with nanoseconds.
type KnowledgeFeedbackEvent struct {
	ID         string
	DocumentID string
	SessionID  string
	Query      string
	Outcome    string
	Note       string
	CreatedAt  string
}

const KnowledgeOutcomeRetrieved = "retrieved"

// KnowledgeFeedbackTotals is the per-document tally the quality score is built
// from.
type KnowledgeFeedbackTotals struct {
	Retrievals   int
	Adopted      int
	Corrected    int
	Rejected     int
	LastFeedback string
}

// RecordKnowledgeFeedback upserts events under the schema's keys: one retrieval
// per (document, session, query) and one verdict per (document, session), the
// newest write winning. That is the same rule the append log applied while
// replaying itself, moved to where it can be enforced.
func RecordKnowledgeFeedback(events []KnowledgeFeedbackEvent) error {
	if len(events) == 0 {
		return nil
	}
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
	for _, event := range events {
		if err := upsertKnowledgeFeedback(tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertKnowledgeFeedback(exec sqlExecer, event KnowledgeFeedbackEvent) error {
	// The conflict target names the partial index that applies to this outcome;
	// a retrieval can never collide with a verdict or the other way round.
	conflict := `(document_id,session_id,query) WHERE outcome = 'retrieved'`
	if event.Outcome != KnowledgeOutcomeRetrieved {
		conflict = `(document_id,session_id) WHERE outcome <> 'retrieved'`
	}
	_, err := exec.Exec(`INSERT INTO knowledge_feedback
		(id,document_id,session_id,query,outcome,note,created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT `+conflict+` DO UPDATE SET
			outcome=excluded.outcome, note=excluded.note, created_at=excluded.created_at`,
		event.ID, event.DocumentID, event.SessionID, event.Query, event.Outcome,
		event.Note, event.CreatedAt)
	return err
}

// KnowledgeFeedbackByDocument tallies feedback per document in one query.
func KnowledgeFeedbackByDocument() (map[string]KnowledgeFeedbackTotals, error) {
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
	rows, err := db.Query(`SELECT document_id,
			SUM(outcome = 'retrieved'),
			SUM(outcome = 'adopted'),
			SUM(outcome = 'corrected'),
			SUM(outcome = 'rejected'),
			MAX(created_at)
		FROM knowledge_feedback GROUP BY document_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]KnowledgeFeedbackTotals{}
	for rows.Next() {
		var documentID string
		var value KnowledgeFeedbackTotals
		if err := rows.Scan(&documentID, &value.Retrievals, &value.Adopted,
			&value.Corrected, &value.Rejected, &value.LastFeedback); err != nil {
			return nil, err
		}
		totals[documentID] = value
	}
	return totals, rows.Err()
}

// DeleteKnowledgeFeedback drops a document's feedback. Deleting the markdown
// file cannot cascade — the database has no row to hang a foreign key on — so
// the delete path calls this, and `atm knowledge doctor` reports whatever
// survives a file removed behind ATM's back.
func DeleteKnowledgeFeedback(documentID string) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM knowledge_feedback WHERE document_id=?`, documentID)
	return err
}

// KnowledgeFeedbackDocumentIDs lists the documents feedback refers to, so a
// caller holding the real document list can spot the ones that no longer exist.
func KnowledgeFeedbackDocumentIDs() ([]string, error) {
	db, err := OpenReadOnly()
	if errors.Is(err, ErrDatabaseMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT DISTINCT document_id FROM knowledge_feedback ORDER BY document_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
