package aiday

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

func LoadHistory(ctx context.Context, db *sql.DB, from, to string) (History, error) {
	h := History{SchemaVersion: ContractVersion, From: from, To: to, Days: []Result{}}
	rows, err := db.QueryContext(ctx, `SELECT day FROM ai_day_results WHERE day>=? AND day<=? ORDER BY day DESC`, from, to)
	if err != nil {
		return h, err
	}
	var days []string
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			rows.Close()
			return h, err
		}
		days = append(days, day)
	}
	if err := rows.Close(); err != nil {
		return h, err
	}
	for _, day := range days {
		r, err := Load(ctx, db, day)
		if err != nil {
			return h, err
		}
		h.Days = append(h.Days, r)
	}
	return h, nil
}

func LoadAtlas(ctx context.Context, db *sql.DB) (Atlas, error) {
	a := Atlas{SchemaVersion: ContractVersion, GeneratedAt: time.Now().Unix(), Total: len(badgeCatalog), Badges: make([]Badge, 0, len(badgeCatalog))}
	// Progression is scoped to the collection start, so the listed dates must be
	// too — otherwise a badge reads "L1 · 3 days" above six backfilled dates.
	start := collectionStart(ctx, db)
	for _, d := range badgeCatalog {
		b := Badge{ID: d.ID, Name: d.Name, Description: d.Description, Family: d.Family, Kind: d.Kind}
		_ = db.QueryRowContext(ctx, `SELECT level,qualified_days,last_qualified,cooldown_until FROM ai_day_badge_progress WHERE badge_id=?`, d.ID).Scan(&b.Level, &b.QualifiedDays, &b.LastQualified, &b.CooldownUntil)
		b.Unlocked = b.Level > 0
		b.NextLevelDays = nextLevel(b.Level)
		b.Progress = levelProgress(b.Level, b.QualifiedDays)
		dateRows, dateErr := db.QueryContext(ctx, `SELECT day FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 AND day>=? ORDER BY day DESC LIMIT 60`, d.ID, start)
		if dateErr != nil {
			return a, dateErr
		}
		b.QualifiedDates = []string{}
		for dateRows.Next() {
			var day string
			if err := dateRows.Scan(&day); err != nil {
				dateRows.Close()
				return a, err
			}
			b.QualifiedDates = append(b.QualifiedDates, day)
		}
		if err := dateRows.Close(); err != nil {
			return a, err
		}
		if b.Unlocked {
			a.Unlocked++
		}
		a.Badges = append(a.Badges, b)
	}
	return a, nil
}

func LoadBadge(ctx context.Context, db *sql.DB, id string) (Badge, error) {
	a, err := LoadAtlas(ctx, db)
	if err != nil {
		return Badge{}, err
	}
	for _, b := range a.Badges {
		if b.ID == id {
			rows, err := db.QueryContext(ctx, `SELECT evidence_json FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 ORDER BY day DESC LIMIT 1`, id)
			if err == nil && rows.Next() {
				var raw string
				_ = rows.Scan(&raw)
				_ = json.Unmarshal([]byte(raw), &b.Evidence)
			}
			if rows != nil {
				rows.Close()
			}
			return b, nil
		}
	}
	return Badge{}, fmt.Errorf("unknown AI Day badge %q", id)
}

func SaveFeedback(ctx context.Context, db *sql.DB, feedback Feedback) error {
	if _, err := time.Parse(time.DateOnly, feedback.Day); err != nil {
		return fmt.Errorf("invalid feedback day: %w", err)
	}
	if feedback.Verdict != "accurate" && feedback.Verdict != "inaccurate" && feedback.Verdict != "corrected" {
		return fmt.Errorf("invalid verdict %q", feedback.Verdict)
	}
	if feedback.CorrectedBadge != "" {
		if _, ok := definition(feedback.CorrectedBadge); !ok {
			return fmt.Errorf("unknown AI Day badge %q", feedback.CorrectedBadge)
		}
	}
	for _, label := range feedback.SemanticLabels {
		valid := false
		for _, allowed := range SemanticIntents {
			if label == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("unknown semantic label %q", label)
		}
	}
	raw, _ := json.Marshal(feedback.SemanticLabels)
	feedback.UpdatedAt = time.Now().Unix()
	_, err := db.ExecContext(ctx, `INSERT INTO ai_day_feedback(day,verdict,corrected_badge_id,semantic_labels_json,updated_at) VALUES (?,?,?,?,?) ON CONFLICT(day) DO UPDATE SET verdict=excluded.verdict,corrected_badge_id=excluded.corrected_badge_id,semantic_labels_json=excluded.semantic_labels_json,updated_at=excluded.updated_at`, feedback.Day, feedback.Verdict, feedback.CorrectedBadge, string(raw), feedback.UpdatedAt)
	return err
}

