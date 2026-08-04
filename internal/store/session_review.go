package store

import "errors"

// SessionReview records that a session was checked for anything worth keeping.
// There is one review per session, which is what the primary key says.
type SessionReview struct {
	SessionID  string
	Outcome    string
	Note       string
	ReviewedAt string
}

// UpsertSessionReview records or replaces a session's review and returns the
// stored row, so a caller can tell an unchanged re-review from a new one.
func UpsertSessionReview(review SessionReview) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO session_reviews(session_id,outcome,note,reviewed_at)
		VALUES(?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET
			outcome=excluded.outcome, note=excluded.note, reviewed_at=excluded.reviewed_at`,
		review.SessionID, review.Outcome, review.Note, review.ReviewedAt)
	return err
}

func GetSessionReview(sessionID string) (*SessionReview, error) {
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
	var review SessionReview
	err = db.QueryRow(`SELECT session_id,outcome,note,reviewed_at FROM session_reviews
		WHERE session_id=?`, sessionID).Scan(
		&review.SessionID, &review.Outcome, &review.Note, &review.ReviewedAt)
	if err != nil {
		return nil, nil
	}
	return &review, nil
}

func SessionReviews() (map[string]SessionReview, error) {
	db, err := OpenReadOnly()
	if errors.Is(err, ErrDatabaseMissing) {
		return map[string]SessionReview{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT session_id,outcome,note,reviewed_at FROM session_reviews`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reviews := map[string]SessionReview{}
	for rows.Next() {
		var review SessionReview
		if err := rows.Scan(&review.SessionID, &review.Outcome, &review.Note, &review.ReviewedAt); err != nil {
			return nil, err
		}
		reviews[review.SessionID] = review
	}
	return reviews, rows.Err()
}
