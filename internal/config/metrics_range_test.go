package config

import (
	"testing"
	"time"
)

func withLoc(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("timezone %s unavailable: %v", name, err)
	}
	old := Loc
	Loc = loc
	t.Cleanup(func() { Loc = old })
}

func TestMetricsRangeBoundsAreCalendarAligned(t *testing.T) {
	// Shanghai, deliberately not UTC: a month boundary computed in the wrong zone
	// files the last evening of the month under the next one.
	withLoc(t, "Asia/Shanghai")
	// A Wednesday, mid-month.
	now := time.Date(2026, 7, 15, 14, 30, 0, 0, Loc)

	cases := []struct {
		name        MetricsRange
		start, end  string
		expectedDay int
	}{
		{RangeToday, "2026-07-15 00:00", "2026-07-16 00:00", 1},
		{RangeYesterday, "2026-07-14 00:00", "2026-07-15 00:00", 1},
		// Week starts Monday: 2026-07-13.
		{RangeThisWeek, "2026-07-13 00:00", "2026-07-20 00:00", 7},
		{RangeLastWeek, "2026-07-06 00:00", "2026-07-13 00:00", 7},
		{RangeThisMonth, "2026-07-01 00:00", "2026-08-01 00:00", 31},
		// Rolling, so it ends tomorrow and starts six days back — not Monday.
		{RangeLast7Days, "2026-07-09 00:00", "2026-07-16 00:00", 7},
		{RangeLast30Days, "2026-06-16 00:00", "2026-07-16 00:00", 30},
	}
	for _, tc := range cases {
		t.Run(string(tc.name), func(t *testing.T) {
			start, end := tc.name.Bounds(now)
			if got := start.Format("2006-01-02 15:04"); got != tc.start {
				t.Errorf("start = %s, want %s", got, tc.start)
			}
			if got := end.Format("2006-01-02 15:04"); got != tc.end {
				t.Errorf("end = %s, want %s", got, tc.end)
			}
			if got := tc.name.Days(now); got != tc.expectedDay {
				t.Errorf("days = %d, want %d", got, tc.expectedDay)
			}
			if start.Location() != Loc || end.Location() != Loc {
				t.Errorf("bounds left the configured location: %v / %v", start.Location(), end.Location())
			}
		})
	}
}

// "This month" on the first of the month must return that one day, not an empty
// window — the case where an off-by-one is invisible for 30 days a month.
func TestMetricsRangeThisMonthOnTheFirstReturnsOneDay(t *testing.T) {
	withLoc(t, "Asia/Shanghai")
	now := time.Date(2026, 7, 1, 0, 30, 0, 0, Loc)
	start, end := RangeThisMonth.Bounds(now)
	if got := start.Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("start = %s", got)
	}
	if !start.Before(now) || !end.After(now) {
		t.Fatalf("now %v is not inside [%v, %v)", now, start, end)
	}
	if got := RangeThisMonth.Days(now); got != 31 {
		t.Fatalf("days = %d, want 31", got)
	}
}

func TestMetricsRangeCrossesMonthAndYearBoundaries(t *testing.T) {
	withLoc(t, "Asia/Shanghai")

	// A Friday, 2027-01-01. Its week began in the previous month and year.
	newYear := time.Date(2027, 1, 1, 9, 0, 0, 0, Loc)
	start, end := RangeThisWeek.Bounds(newYear)
	if got := start.Format("2006-01-02"); got != "2026-12-28" {
		t.Errorf("week start = %s, want 2026-12-28 (Monday of the previous year)", got)
	}
	if got := end.Format("2006-01-02"); got != "2027-01-04" {
		t.Errorf("week end = %s", got)
	}
	// Yesterday crosses the year.
	start, end = RangeYesterday.Bounds(newYear)
	if got := start.Format("2006-01-02"); got != "2026-12-31" {
		t.Errorf("yesterday start = %s", got)
	}
	if got := end.Format("2006-01-02"); got != "2027-01-01" {
		t.Errorf("yesterday end = %s", got)
	}
	// A month with 28 days, and one with 31, both come out whole.
	feb := time.Date(2027, 2, 10, 12, 0, 0, 0, Loc)
	if got := RangeThisMonth.Days(feb); got != 28 {
		t.Errorf("February 2027 days = %d, want 28", got)
	}
	dec := time.Date(2026, 12, 10, 12, 0, 0, 0, Loc)
	if got := RangeThisMonth.Days(dec); got != 31 {
		t.Errorf("December days = %d, want 31", got)
	}
	// A leap February, for the year rule that trips every four years.
	leap := time.Date(2028, 2, 10, 12, 0, 0, 0, Loc)
	if got := RangeThisMonth.Days(leap); got != 29 {
		t.Errorf("February 2028 days = %d, want 29", got)
	}
}

// Sunday is the end of a week, not the start of the next one. Go numbers it 0,
// so taking Weekday() at face value moves a whole day into the wrong week.
func TestMetricsRangeWeekStartsOnMonday(t *testing.T) {
	withLoc(t, "Asia/Shanghai")
	sunday := time.Date(2026, 7, 19, 23, 0, 0, 0, Loc)
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("fixture is not a Sunday: %v", sunday.Weekday())
	}
	start, end := RangeThisWeek.Bounds(sunday)
	if got := start.Format("2006-01-02"); got != "2026-07-13" {
		t.Errorf("week start = %s, want the preceding Monday 2026-07-13", got)
	}
	if !end.After(sunday) {
		t.Errorf("Sunday %v fell outside its own week, ending %v", sunday, end)
	}
}

func TestParseMetricsRangeAcceptsHyphensAndRejectsUnknown(t *testing.T) {
	for _, input := range []string{"this_month", "this-month", "THIS-MONTH", "  this_month  "} {
		got, err := ParseMetricsRange(input)
		if err != nil || got != RangeThisMonth {
			t.Errorf("ParseMetricsRange(%q) = %v, %v", input, got, err)
		}
	}
	if _, err := ParseMetricsRange("this_year"); err == nil {
		t.Error("unsupported range accepted")
	}
}

func TestMetricsRangeHourlyOnlyForSingleDays(t *testing.T) {
	withLoc(t, "Asia/Shanghai")
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, Loc)
	for _, r := range []MetricsRange{RangeToday, RangeYesterday} {
		if !r.Hourly(now) {
			t.Errorf("%s should bucket by hour", r)
		}
	}
	for _, r := range []MetricsRange{RangeThisWeek, RangeLastWeek, RangeThisMonth, RangeLast7Days, RangeLast30Days} {
		if r.Hourly(now) {
			t.Errorf("%s should bucket by day", r)
		}
	}
}