// ClearFeedback removes a day's verdict so the engine's own conclusion applies
// again. A correction has to be undoable: it overrides the computed badge for
// that day indefinitely, and the browser submits one on a single click.
func ClearFeedback(ctx context.Context, db *sql.DB, day string) error {
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return fmt.Errorf("invalid feedback day: %w", err)
	}
	_, err := db.ExecContext(ctx, `DELETE FROM ai_day_feedback WHERE day=?`, day)
	return err
}

// CountEventsOlderThan reports how many derived events a retention change would
// delete, so the caller can say so before doing it.
func CountEventsOlderThan(ctx context.Context, db *sql.DB, days int, now time.Time) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_day_events WHERE occurred_at < ?`, now.AddDate(0, 0, -days).Unix()).Scan(&n)
	return n, err
}

func LoadPrivacy(ctx context.Context, db *sql.DB) (Privacy, error) {
	p := Privacy{SchemaVersion: ContractVersion, SemanticEnabled: settingBool(ctx, db, "semantic_enabled", true), RetentionDays: settingInt(ctx, db, "retention_days", 90), RawRetained: false, Sources: []SourceSetting{}}
	rows, err := db.QueryContext(ctx, `WITH names AS (SELECT DISTINCT COALESCE(NULLIF(agent,''),'unknown') source FROM sessions UNION SELECT source FROM ai_day_sources) SELECT n.source,COALESCE(s.enabled,1),COALESCE(s.semantic_enabled,1),COUNT(e.event_id),COALESCE(MAX(e.occurred_at),0) FROM names n LEFT JOIN ai_day_sources s ON s.source=n.source LEFT JOIN ai_day_events e ON e.source=n.source GROUP BY n.source,s.enabled,s.semantic_enabled ORDER BY n.source`)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var s SourceSetting
		if err := rows.Scan(&s.Source, &s.Enabled, &s.SemanticEnabled, &s.EventCount, &s.LastEventAt); err != nil {
			return p, err
		}
		// ATM's own model calls are never ingested, so offering a permission
		// toggle for them would present a control that does nothing.
		if IsSelfSource(s.Source) {
			continue
		}
		p.Sources = append(p.Sources, s)
	}
	return p, rows.Err()
}

func SetSource(ctx context.Context, db *sql.DB, source string, enabled, semantic bool) error {
	if source == "" {
		return errors.New("source is required")
	}
	_, err := db.ExecContext(ctx, `INSERT INTO ai_day_sources(source,enabled,semantic_enabled,updated_at) VALUES (?,?,?,?) ON CONFLICT(source) DO UPDATE SET enabled=excluded.enabled,semantic_enabled=excluded.semantic_enabled,updated_at=excluded.updated_at`, source, enabled, semantic, time.Now().Unix())
	return err
}
func SetPrivacy(ctx context.Context, db *sql.DB, semantic *bool, retention *int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if semantic != nil {
		value := "0"
		if *semantic {
			value = "1"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_day_settings(key,value,updated_at) VALUES ('semantic_enabled',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, value, now); err != nil {
			return err
		}
		if !*semantic {
			if _, err = tx.ExecContext(ctx, `UPDATE ai_day_events SET semantic_labels_json='[]', semantic_confidence=0`); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM ai_day_session_features`); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM ai_day_features`); err != nil {
				return err
			}
		}
	}
	if retention != nil {
		if *retention < 1 || *retention > 3650 {
			return errors.New("retention days must be between 1 and 3650")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_day_settings(key,value,updated_at) VALUES ('retention_days',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, strconv.Itoa(*retention), now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if semantic != nil && !*semantic {
		if err := rebuildBadgeProgress(ctx, db); err != nil {
			return err
		}
	}
	if retention != nil {
		_, err = PruneEvents(ctx, db, time.Now())
	}
	return err
}

func PruneEvents(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	days := settingInt(ctx, db, "retention_days", 90)
	result, err := db.ExecContext(ctx, `DELETE FROM ai_day_events WHERE occurred_at < ?`, now.AddDate(0, 0, -days).Unix())
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}
func settingInt(ctx context.Context, db queryRower, key string, fallback int) int {
	var value string
	if db.QueryRowContext(ctx, `SELECT value FROM ai_day_settings WHERE key=?`, key).Scan(&value) != nil {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func DeleteRange(ctx context.Context, db *sql.DB, from, to string) (DeleteSummary, error) {
	s := DeleteSummary{From: from, To: to}
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return s, err
	}
	end, err := time.Parse(time.DateOnly, to)
	if err != nil || end.Before(start) {
		return s, errors.New("invalid delete range")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return s, err
	}
	defer tx.Rollback()
	eventResult, err := tx.ExecContext(ctx, `DELETE FROM ai_day_events WHERE occurred_at>=? AND occurred_at<?`, start.Unix(), end.AddDate(0, 0, 1).Unix())
	if err != nil {
		return s, err
	}
	s.Events, _ = eventResult.RowsAffected()
	feedbackResult, err := tx.ExecContext(ctx, `DELETE FROM ai_day_feedback WHERE day>=? AND day<=?`, from, to)
	if err != nil {
		return s, err
	}
	s.Feedback, _ = feedbackResult.RowsAffected()
	projectionResult, err := tx.ExecContext(ctx, `DELETE FROM ai_day_features WHERE day>=? AND day<=?`, from, to)
	if err != nil {
		return s, err
	}
	s.Projections, _ = projectionResult.RowsAffected()
	if err := tx.Commit(); err != nil {
		return s, err
	}
	return s, rebuildBadgeProgress(ctx, db)
}

func DeleteSource(ctx context.Context, db *sql.DB, source string) (int64, error) {
	if err := SetSource(ctx, db, source, false, false); err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM ai_day_events WHERE source=?`, source)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	// A daily projection does not record which source contributed each metric.
	// Drop projections rather than leave deleted-source evidence visible; the
	// next rebuild recreates them from the remaining enabled sources.
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_day_session_features`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_day_features`); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, rebuildBadgeProgress(ctx, db)
}

