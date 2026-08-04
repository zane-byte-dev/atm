package store

import (
	"testing"
	"time"
)

func quotaSampleAt(percent float64, resetsAt int64, ts int64) QuotaSample {
	return QuotaSample{Agent: "codex", WindowMinutes: 300, UsedPercent: percent, ResetsAt: resetsAt, TS: ts}
}

// The reset boundary is the whole reason resets_at is stored per sample. A window
// that refilled halfway through the lookback drops from high to low, and treating
// that as one series reports either a large negative rate or — worse — a rate near
// zero, which reads as "steady" for a window that is in fact filling fast.
func TestQuotaTrendSegmentsAtTheResetBoundary(t *testing.T) {
	base := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC).Unix()
	hour := int64(3600)
	firstReset := base + 2*hour
	secondReset := base + 8*hour

	samples := []QuotaSample{
		// Filling towards the first reset.
		quotaSampleAt(60, firstReset, base),
		quotaSampleAt(90, firstReset, base+hour),
		// Refilled: new period, and only these samples may be differenced.
		quotaSampleAt(5, secondReset, base+3*hour),
		quotaSampleAt(15, secondReset, base+4*hour),
	}
	trend, ok := quotaTrendFromSamples("codex", 300, samples)
	if !ok {
		t.Fatal("no trend from four samples")
	}
	if trend.Samples != 2 || trend.FromPercent != 5 || trend.ToPercent != 15 {
		t.Fatalf("trend crossed the reset boundary: %#v", trend)
	}
	if trend.PercentPerHour < 9.9 || trend.PercentPerHour > 10.1 {
		t.Fatalf("rate = %v, want ~10 percent per hour", trend.PercentPerHour)
	}
	// Never a negative rate from a refill: that is the false signal being avoided.
	if trend.PercentPerHour < 0 {
		t.Fatalf("refill produced negative usage: %v", trend.PercentPerHour)
	}
}

// Some sources report no reset time at all, so resets_at is 0 for every sample
// and a falling percentage is the only evidence a period ended.
func TestQuotaTrendSegmentsOnARefillWithoutAResetTime(t *testing.T) {
	base := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC).Unix()
	hour := int64(3600)
	samples := []QuotaSample{
		quotaSampleAt(70, 0, base),
		quotaSampleAt(95, 0, base+hour),
		quotaSampleAt(10, 0, base+2*hour),
		quotaSampleAt(30, 0, base+3*hour),
	}
	trend, ok := quotaTrendFromSamples("codex", 300, samples)
	if !ok {
		t.Fatal("no trend")
	}
	if trend.Samples != 2 || trend.FromPercent != 10 || trend.ToPercent != 30 {
		t.Fatalf("trend spanned the refill: %#v", trend)
	}
	if trend.PercentPerHour < 19.9 || trend.PercentPerHour > 20.1 {
		t.Fatalf("rate = %v, want ~20 percent per hour", trend.PercentPerHour)
	}
}

// The projection is the reading that changes a decision: at this rate the quota
// runs out before it refills, so stop or switch models now.
func TestQuotaTrendProjectsRunningOutBeforeReset(t *testing.T) {
	base := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC).Unix()
	hour := int64(3600)

	// 20 points an hour with 40 left and 6 hours until reset: full in ~2h.
	soon := []QuotaSample{
		quotaSampleAt(40, base+6*hour, base),
		quotaSampleAt(60, base+6*hour, base+hour),
	}
	trend, ok := quotaTrendFromSamples("codex", 300, soon)
	if !ok {
		t.Fatal("no trend")
	}
	if !trend.FullBeforeReset {
		t.Fatalf("did not project running out before reset: %#v", trend)
	}
	if trend.FullAt <= base+hour {
		t.Fatalf("full_at %d is not in the future", trend.FullAt)
	}

	// Same slope but the reset arrives first: not the alarming case.
	later := []QuotaSample{
		quotaSampleAt(40, base+hour+60, base),
		quotaSampleAt(60, base+hour+60, base+hour),
	}
	trend, ok = quotaTrendFromSamples("codex", 300, later)
	if !ok {
		t.Fatal("no trend")
	}
	if trend.FullBeforeReset {
		t.Fatalf("reset comes first, so this should not be flagged: %#v", trend)
	}
}

// Thin history is not a trend of zero. Callers show the plain percentage instead,
// so "not enough data yet" must be distinguishable from "not moving".
func TestQuotaTrendNeedsTwoSamplesInOnePeriod(t *testing.T) {
	base := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC).Unix()
	if _, ok := quotaTrendFromSamples("codex", 300, nil); ok {
		t.Error("empty history produced a trend")
	}
	if _, ok := quotaTrendFromSamples("codex", 300, []QuotaSample{quotaSampleAt(50, 0, base)}); ok {
		t.Error("a single sample produced a trend")
	}
	// Two samples at the same instant cannot be divided by an elapsed time.
	sameInstant := []QuotaSample{quotaSampleAt(50, 0, base), quotaSampleAt(60, 0, base)}
	if _, ok := quotaTrendFromSamples("codex", 300, sameInstant); ok {
		t.Error("zero elapsed time produced a trend")
	}
	// A lone sample after a refill is also not enough.
	afterRefill := []QuotaSample{quotaSampleAt(90, 0, base), quotaSampleAt(5, 0, base+3600)}
	if _, ok := quotaTrendFromSamples("codex", 300, afterRefill); ok {
		t.Error("one sample in the new period produced a trend")
	}
}

func TestRecordQuotaSamplesPrunesPastRetentionAndReadsBack(t *testing.T) {
	db := openTempDB(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	// One sample inside the window and one already past it.
	old := now.Add(-QuotaHistoryRetention - time.Hour)
	if err := RecordQuotaSamples(db, []QuotaSample{
		quotaSampleAt(10, 0, old.Unix()),
		quotaSampleAt(20, 0, now.Add(-2*time.Hour).Unix()),
		quotaSampleAt(30, 0, now.Add(-time.Hour).Unix()),
	}, now); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := db.QueryRow(`SELECT COUNT(*) FROM quota_history`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != 2 {
		t.Fatalf("kept %d rows, want the 2 inside the retention window", kept)
	}

	trend, ok, err := QuotaTrendFor(db, "codex", 300, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no trend from two retained samples")
	}
	if trend.PercentPerHour < 9.9 || trend.PercentPerHour > 10.1 {
		t.Fatalf("rate = %v, want ~10 percent per hour", trend.PercentPerHour)
	}

	// Re-recording the same instant must not fail the write.
	if err := RecordQuotaSamples(db, []QuotaSample{
		quotaSampleAt(30, 0, now.Add(-time.Hour).Unix()),
	}, now); err != nil {
		t.Fatalf("duplicate sample: %v", err)
	}

	// A window with no history at all reports no trend rather than an error.
	if _, ok, err := QuotaTrendFor(db, "codex", 10080, now); err != nil || ok {
		t.Fatalf("unknown window: ok=%v err=%v", ok, err)
	}
	if _, ok, err := QuotaTrendFor(db, "grokbuild", 300, now); err != nil || ok {
		t.Fatalf("unknown agent: ok=%v err=%v", ok, err)
	}
}
