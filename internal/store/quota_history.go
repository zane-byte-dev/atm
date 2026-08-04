package store

import (
	"database/sql"
	"time"
)

// QuotaHistoryRetention is how far back samples are kept. A rate limit's longest
// window is a week, so a week of history is enough to see one full cycle, and the
// table stays small enough (a handful of windows at sync cadence) that pruning on
// write costs nothing worth measuring.
const QuotaHistoryRetention = 7 * 24 * time.Hour

// QuotaTrendLookback bounds how far back a rate is computed from. Beyond a few
// hours the number stops describing "right now", which is the only question the
// menu bar reading has to answer.
const QuotaTrendLookback = 6 * time.Hour

// QuotaSample is one observation of one rate-limit window.
type QuotaSample struct {
	Agent         string
	WindowMinutes int
	UsedPercent   float64
	// ResetsAt is the refill time the source reported, or 0 when it reports none.
	ResetsAt int64
	TS       int64
}

// QuotaTrend is how fast a window is filling, derived from the samples that
// belong to the current refill period.
type QuotaTrend struct {
	Agent          string  `json:"agent"`
	WindowMinutes  int     `json:"window_minutes"`
	PercentPerHour float64 `json:"percent_per_hour"`
	Samples        int     `json:"samples"`
	SpanMinutes    int     `json:"span_minutes"`
	FromPercent    float64 `json:"from_percent"`
	ToPercent      float64 `json:"to_percent"`
	// FullAt is when the window would reach 100% if the current rate held, or 0
	// when it is not filling. It is a projection, not a promise.
	FullAt int64 `json:"full_at,omitempty"`
	// FullBeforeReset marks the case worth acting on: at this rate the quota runs
	// out before it refills.
	FullBeforeReset bool `json:"full_before_reset"`
}

// RecordQuotaSamples appends observations and prunes anything past the retention
// window. Duplicate (agent, window, ts) rows are ignored rather than merged, so
// two syncs in the same second cannot fail the write.
func RecordQuotaSamples(db *sql.DB, samples []QuotaSample, now time.Time) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sample := range samples {
		if sample.Agent == "" || sample.WindowMinutes <= 0 {
			continue
		}
		ts := sample.TS
		if ts == 0 {
			ts = now.Unix()
		}
		if err := execTx(tx, `INSERT OR IGNORE INTO quota_history
			(agent, window_minutes, used_percent, resets_at, ts) VALUES (?, ?, ?, ?, ?)`,
			sample.Agent, sample.WindowMinutes, sample.UsedPercent, sample.ResetsAt, ts); err != nil {
			return err
		}
	}
	if err := execTx(tx, `DELETE FROM quota_history WHERE ts < ?`,
		now.Add(-QuotaHistoryRetention).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// QuotaTrendFor computes the fill rate of one window. ok is false when there is
// not enough history to say anything — no samples at all, a single sample, or a
// span too short to divide by — and every caller is expected to fall back to
// showing the plain percentage rather than an empty trend.
func QuotaTrendFor(db *sql.DB, agent string, windowMinutes int, now time.Time) (QuotaTrend, bool, error) {
	rows, err := db.Query(`SELECT used_percent, resets_at, ts FROM quota_history
		WHERE agent = ? AND window_minutes = ? AND ts >= ?
		ORDER BY ts`, agent, windowMinutes, now.Add(-QuotaTrendLookback).Unix())
	if err != nil {
		return QuotaTrend{}, false, err
	}
	defer rows.Close()
	var samples []QuotaSample
	for rows.Next() {
		sample := QuotaSample{Agent: agent, WindowMinutes: windowMinutes}
		if err := rows.Scan(&sample.UsedPercent, &sample.ResetsAt, &sample.TS); err != nil {
			return QuotaTrend{}, false, err
		}
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return QuotaTrend{}, false, err
	}
	trend, ok := quotaTrendFromSamples(agent, windowMinutes, samples)
	return trend, ok, nil
}

// quotaTrendFromSamples derives the rate from the trailing run of samples that
// share one refill period. Two things end a period, and both must be honoured or
// the rate is nonsense:
//
//   - resets_at changes: the source moved to the next window.
//   - used_percent drops: the quota refilled. This is the case that catches a
//     source reporting no reset time at all, where resets_at is 0 for every
//     sample and the fall back to zero is the only evidence a period ended.
//
// Differencing the whole series instead would turn a refill into a large negative
// rate, and a refill mid-series into a rate near zero — reporting "steady" for a
// window that is in fact filling fast.
func quotaTrendFromSamples(agent string, windowMinutes int, samples []QuotaSample) (QuotaTrend, bool) {
	if len(samples) < 2 {
		return QuotaTrend{}, false
	}
	start := len(samples) - 1
	for i := len(samples) - 1; i > 0; i-- {
		if samples[i].ResetsAt != samples[i-1].ResetsAt {
			break
		}
		if samples[i].UsedPercent < samples[i-1].UsedPercent {
			break
		}
		start = i - 1
	}
	segment := samples[start:]
	if len(segment) < 2 {
		return QuotaTrend{}, false
	}
	first, last := segment[0], segment[len(segment)-1]
	elapsed := time.Duration(last.TS-first.TS) * time.Second
	if elapsed <= 0 {
		return QuotaTrend{}, false
	}
	trend := QuotaTrend{
		Agent:          agent,
		WindowMinutes:  windowMinutes,
		PercentPerHour: (last.UsedPercent - first.UsedPercent) / elapsed.Hours(),
		Samples:        len(segment),
		SpanMinutes:    int(elapsed.Minutes()),
		FromPercent:    first.UsedPercent,
		ToPercent:      last.UsedPercent,
	}
	if trend.PercentPerHour > 0 && last.UsedPercent < 100 {
		hoursToFull := (100 - last.UsedPercent) / trend.PercentPerHour
		fullAt := time.Unix(last.TS, 0).Add(time.Duration(hoursToFull * float64(time.Hour)))
		trend.FullAt = fullAt.Unix()
		trend.FullBeforeReset = last.ResetsAt > 0 && fullAt.Before(time.Unix(last.ResetsAt, 0))
	}
	return trend, true
}
