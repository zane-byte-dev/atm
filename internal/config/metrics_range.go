package config

import (
	"fmt"
	"strings"
	"time"
)

// MetricsRange names a reporting window for usage and spend. Rolling day counts
// alone cannot answer the question people actually ask about cost: "last 30 days"
// is not "this month", and a billing statement is drawn on calendar boundaries.
// Naming the window also gives "yesterday" somewhere to live — "today" is close to
// empty every morning, which is exactly when a review of the day before is wanted.
//
// Boundaries are computed in Loc, not UTC: a month that ends at the wrong hour
// puts a whole evening's spend in the wrong month.
type MetricsRange string

const (
	RangeToday      MetricsRange = "today"
	RangeYesterday  MetricsRange = "yesterday"
	RangeThisWeek   MetricsRange = "this_week"
	RangeLastWeek   MetricsRange = "last_week"
	RangeThisMonth  MetricsRange = "this_month"
	RangeLast7Days  MetricsRange = "last_7_days"
	RangeLast30Days MetricsRange = "last_30_days"
)

// MetricsRanges is every supported window, in the order a picker should show
// them: narrowing calendar periods first, then the rolling windows that have no
// calendar boundary at all.
var MetricsRanges = []MetricsRange{
	RangeToday, RangeYesterday, RangeThisWeek, RangeLastWeek, RangeThisMonth,
	RangeLast7Days, RangeLast30Days,
}

// ParseMetricsRange resolves a name, accepting hyphens for underscores so
// `--range this-month` works as well as `this_month`.
func ParseMetricsRange(value string) (MetricsRange, error) {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	for _, candidate := range MetricsRanges {
		if string(candidate) == normalized {
			return candidate, nil
		}
	}
	names := make([]string, 0, len(MetricsRanges))
	for _, candidate := range MetricsRanges {
		names = append(names, string(candidate))
	}
	return "", fmt.Errorf("unknown range %q; expected one of %s", value, strings.Join(names, ", "))
}

// Bounds returns the window as [start, end). end is exclusive so a day, a week
// and a month compose the same way, and no reading is ever counted twice at a
// boundary. For periods that include now, end is the start of the next period
// rather than now itself: a query cannot match a future timestamp anyway, and
// clamping to now would make two calls a second apart return different windows.
func (r MetricsRange) Bounds(now time.Time) (start, end time.Time) {
	now = now.In(Loc)
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, Loc)
	switch r {
	case RangeYesterday:
		return startOfToday.AddDate(0, 0, -1), startOfToday
	case RangeThisWeek:
		weekStart := startOfWeek(startOfToday)
		return weekStart, weekStart.AddDate(0, 0, 7)
	case RangeLastWeek:
		weekStart := startOfWeek(startOfToday).AddDate(0, 0, -7)
		return weekStart, weekStart.AddDate(0, 0, 7)
	case RangeThisMonth:
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, Loc)
		return monthStart, monthStart.AddDate(0, 1, 0)
	case RangeLast7Days:
		// Rolling, like last_30_days: 6 whole days plus the one in progress. Not
		// interchangeable with this_week, which holds only a few hours of data
		// every Monday morning — a fixed three-slot picker wants this one.
		return startOfToday.AddDate(0, 0, -6), startOfToday.AddDate(0, 0, 1)
	case RangeLast30Days:
		// Rolling and inclusive of today, matching what `--days 30` has always
		// meant: 29 whole days plus the one in progress.
		return startOfToday.AddDate(0, 0, -29), startOfToday.AddDate(0, 0, 1)
	default:
		return startOfToday, startOfToday.AddDate(0, 0, 1)
	}
}

// UnixBounds is Bounds in the epoch seconds the store queries take.
func (r MetricsRange) UnixBounds(now time.Time) (int64, int64) {
	start, end := r.Bounds(now)
	return start.Unix(), end.Unix()
}

// Days is how many calendar days the window spans, for the callers that still
// take a day count — day and hour bucket queries, and the subscription
// comparison, which divides spend by the period's length.
func (r MetricsRange) Days(now time.Time) int {
	start, end := r.Bounds(now)
	return int(end.Sub(start).Hours() / 24)
}

// Hourly reports whether the window is short enough that hour buckets, not day
// buckets, are the readable granularity.
func (r MetricsRange) Hourly(now time.Time) bool {
	return r.Days(now) <= 1
}

// startOfWeek snaps to Monday. Go numbers Sunday as 0, and taking that literally
// would put Sunday's work in the week that is about to start rather than the one
// it finished.
func startOfWeek(day time.Time) time.Time {
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset)
}
