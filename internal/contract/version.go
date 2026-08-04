package contract

// 6 adds last_7_days to `ranges`: a rolling week, next to the rolling month that
// was already there. The compact pickers only have room for three windows, and
// this_week is the wrong one to show in a fixed slot because it collapses to a
// single day every Monday.
//
// 5 keys `ranges` by name — today, yesterday, this_week, last_week, this_month,
// last_30_days — instead of by rolling day count. A day count cannot express a
// calendar period, and "this month" is the figure a bill is read against, which
// "last 30 days" never matches. The desktop app refuses a version it does not
// know, so CLI and app move together.
const DashboardSchemaVersion = 6