type Export struct {
	SchemaVersion int        `json:"schema_version"`
	ExportedAt    int64      `json:"exported_at"`
	Privacy       Privacy    `json:"privacy"`
	Atlas         Atlas      `json:"atlas"`
	History       History    `json:"history"`
	Feedback      []Feedback `json:"feedback"`
}

func ExportAll(ctx context.Context, db *sql.DB) (Export, error) {
	e := Export{SchemaVersion: ContractVersion, ExportedAt: time.Now().Unix(), Feedback: []Feedback{}}
	var err error
	e.Privacy, err = LoadPrivacy(ctx, db)
	if err != nil {
		return e, err
	}
	e.Atlas, err = LoadAtlas(ctx, db)
	if err != nil {
		return e, err
	}
	var min, max string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MIN(day),''),COALESCE(MAX(day),'') FROM ai_day_results`).Scan(&min, &max)
	if min != "" {
		e.History, err = LoadHistory(ctx, db, min, max)
		if err != nil {
			return e, err
		}
	} else {
		e.History = History{SchemaVersion: ContractVersion, Days: []Result{}}
	}
	rows, err := db.QueryContext(ctx, `SELECT day,verdict,corrected_badge_id,semantic_labels_json,updated_at FROM ai_day_feedback ORDER BY day`)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var f Feedback
		var raw string
		if err := rows.Scan(&f.Day, &f.Verdict, &f.CorrectedBadge, &raw, &f.UpdatedAt); err != nil {
			return e, err
		}
		_ = json.Unmarshal([]byte(raw), &f.SemanticLabels)
		e.Feedback = append(e.Feedback, f)
	}
	sort.Slice(e.Feedback, func(i, j int) bool { return e.Feedback[i].Day < e.Feedback[j].Day })
	return e, rows.Err()
}
