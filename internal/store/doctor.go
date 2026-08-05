package store

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/zane-byte-dev/atm/internal/config"
)

type Coverage struct {
	Agent            string  `json:"agent"`
	Sessions         int     `json:"sessions"`
	ReportedRequests int     `json:"reported_requests"`
	DetailedRequests int     `json:"detailed_requests"`
	CoveragePercent  float64 `json:"coverage_percent"`
	CoverageStatus   string  `json:"coverage_status"`
	DetailedExcess   int     `json:"detailed_excess,omitempty"`
	UnknownModels    int     `json:"unknown_models"`
	// TimedRequests is how many of the detailed requests carry a usable
	// generation window, and TimedPercent its share. Speed is derived from record
	// timestamps, so some agents can be measured and others cannot; without these
	// two numbers an agent missing from the speed tables looks idle rather than
	// unmeasurable.
	TimedRequests int     `json:"timed_requests"`
	TimedPercent  float64 `json:"timed_percent"`
}

// ModelPricing is one model ATM has charged spend to, with the rate it resolved
// to and how. Doctor reports the ones ATM had to guess at: a rate ATM does not
// know produces a number that looks exactly like a known one, so the only way a
// user can tell which totals are trustworthy is to be told.
type ModelPricing struct {
	Model    string        `json:"model"`
	Source   PricingSource `json:"source"`
	CostUSD  float64       `json:"cost_usd"`
	Requests int           `json:"requests"`
}

