package aiday

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type sessionAggregate struct {
	source, modality, mode       string
	events, turns, tools, active int64
	semantic                     map[string]int64
}

func aggregate(ctx context.Context, db *sql.DB, start, end time.Time, loc *time.Location) (Features, error) {
	f := Features{Day: start.Format(time.DateOnly), Timezone: loc.String(), BuiltAt: time.Now().Unix(), FeatureVersion: FeatureVersion, SemanticCounts: map[string]int64{}, ModalityCounts: map[string]int64{}}
	rows, err := db.QueryContext(ctx, `SELECT session_hash, source, event_type, quantity, modality, execution_mode, input_tokens, output_tokens, cache_create_tokens, cache_read_tokens, duration_ms, semantic_labels_json FROM ai_day_events WHERE occurred_at >= ? AND occurred_at < ? ORDER BY occurred_at, event_id`, start.Unix(), end.Unix())
	if err != nil {
		return Features{}, err
	}
	defer rows.Close()
	sessions := map[string]*sessionAggregate{}
	sources := map[string]bool{}
	for rows.Next() {
		var session, source, eventType, modality, mode, labelsJSON string
		var quantity, input, output, cacheCreate, cacheRead, duration int64
		if err := rows.Scan(&session, &source, &eventType, &quantity, &modality, &mode, &input, &output, &cacheCreate, &cacheRead, &duration, &labelsJSON); err != nil {
			return Features{}, err
		}
		s := sessions[session]
		if s == nil {
			s = &sessionAggregate{source: source, modality: modality, mode: mode, semantic: map[string]int64{}}
			sessions[session] = s
		}
		s.events++
		if eventType == "turn" {
			f.TurnCount += quantity
			s.turns += quantity
		}
		if eventType == "tool" {
			f.ToolCalls += quantity
			s.tools += quantity
			s.mode = "agentic"
		}
		f.EventCount++
		f.InputTokens += input
		f.OutputTokens += output
		f.CacheCreateTokens += cacheCreate
		f.CacheReadTokens += cacheRead
		f.GenerationSeconds += duration / 1000
		s.active += duration / 1000
		// Modality counts only turns and tool use, one per event, and never
		// weighted by quantity. Including `usage` events buried every real signal
		// under a "general" count equal to the day's API-call volume (137 general
		// vs 1 code on a day spent entirely in code), and weighting by quantity
		// mixed "one turn" with "this tool ran thirty times" in one total.
		if eventType == "turn" || eventType == "tool" {
			f.ModalityCounts[modality]++
		}
		sources[source] = true
		var labels []string
		if json.Unmarshal([]byte(labelsJSON), &labels) == nil {
			for _, label := range labels {
				f.SemanticCounts[label] += quantity
				s.semantic[label] += quantity
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Features{}, err
	}
	f.SessionCount, f.SourceCount = int64(len(sessions)), int64(len(sources))
	f.ActiveSeconds = f.GenerationSeconds
	// ATM's current transcripts do not expose reliable foreground/background
	// lifecycle events. Agentic tool time is represented as background only when
	// a duration is available; unknown time remains zero rather than invented.
	f.ForegroundSeconds = f.ActiveSeconds

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Features{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_day_session_features WHERE day=?`, f.Day); err != nil {
		return Features{}, err
	}
	for hash, s := range sessions {
		semanticJSON, _ := json.Marshal(s.semantic)
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_day_session_features(day,session_hash,source,modality,execution_mode,event_count,turn_count,tool_calls,active_seconds,semantic_counts_json,built_at,feature_version) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, f.Day, hash, s.source, s.modality, s.mode, s.events, s.turns, s.tools, s.active, string(semanticJSON), f.BuiltAt, FeatureVersion); err != nil {
			return Features{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Features{}, err
	}
	return f, nil
}

func loadFeatureDetails(ctx context.Context, db *sql.DB, result *Result) error {
	var semanticJSON, modalityJSON string
	err := db.QueryRowContext(ctx, `SELECT event_count,active_seconds,foreground_seconds,background_seconds,semantic_counts_json,modality_counts_json FROM ai_day_feature_details WHERE day=?`, result.Day).Scan(&result.Features.EventCount, &result.Features.ActiveSeconds, &result.Features.ForegroundSeconds, &result.Features.BackgroundSeconds, &semanticJSON, &modalityJSON)
	if err == sql.ErrNoRows {
		result.Features.SemanticCounts = map[string]int64{}
		result.Features.ModalityCounts = map[string]int64{}
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(semanticJSON), &result.Features.SemanticCounts); err != nil {
		return err
	}
	return json.Unmarshal([]byte(modalityJSON), &result.Features.ModalityCounts)
}
