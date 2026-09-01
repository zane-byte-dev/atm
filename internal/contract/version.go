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

// IPCProtocolVersion versions the whole `atm _ipc` surface, not one payload.
//
// That is the point of having it: the app's other reads are ordinary CLI
// commands, so each of them is its own unversioned promise and renaming a flag
// breaks one screen at runtime. Everything reached through `_ipc` moves together
// under this number instead, and the app refuses a version it does not know
// rather than decoding a payload whose shape it is guessing at.
//
// Bump it when an existing verb's payload changes shape or a verb is removed.
// Adding a verb does not need a bump: an app that has never heard of it will
// never ask for it.
const IPCProtocolVersion = 1
