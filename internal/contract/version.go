package contract

// 6 adds last_7_days to `ranges`: a rolling week, next to the rolling month that
// was already there. The compact pickers only have room for three windows, and
// this_week is the wrong one to show in a fixed slot because it collapses to a
// single day every Monday.
//
// 5 keys `ranges` by name — today, yesterday, this_week, last_week, this_month,
// last_30_days — instead of by rolling day count. A day count cannot express a
// calendar period, and "this month" is the figure a bill is read against, which
// "last 30 days" never matches. Consumers can reject a schema they do not know
// instead of guessing at changed payload fields.
const DashboardSchemaVersion = 6