// GetModelPricing lists every model with recorded spend, most expensive first.
// The rate is resolved live rather than read back from the row, which is sound
// because sync reprices stored cost against the same table; see RepriceUsage.
func GetModelPricing(db *sql.DB) ([]ModelPricing, error) {
	rows, err := db.Query(`SELECT model, COALESCE(SUM(cost_usd),0), COALESCE(SUM(request_count),0)
		FROM usage_events WHERE model <> '' GROUP BY model
		UNION ALL
		SELECT model, COALESCE(SUM(cost_usd),0), COALESCE(SUM(request_count),0)
		FROM usage u WHERE model <> '' AND NOT EXISTS (
			SELECT 1 FROM usage_events e WHERE e.session_id = u.session_id) GROUP BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]*ModelPricing{}
	var order []string
	for rows.Next() {
		var model string
		var cost float64
		var requests int
		if err := rows.Scan(&model, &cost, &requests); err != nil {
			return nil, err
		}
		if existing, ok := totals[model]; ok {
			existing.CostUSD += cost
			existing.Requests += requests
			continue
		}
		_, source := PricingFor(model)
		totals[model] = &ModelPricing{Model: model, Source: source, CostUSD: cost, Requests: requests}
		order = append(order, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]ModelPricing, 0, len(order))
	for _, model := range order {
		result = append(result, *totals[model])
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CostUSD != result[j].CostUSD {
			return result[i].CostUSD > result[j].CostUSD
		}
		return result[i].Model < result[j].Model
	})
	return result, nil
}

// ExtractionCounts is what a parser actually got out of one agent's transcripts,
// as opposed to how many files it found. Every agent ships its own format and
// changes it without telling ATM, so the failure mode that matters is not an
// error — it is a parser that still reads the file, still creates the session,
// and silently extracts nothing from it. Files on disk with zero messages or
// zero token accounting behind them is what that looks like from here.
type ExtractionCounts struct {
	Agent    string `json:"agent"`
	Sessions int    `json:"sessions"`
	Messages int    `json:"messages"`
	// UsageRows counts rows in either accounting table, because agents differ in
	// which one they populate and an agent that reports neither is the signal.
	UsageRows int `json:"usage_rows"`
	Tools     int `json:"tools"`
}

// GetExtractionCounts reports, per agent, how much content the parsers pulled
// out of the sessions they indexed. Read-only and index-based: it re-parses
// nothing, so doctor stays cheap.
func GetExtractionCounts(db *sql.DB) (map[string]ExtractionCounts, error) {
	rows, err := db.Query(`SELECT s.agent, COUNT(DISTINCT s.id),
		(SELECT COUNT(*) FROM messages m JOIN sessions ms ON m.session_id=ms.id WHERE ms.agent=s.agent),
		(SELECT COUNT(*) FROM usage u JOIN sessions us ON u.session_id=us.id WHERE us.agent=s.agent)
			+ (SELECT COUNT(*) FROM usage_events e JOIN sessions es ON e.session_id=es.id WHERE es.agent=s.agent),
		(SELECT COALESCE(SUM(t.count),0) FROM tools t JOIN sessions ts ON t.session_id=ts.id WHERE ts.agent=s.agent)
		FROM sessions s GROUP BY s.agent`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ExtractionCounts{}
	for rows.Next() {
		var counts ExtractionCounts
		if err := rows.Scan(&counts.Agent, &counts.Sessions, &counts.Messages,
			&counts.UsageRows, &counts.Tools); err != nil {
			return nil, err
		}
		out[counts.Agent] = counts
	}
	return out, rows.Err()
}

// ForgettableSession is a session resolved for `atm session forget`, with what
// forgetting it would cost the user in tokens and dollars.
type ForgettableSession struct {
	ID        string
	ShortID   string
	Agent     string
	Project   string
	FilePath  string
	CreatedAt string
	Messages  int
	Requests  int
	CostUSD   float64
	// SourceTracked reports that the last sync still found this session's
	// transcript, which makes forgetting it pointless: the next sync reindexes
	// it. Only retained history — a session whose source is gone — can be
	// forgotten for good.
	SourceTracked bool
}

// FindForgettableSession resolves a session id or short-id prefix, newest first,
// the way GetSession does.
func FindForgettableSession(db *sql.DB, idOrPrefix string) (*ForgettableSession, error) {
	var s ForgettableSession
	err := db.QueryRow(`SELECT s.id, s.short_id, s.agent, s.project, s.file_path, s.created_at,
			(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id),
			COALESCE((SELECT u.request_count FROM usage u WHERE u.session_id = s.id), 0),
			COALESCE((SELECT u.cost_usd FROM usage u WHERE u.session_id = s.id), 0),
			EXISTS (SELECT 1 FROM sync_state ss WHERE ss.file_path = s.file_path)
		FROM sessions s WHERE s.id = ? OR s.short_id LIKE ?
		ORDER BY s.last_ts DESC LIMIT 1`, sessionLookupArgs(idOrPrefix)...).
		Scan(&s.ID, &s.ShortID, &s.Agent, &s.Project, &s.FilePath, &s.CreatedAt,
			&s.Messages, &s.Requests, &s.CostUSD, &s.SourceTracked)
	if err != nil {
		return nil, err
	}
	s.Project = config.CanonicalProject(s.Project)
	return &s, nil
}

// ForgetSession drops a session and everything derived from it. Messages, tools,
// usage and usage_events go with it through ON DELETE CASCADE, so the tokens
// leave the totals — that is the point, and why the caller confirms first.
func ForgetSession(db *sql.DB, id string) error {
	res, err := db.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

// GetRetainedSessionCounts counts, per agent, the sessions ATM keeps after their
// transcript left the disk. See forgetRemovedSources for why a missing
// sync_state row is what identifies them: it is that table's primary key, and
// every synced file has one until the file disappears.
func GetRetainedSessionCounts(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`SELECT agent, COUNT(*) FROM sessions s
		WHERE NOT EXISTS (SELECT 1 FROM sync_state ss WHERE ss.file_path = s.file_path)
		GROUP BY agent`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var agent string
		var n int
		if err := rows.Scan(&agent, &n); err != nil {
			return nil, err
		}
		out[agent] = n
	}
	return out, rows.Err()
}

func GetCoverage(db *sql.DB) ([]Coverage, error) {
	rows, err := db.Query(`SELECT s.agent,COUNT(DISTINCT s.id),COALESCE(SUM(u.request_count),0),
		(SELECT COALESCE(SUM(e.request_count),0) FROM usage_events e JOIN sessions s2 ON e.session_id=s2.id WHERE s2.agent=s.agent),
		(SELECT COALESCE(SUM(e.request_count),0) FROM usage_events e JOIN sessions s3 ON e.session_id=s3.id WHERE s3.agent=s.agent AND e.model=''),
		(SELECT COALESCE(SUM(e.request_count),0) FROM usage_events e JOIN sessions s4 ON e.session_id=s4.id WHERE s4.agent=s.agent AND e.duration_ms > 0)
		FROM sessions s LEFT JOIN usage u ON u.session_id=s.id GROUP BY s.agent ORDER BY s.agent`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Coverage
	for rows.Next() {
		var c Coverage
		if err := rows.Scan(&c.Agent, &c.Sessions, &c.ReportedRequests, &c.DetailedRequests,
			&c.UnknownModels, &c.TimedRequests); err != nil {
			return nil, err
		}
		if c.DetailedRequests > 0 {
			c.TimedPercent = float64(c.TimedRequests) / float64(c.DetailedRequests) * 100
		}
		switch {
		case c.ReportedRequests == 0 && c.DetailedRequests == 0:
			c.CoverageStatus = "unavailable"
		case c.ReportedRequests == 0:
			c.CoverageStatus = "inconsistent"
			c.DetailedExcess = c.DetailedRequests
		case c.DetailedRequests > c.ReportedRequests:
			c.CoveragePercent = 100
			c.CoverageStatus = "inconsistent"
			c.DetailedExcess = c.DetailedRequests - c.ReportedRequests
		case c.DetailedRequests == c.ReportedRequests:
			c.CoveragePercent = 100
			c.CoverageStatus = "complete"
		default:
			c.CoveragePercent = float64(c.DetailedRequests) / float64(c.ReportedRequests) * 100
			c.CoverageStatus = "partial"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
