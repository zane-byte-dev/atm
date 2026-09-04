package store

import (
	"database/sql"
	"fmt"
)

// SchemaVersion is the shape createSchema produces. Changing the schema means
// bumping this, adding a step to migrate(), running it on the live database, then
// raising minUpgradableVersion and deleting the step again. `atm sync status`
// reports it.

// minUpgradableVersion is the oldest existing database migrate() can still bring
// forward. ATM is a single-user tool with one live database.
//
// v22 adds usage_events.duration_ms so request throughput can be measured, v23
// indexes event timestamps for event-time dashboard queries, and v24 adds
// quota_history so a rate limit reading can carry a direction. v25 adds the
// connector collection ledger used by local automatic requirement intake, v26
// adds per-source exclusion patterns, v27 adds the synced chat archive that
// makes reading and searching a conversation work offline, and v28 adds
// collection_items.proposed_action so an on-demand analysis can hold a decision
// for a human to confirm, plus collection_sources.instruction for what a
// particular source should be watched for. v29 adds per-source processing
// strategy and cadence. v30 gives collection a second destination: the
// 'insight' decision, which distils chat worth remembering into the knowledge
// base instead of forcing it into a Todo or dropping it. v31 removes the
// service-specific source-kind constraint so registered connectors own their
// vocabulary. v32 adds collection_sources.decision_unit so a notification feed
// can be decided per message instead of per time window. v33 adds todos.creator
// so "who filed this" becomes a field that can be filtered and counted, instead
// of a guess read out of the free-text source. v34 adds
// collection_items.attempts so a batch the classifier cannot process stops
// being retried forever. v35 drops todos.lane: the work/personal lane was a
// second, weaker classification living alongside the tag table — nothing
// branched on it, the value was derived rather than chosen (the app took the
// dominant lane of the project, the collector hardcoded "work"), and the only
// reader was an optional --lane filter. v35 also drops todos.feature_path, which
// no code has read or written for as long as it has existed — it survived only
// because dropping it alone did not justify a schema bump, and this one is
// already paid for. v36 restores explicit Agent task runs: one row is the
// durable claim, process record and outcome for one manual Todo dispatch. v37
// lets each task-producing collection source explicitly opt into dispatching a
// newly created Todo, and records that handoff separately from classification.
// v40 adds the derived AI Day feature and result tables. They deliberately hold
// only daily aggregates and selected concept metadata: transcripts remain in the
// existing session mirror and are never copied into this product surface. v41
// completes that projection with content-free normalized events, session
// aggregates, badge history/progress, feedback, source permissions and settings.
// v43 stops treating an insight as an automatic knowledge write: each collection
// item records the knowledge document created only after the user explicitly
// saves its conclusion. v44 adds collection_items.read_at so newly collected
// Todos, supplements and conclusions remain visibly pending until the user has
// actually opened or acknowledged them. v45 adds approvals, the outbound action
// gate's ledger: one row per attempt by an agent to run a command that reaches
// someone else, plus what the user decided about it. Nothing is backfilled —
// before v45 nothing was gated, so there are no historical decisions to invent.
// v46 adds collection_sources.muted so one noisy source can be taken out of the
// desktop notifications without being taken out of collection: unread counts and
// badges are untouched, only the banner is. Nothing is backfilled — 0 is the
// behaviour every source had before. v47 adds collection_items.archived_at so a
// person can settle a collected result without deleting its audit trail or
// releasing its messages for collection again. v48 adds todo_images, the
// normalized metadata for locally managed Todo image files. v49 adds the Work
// effect outbox: lifecycle transitions and their filesystem/UI effects are
// committed together, so a process crash cannot make a successful transition
// permanently lose its follow-up work. v50 adds append-only Todo plan revisions.
// A plan is execution state, not Todo lifecycle: completing every item never
// submits or closes the Todo. v52 removes the background-dispatch subsystem:
// task_runs, the collection dispatch columns and todo_plan_revisions.run_id all
// go, because ATM no longer starts an Agent and nothing can write them. v53
// persists session lineage, parser/content quality, structured result state and
// message provenance so a copied subagent history is no longer presented or
// counted as the child's own conversation. v54 adds a content-free CLI
// invocation ledger: command paths and stable outcome classes are observable,
// while arguments, error messages, working directories and user content are not
// representable in the schema. v55 records immutable Todo creation responses so
// a retried Web request cannot create a second Todo or return later edits. v56
// adds durable background execution receipts and domain change revisions.
//
// Keep the minimum at 21 while those upgrade steps exist; after the live database
// has been upgraded, raise this to SchemaVersion and delete the steps. Note what a
// hard
// reject costs: session tables rebuild from agent logs on the next `atm sync`,
// but todos, memory and knowledge are this database's own records and have
// nowhere to rebuild from.
const (
	SchemaVersion        = 56
	minUpgradableVersion = 21
)

// datePattern matches the 'YYYY-MM-DD' form used by every date-only column.
// Empty string is the domain's "unset" value and is allowed alongside it.
const datePattern = `[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]`

// createSchema builds the whole database in its current shape. Fresh databases
// take this path instead of replaying history, so this file — not a chain of
// migrations — is the readable definition of what ATM stores. It runs inside the
// caller's transaction; see bootstrapSchema for why that matters.
func createSchema(tx *sql.Tx) error {
	statements := []string{
		backgroundJobsSchema,
		`CREATE INDEX idx_background_jobs_created ON background_jobs(created_at DESC)`,
		// --- agent session mirror: derived data, rebuilt by `atm sync` ---
		`CREATE TABLE sync_state (
			file_path    TEXT PRIMARY KEY,
			agent        TEXT NOT NULL,
			mtime_unix   INTEGER NOT NULL,
			size_bytes   INTEGER NOT NULL,
			offset_bytes INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE sync_health (
			scope             TEXT PRIMARY KEY,
			last_attempt_ts   INTEGER NOT NULL DEFAULT 0,
			last_success_ts   INTEGER NOT NULL DEFAULT 0,
			last_status       TEXT NOT NULL DEFAULT 'never',
			last_error        TEXT NOT NULL DEFAULT '',
			last_synced_files INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE sessions (
			id                TEXT PRIMARY KEY,
			short_id          TEXT NOT NULL,
			agent             TEXT NOT NULL,
			project           TEXT NOT NULL DEFAULT '',
			file_path         TEXT NOT NULL,
			created_at        TEXT NOT NULL DEFAULT '',
			created_ts        INTEGER NOT NULL DEFAULT 0,
			summary           TEXT NOT NULL DEFAULT '',
			last_ts           INTEGER NOT NULL DEFAULT 0,
			resume_id         TEXT NOT NULL DEFAULT '',
			root_session_id   TEXT NOT NULL DEFAULT '',
			parent_session_id TEXT NOT NULL DEFAULT '',
			agent_path        TEXT NOT NULL DEFAULT '',
			agent_nickname    TEXT NOT NULL DEFAULT '',
			subagent_depth    INTEGER NOT NULL DEFAULT 0,
			is_subagent       INTEGER NOT NULL DEFAULT 0,
			is_internal       INTEGER NOT NULL DEFAULT 0,
			parser_version    INTEGER NOT NULL DEFAULT 0,
			content_state     TEXT NOT NULL DEFAULT 'empty',
			result_status     TEXT NOT NULL DEFAULT 'unknown',
			latest_progress   TEXT NOT NULL DEFAULT '',
			final_result      TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_sessions_agent ON sessions(agent)`,
		`CREATE INDEX idx_sessions_created_ts ON sessions(created_ts)`,
		`CREATE INDEX idx_sessions_short_id ON sessions(short_id)`,
		`CREATE INDEX idx_sessions_last_ts ON sessions(last_ts)`,
		`CREATE INDEX idx_sessions_resume_id ON sessions(resume_id)`,
		`CREATE INDEX idx_sessions_root_session ON sessions(root_session_id)`,
		`CREATE INDEX idx_sessions_parent_session ON sessions(parent_session_id)`,
		`CREATE TABLE messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			seq        INTEGER NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			ts         INTEGER NOT NULL DEFAULT 0,
			scope      TEXT NOT NULL DEFAULT 'local',
			kind       TEXT NOT NULL DEFAULT 'conversation',
			phase      TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_messages_session ON messages(session_id)`,
		// seq is dense per session: a full re-sync deletes the session and starts
		// from 0, an incremental append continues from the highest seq. Two syncs
		// appending the same tail would violate this, which is exactly what the
		// constraint is for.
		`CREATE UNIQUE INDEX idx_messages_session_seq ON messages(session_id, seq)`,
		`CREATE INDEX idx_messages_ts ON messages(ts)`,
		`CREATE TABLE tools (
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			count      INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (session_id, name)
		)`,
		`CREATE TABLE usage (
			session_id          TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
			model               TEXT NOT NULL DEFAULT '',
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			request_count       INTEGER NOT NULL DEFAULT 0
		)`,
		// One row per model request. fingerprint is that request's identity as the
		// transcript reports it, and the unique index below is what keeps a
		// request counted once: resuming or forking a session copies the earlier
		// transcript into a new file, so the same request arrives twice from two
		// different sessions. The index is deliberately not scoped to
		// session_id — the duplicate is in another session, which is the whole
		// point. Transcripts that offer no identity leave fingerprint empty and
		// the partial index lets every one of them through.
		`CREATE TABLE usage_events (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id          TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			model               TEXT NOT NULL DEFAULT '',
			ts                  INTEGER NOT NULL DEFAULT 0,
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
			cost_usd            REAL NOT NULL DEFAULT 0,
			fingerprint         TEXT NOT NULL DEFAULT '',
			-- How many model calls this row represents (default 1). Aggregated
			-- turn usage (e.g. Grok modelCalls) stores the full call count here
			-- so rollup and doctor coverage can SUM rather than COUNT rows.
			request_count       INTEGER NOT NULL DEFAULT 1,
			-- How long the model spent generating, in milliseconds, covering every
			-- call this row represents (see request_count). Tool execution between
			-- calls is excluded. Grok reports this itself; for every other agent it
			-- is derived from the transcript's record timestamps, so 0 means "not
			-- measurable from this transcript", never "instant".
			duration_ms         INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX idx_usage_events_session ON usage_events(session_id)`,
		`CREATE INDEX idx_usage_events_ts ON usage_events(ts)`,
		`CREATE INDEX idx_usage_events_model ON usage_events(model)`,
		`CREATE UNIQUE INDEX idx_usage_events_fingerprint
			ON usage_events(fingerprint) WHERE fingerprint <> ''`,
		// ATM's own CLI invocation telemetry is intentionally independent of the
		// rebuildable session mirror. A session sync or forget must not erase the
		// success denominator used to diagnose command-contract failures.
		`CREATE TABLE cli_invocations (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			occurred_at  INTEGER NOT NULL,
			session_id  TEXT NOT NULL DEFAULT '',
			agent        TEXT NOT NULL DEFAULT '',
			version      TEXT NOT NULL DEFAULT '',
			command_path TEXT NOT NULL CHECK (command_path <> ''),
			exit_code    INTEGER NOT NULL,
			error_code   TEXT NOT NULL DEFAULT '',
			cause_class  TEXT NOT NULL DEFAULT '',
			retryable    INTEGER NOT NULL DEFAULT 0 CHECK (retryable IN (0,1)),
			duration_ms  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
			success      INTEGER NOT NULL CHECK (success IN (0,1)),
			CHECK (success = 0 OR exit_code = 0)
		)`,
		`CREATE INDEX idx_cli_invocations_time ON cli_invocations(occurred_at DESC)`,
		`CREATE INDEX idx_cli_invocations_session_time ON cli_invocations(session_id,occurred_at DESC)`,
		`CREATE INDEX idx_cli_invocations_failure_time ON cli_invocations(success,occurred_at DESC)`,
		// --- AI Day: rebuildable daily projections ---
		// These tables are derived from the session mirror. A rebuild replaces
		// one row per local calendar day, making the operation idempotent and the
		// selected concept reproducible without retaining another copy of messages.
		`CREATE TABLE ai_day_features (
			day                 TEXT PRIMARY KEY CHECK (day GLOB '` + datePattern + `'),
			timezone            TEXT NOT NULL,
			session_count       INTEGER NOT NULL DEFAULT 0,
			turn_count          INTEGER NOT NULL DEFAULT 0,
			tool_calls          INTEGER NOT NULL DEFAULT 0,
			source_count        INTEGER NOT NULL DEFAULT 0,
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
			generation_seconds  INTEGER NOT NULL DEFAULT 0,
			built_at            INTEGER NOT NULL,
			feature_version     INTEGER NOT NULL
		)`,
		`CREATE TABLE ai_day_results (
			day            TEXT PRIMARY KEY REFERENCES ai_day_features(day) ON DELETE CASCADE,
			state          TEXT NOT NULL CHECK (state IN ('ready','empty')),
			concept_id     TEXT NOT NULL DEFAULT '',
			title          TEXT NOT NULL DEFAULT '',
			explanation    TEXT NOT NULL DEFAULT '',
			tags_json      TEXT NOT NULL DEFAULT '[]',
			evidence_json  TEXT NOT NULL DEFAULT '[]',
			-- How much to trust this conclusion: baseline length, evidence
			-- strength and source coverage combined. Never raised by feedback.
			confidence     REAL NOT NULL DEFAULT 0,
			-- The selected badge's own normalized signal, kept separate so a
			-- weak-evidence day is distinguishable from a short-history one.
			evidence_strength REAL NOT NULL DEFAULT 0,
			-- 'computed' or 'user_corrected'.
			origin         TEXT NOT NULL DEFAULT 'computed',
			-- What the engine chose before a correction, so its own answer is
			-- preserved rather than overwritten by the user's.
			computed_badge_id TEXT NOT NULL DEFAULT '',
			baseline_days  INTEGER NOT NULL DEFAULT 0,
			generated_at   INTEGER NOT NULL,
			engine_version INTEGER NOT NULL
		)`,
		`CREATE TABLE ai_day_events (
			event_id             TEXT PRIMARY KEY,
			occurred_at          INTEGER NOT NULL,
			source               TEXT NOT NULL,
			session_hash         TEXT NOT NULL,
			event_type           TEXT NOT NULL,
			quantity             INTEGER NOT NULL DEFAULT 1,
			modality             TEXT NOT NULL DEFAULT 'general',
			execution_mode       TEXT NOT NULL DEFAULT 'interactive',
			input_tokens         INTEGER NOT NULL DEFAULT 0,
			output_tokens        INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens  INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens    INTEGER NOT NULL DEFAULT 0,
			duration_ms          INTEGER NOT NULL DEFAULT 0,
			semantic_labels_json TEXT NOT NULL DEFAULT '[]',
			semantic_confidence  REAL NOT NULL DEFAULT 0,
			raw_content_retained INTEGER NOT NULL DEFAULT 0 CHECK (raw_content_retained = 0),
			schema_version       INTEGER NOT NULL,
			ingested_at          INTEGER NOT NULL
		)`,
		`CREATE INDEX idx_ai_day_events_time ON ai_day_events(occurred_at)`,
		`CREATE INDEX idx_ai_day_events_source_time ON ai_day_events(source, occurred_at)`,
		`CREATE INDEX idx_ai_day_events_session_time ON ai_day_events(session_hash, occurred_at)`,
		`CREATE TABLE ai_day_session_features (
			day                  TEXT NOT NULL CHECK (day GLOB '` + datePattern + `'),
			session_hash         TEXT NOT NULL,
			source               TEXT NOT NULL,
			modality             TEXT NOT NULL DEFAULT 'general',
			execution_mode       TEXT NOT NULL DEFAULT 'interactive',
			event_count          INTEGER NOT NULL DEFAULT 0,
			turn_count           INTEGER NOT NULL DEFAULT 0,
			tool_calls           INTEGER NOT NULL DEFAULT 0,
			active_seconds       INTEGER NOT NULL DEFAULT 0,
			semantic_counts_json TEXT NOT NULL DEFAULT '{}',
			built_at             INTEGER NOT NULL,
			feature_version      INTEGER NOT NULL,
			PRIMARY KEY (day, session_hash)
		)`,
		`CREATE TABLE ai_day_feature_details (
			day                  TEXT PRIMARY KEY REFERENCES ai_day_features(day) ON DELETE CASCADE,
			event_count          INTEGER NOT NULL DEFAULT 0,
			active_seconds       INTEGER NOT NULL DEFAULT 0,
			foreground_seconds   INTEGER NOT NULL DEFAULT 0,
			background_seconds   INTEGER NOT NULL DEFAULT 0,
			semantic_counts_json TEXT NOT NULL DEFAULT '{}',
			modality_counts_json TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE ai_day_badge_days (
			day           TEXT NOT NULL REFERENCES ai_day_features(day) ON DELETE CASCADE,
			badge_id      TEXT NOT NULL,
			qualified     INTEGER NOT NULL DEFAULT 0,
			selected      INTEGER NOT NULL DEFAULT 0,
			level         INTEGER NOT NULL DEFAULT 0,
			score         REAL NOT NULL DEFAULT 0,
			evidence_json TEXT NOT NULL DEFAULT '[]',
			PRIMARY KEY (day, badge_id)
		)`,
		`CREATE TABLE ai_day_badge_progress (
			badge_id         TEXT PRIMARY KEY,
			level            INTEGER NOT NULL DEFAULT 0,
			qualified_days   INTEGER NOT NULL DEFAULT 0,
			last_qualified   TEXT NOT NULL DEFAULT '',
			cooldown_until   TEXT NOT NULL DEFAULT '',
			first_unlocked   TEXT NOT NULL DEFAULT '',
			updated_at       INTEGER NOT NULL
		)`,
		`CREATE TABLE ai_day_feedback (
			day                  TEXT PRIMARY KEY,
			verdict              TEXT NOT NULL CHECK (verdict IN ('accurate','inaccurate','corrected')),
			corrected_badge_id   TEXT NOT NULL DEFAULT '',
			semantic_labels_json TEXT NOT NULL DEFAULT '[]',
			updated_at           INTEGER NOT NULL
		)`,
		`CREATE TABLE ai_day_sources (
			source           TEXT PRIMARY KEY,
			enabled          INTEGER NOT NULL DEFAULT 1,
			semantic_enabled INTEGER NOT NULL DEFAULT 1,
			updated_at       INTEGER NOT NULL
		)`,
		`CREATE TABLE ai_day_settings (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// Samples of an agent's rate-limit windows over time. A quota source only
		// ever reports "now", and a bare percentage cannot be acted on: 89% that
		// has not moved in an hour and 89% that climbed thirty points in one are
		// the same reading and opposite situations. Sampling is driven by `atm
		// sync`, so no new timer exists and the resolution is whatever the caller's
		// sync cadence already is.
		//
		// resets_at is stored with each sample because it identifies the refill
		// period the sample belongs to. Rate is only ever computed within one
		// period; differencing across a refill would read the drop back to zero as
		// enormous negative usage.
		`CREATE TABLE quota_history (
			agent          TEXT NOT NULL,
			window_minutes INTEGER NOT NULL,
			used_percent   REAL NOT NULL,
			resets_at      INTEGER NOT NULL DEFAULT 0,
			ts             INTEGER NOT NULL,
			PRIMARY KEY (agent, window_minutes, ts)
		)`,
		`CREATE INDEX idx_quota_history_lookup ON quota_history(agent, window_minutes, ts)`,
		`CREATE TABLE skill_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			ts         INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX idx_skill_events_session ON skill_events(session_id)`,
		`CREATE INDEX idx_skill_events_name_ts ON skill_events(name, ts)`,

		// --- work state: the authored data, this database's only original ---
		// Date-only columns are TEXT 'YYYY-MM-DD'; instants (start_ts, done_ts,
		// bound_at, unbound_at) are INTEGER epoch seconds. That split is
		// deliberate — a due date has no time of day — and the CHECK
		// constraints below pin the text format so the distinction cannot rot.
		`CREATE TABLE todos (
			id                TEXT PRIMARY KEY,
			position          INTEGER NOT NULL,
			title             TEXT NOT NULL,
			description       TEXT NOT NULL DEFAULT '',
			priority          TEXT NOT NULL CHECK (priority IN ('','P0','P1','P2','P3')),
			status            TEXT NOT NULL CHECK (status IN
				('open','in_progress','waiting','review','blocked','done','dropped')),
			project           TEXT NOT NULL DEFAULT '',
			wake_condition    TEXT NOT NULL DEFAULT '',
			review_at         TEXT NOT NULL DEFAULT ''
				CHECK (review_at='' OR review_at GLOB '` + datePattern + `'),
			maintenance_limit INTEGER NOT NULL DEFAULT 0,
			created           TEXT NOT NULL
				CHECK (created='' OR created GLOB '` + datePattern + `'),
			source            TEXT NOT NULL DEFAULT '',
			-- Who filed it: 'me', 'collect', or an agent name. No CHECK: the
			-- agent vocabulary grows with the CLIs ATM reads, and a constraint
			-- here would make adding one a schema migration. Empty means the
			-- todo predates the field; nothing was backfilled, because the old
			-- free-text source cannot say who typed it.
			creator           TEXT NOT NULL DEFAULT '',
			closed            TEXT
				CHECK (closed IS NULL OR closed='' OR closed GLOB '` + datePattern + `'),
			closed_reason     TEXT,
			on_done           TEXT NOT NULL DEFAULT '',
			start_ts          INTEGER,
			done_ts           INTEGER,
			-- NULL means live. An archived todo keeps its row so its ID is never
			-- reused and dependencies, documents, and progress notes can still
			-- name it; loadTodos excludes it from the working set.
			archived_at       INTEGER
		)`,
		`CREATE INDEX idx_todos_status_position ON todos(status, position)`,
		`CREATE INDEX idx_todos_archived ON todos(archived_at)`,
		`CREATE INDEX idx_todos_project_status ON todos(project, status)`,
		`CREATE TABLE todo_tags (
			todo_id  TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			position INTEGER NOT NULL,
			tag      TEXT NOT NULL,
			PRIMARY KEY (todo_id, tag)
		)`,
		// Both sides are real references now that archiving keeps the row:
		// deleting a todo removes the links that point at it, because a link to
		// a todo that no longer exists cannot be acted on.
		`CREATE TABLE todo_dependencies (
			todo_id       TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			position      INTEGER NOT NULL,
			dependency_id TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			PRIMARY KEY (todo_id, dependency_id)
		)`,
		`CREATE TABLE todo_links (
			todo_id  TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			position INTEGER NOT NULL,
			url      TEXT NOT NULL,
			kind     TEXT NOT NULL DEFAULT '',
			title    TEXT NOT NULL DEFAULT '',
			relation TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (todo_id, url)
		)`,
		`CREATE TABLE todo_images (
			todo_id       TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			position      INTEGER NOT NULL,
			stored_name   TEXT NOT NULL,
			original_name TEXT NOT NULL,
			media_type    TEXT NOT NULL,
			size_bytes    INTEGER NOT NULL CHECK (size_bytes >= 0),
			PRIMARY KEY (todo_id, stored_name)
		)`,
		`CREATE INDEX idx_todo_images_todo_position ON todo_images(todo_id, position)`,
		`CREATE TABLE todo_session_bindings (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			todo_id    TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			agent      TEXT NOT NULL DEFAULT '',
			project    TEXT NOT NULL DEFAULT '',
			cwd        TEXT NOT NULL DEFAULT '',
			bound_at   INTEGER NOT NULL,
			unbound_at INTEGER,
			reason     TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_todo_bindings_todo ON todo_session_bindings(todo_id, bound_at)`,
		`CREATE INDEX idx_todo_bindings_session ON todo_session_bindings(session_id, bound_at)`,
		// One open binding per session, enforced by the database rather than by
		// every caller remembering to close the previous one.
		`CREATE UNIQUE INDEX idx_todo_bindings_active_session
			ON todo_session_bindings(session_id) WHERE unbound_at IS NULL`,
		// work_state_meta holds the cross-process write lock row. See
		// acquireWorkWriteLock.
		`CREATE TABLE work_state_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		// Creation retries return the original response, even after the Todo is
		// edited or deleted. This deliberately has no foreign key to todos: the
		// record is written before writeTodos and must survive Todo deletion.
		`CREATE TABLE work_create_idempotency (
			idempotency_key TEXT PRIMARY KEY,
			payload_hash    TEXT NOT NULL,
			todo_id         TEXT NOT NULL,
			result_json     TEXT NOT NULL,
			created_at      INTEGER NOT NULL
		)`,
		// Lifecycle state is authoritative in SQLite, while Todo documents and UI
		// notifications live outside it. Each row is a durable request to bring
		// those projections up to date after the surrounding WorkStateTx commits.
		// Delivery is at-least-once: completed_at is written only after the adapter
		// successfully applies the whole effect.
		`CREATE TABLE work_effect_outbox (
			id              TEXT PRIMARY KEY,
			request_id      TEXT NOT NULL,
			todo_id         TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			kind            TEXT NOT NULL,
			payload_json    TEXT NOT NULL,
			created_at      INTEGER NOT NULL,
			completed_at    INTEGER,
			attempt_count   INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
			last_attempt_at INTEGER,
			last_error      TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_work_effect_outbox_pending
			ON work_effect_outbox(todo_id, created_at, id) WHERE completed_at IS NULL`,
		// Plans are immutable snapshots. The composite primary key gives each Todo
		// its own monotonic revision stream; snapshot_hash makes retry idempotency
		// cheap without normalizing short-lived steps into permanent Todo rows.
		`CREATE TABLE todo_plan_revisions (
			todo_id       TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			revision      INTEGER NOT NULL CHECK (revision >= 1),
			base_revision INTEGER NOT NULL CHECK (base_revision >= 0),
			snapshot_json TEXT NOT NULL,
			snapshot_hash TEXT NOT NULL,
			request_id    TEXT NOT NULL,
			actor_kind    TEXT NOT NULL,
			origin        TEXT NOT NULL,
			session_id    TEXT NOT NULL DEFAULT '',
			binding_id    INTEGER,
			agent         TEXT NOT NULL DEFAULT '',
			created_at    INTEGER NOT NULL,
			PRIMARY KEY (todo_id, revision)
		)`,
		`CREATE INDEX idx_todo_plan_latest ON todo_plan_revisions(todo_id, revision DESC)`,

		// --- outbound action gate: what an agent tried to send, and what you decided ---
		// One row per gated command attempt. This table is the only place in the
		// database whose contents are later handed to exec(), so `atm guard approve`
		// refuses any real_bin that is not a currently installed shim's real binary.
		`CREATE TABLE approvals (
			id           TEXT PRIMARY KEY,
			-- dedup_key identifies the command; id identifies the request. They are
			-- deliberately separate: making the content hash the primary key would
			-- force a later approval of a previously denied command to overwrite the
			-- denial, erasing the one record this feature exists to keep.
			dedup_key    TEXT NOT NULL,
			tool         TEXT NOT NULL,
			rule_id      TEXT NOT NULL DEFAULT '',
			real_bin     TEXT NOT NULL,
			argv         TEXT NOT NULL,
			cwd          TEXT NOT NULL DEFAULT '',
			-- Best effort only: Qoder and Antigravity set none of the session env
			-- vars ATM knows, so this is often empty. cwd is the reliable answer to
			-- "who asked".
			env_agent    TEXT NOT NULL DEFAULT '',
			label        TEXT NOT NULL DEFAULT '',
			preview_target TEXT NOT NULL DEFAULT '',
			preview_title  TEXT NOT NULL DEFAULT '',
			preview_body   TEXT NOT NULL DEFAULT '',
			-- 'running' is terminal for automation. A gate that dies between running
			-- and done leaves no evidence of whether the message went out, and no
			-- lock can recover information that does not exist, so nothing ever
			-- retries such a row.
			status       TEXT NOT NULL CHECK (status IN ('pending','approved','running','done','denied','expired')),
			-- Deferred execution is refused when the body arrived on a pipe, because
			-- stdin cannot be reproduced and a silent wrong-content send is worse
			-- than a refusal.
			stdin_piped  INTEGER NOT NULL DEFAULT 0 CHECK (stdin_piped IN (0,1)),
			-- gate_pid is diagnostic only. gate_deadline is the ownership boundary:
			-- the waiting gate owns execution until then, whoever wins the claim
			-- owns it after. kill(pid,0) is unusable here because the exposure
			-- window is the request's whole lifetime, long enough for PID reuse.
			gate_pid      INTEGER NOT NULL DEFAULT 0,
			gate_deadline INTEGER NOT NULL DEFAULT 0,
			-- How many times an agent re-ran the identical command. A rising count
			-- means the wait budget is wrong, not that the user is indecisive.
			attach_count INTEGER NOT NULL DEFAULT 1,
			requested_at INTEGER NOT NULL,
			expires_at   INTEGER NOT NULL,
			decided_at   INTEGER,
			decided_by   TEXT NOT NULL DEFAULT '',
			reason       TEXT NOT NULL DEFAULT '',
			ran_by       TEXT NOT NULL DEFAULT '',
			exit_code    INTEGER,
			output       TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_approvals_status_requested ON approvals(status, requested_at DESC)`,
		`CREATE INDEX idx_approvals_dedup ON approvals(dedup_key, requested_at DESC)`,
		// At most one pending request per identical command. A retrying agent
		// attaches to the existing row instead of raising a second banner.
		`CREATE UNIQUE INDEX idx_approvals_pending_dedup ON approvals(dedup_key)
			WHERE status='pending'`,

		// --- automatic collection: connector input, decisions and audit ---
		// Sources are user-authored configuration. Runs and items are an audit
		// ledger: deleting a source must not erase why a Todo was created, so the
		// historical tables deliberately do not foreign-key source_id.
		`CREATE TABLE collection_sources (
			id          TEXT PRIMARY KEY,
			connector   TEXT NOT NULL,
			kind        TEXT NOT NULL,
			external_id TEXT NOT NULL,
			name        TEXT NOT NULL DEFAULT '',
			project     TEXT NOT NULL DEFAULT '',
			exclude_pattern TEXT NOT NULL DEFAULT '',
			-- What this source should be watched for, in the user's own words
			-- ("关注 MR、需求"). Trusted input: it is passed to the classifier as
			-- instruction, unlike the chat itself. exclude_pattern is the blunt
			-- inverse (drop anything containing these keywords, no model call).
			instruction     TEXT NOT NULL DEFAULT '',
			-- Default knowledge destination for an explicitly saved conclusion and
			-- for optional manual digests. Empty falls back to the configured default.
			knowledge_collection TEXT NOT NULL DEFAULT '',
			-- What this source is allowed to produce. 'tasks' may reach the Todo
			-- list; 'observe' may not, however concrete the chat looks — the
			-- restriction is here rather than in the prompt so a noisy group cannot
			-- talk the classifier into filing work for other people.
			strategy        TEXT NOT NULL DEFAULT 'tasks'
				CHECK (strategy IN ('tasks','observe')),
			-- How much of a fetched window one decision covers. 'window' groups
			-- messages by "same conversation, gaps under fifteen minutes", which
			-- is right for chat: a request and its follow-up clarifications are
			-- one piece of work. A notification feed is the opposite — every push
			-- is a separate event, and grouping them means all but one are lost,
			-- because a batch yields exactly one decision. 'message' decides on
			-- each message, still reading its window as context.
			decision_unit   TEXT NOT NULL DEFAULT 'window'
				CHECK (decision_unit IN ('window','message')),
			interval_minutes INTEGER NOT NULL DEFAULT 5
				CHECK (interval_minutes BETWEEN 1 AND 1440),
			priority    TEXT NOT NULL DEFAULT 'P2'
				CHECK (priority IN ('P0','P1','P2','P3')),
			enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
			-- Whether this source's new results stay out of desktop notifications.
			-- Only the banner: muted results still collect, still count as unread
			-- and still raise the sidebar and menubar badges. Deliberately absent
			-- from the upsert's conflict update so editing a source cannot undo it.
			muted       INTEGER NOT NULL DEFAULT 0 CHECK (muted IN (0,1)),
			created_at  INTEGER NOT NULL,
			updated_at  INTEGER NOT NULL,
			UNIQUE (connector, kind, external_id)
		)`,
		`CREATE INDEX idx_collection_sources_connector ON collection_sources(connector,enabled,name)`,
		`CREATE TABLE collection_checkpoints (
			source_id   TEXT PRIMARY KEY REFERENCES collection_sources(id) ON DELETE CASCADE,
			cursor_time INTEGER NOT NULL DEFAULT 0,
			cursor      TEXT NOT NULL DEFAULT '',
			updated_at  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE collection_runs (
			id             TEXT PRIMARY KEY,
			connector      TEXT NOT NULL,
			source_id      TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL CHECK (status IN ('running','succeeded','failed')),
			started_at     INTEGER NOT NULL,
			finished_at    INTEGER NOT NULL DEFAULT 0,
			fetched_count  INTEGER NOT NULL DEFAULT 0,
			analyzed_count INTEGER NOT NULL DEFAULT 0,
			created_count  INTEGER NOT NULL DEFAULT 0,
			appended_count INTEGER NOT NULL DEFAULT 0,
			insight_count  INTEGER NOT NULL DEFAULT 0,
			ignored_count  INTEGER NOT NULL DEFAULT 0,
			failed_count   INTEGER NOT NULL DEFAULT 0,
			error          TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_collection_runs_started ON collection_runs(started_at DESC)`,
		`CREATE TABLE collection_items (
			id              TEXT PRIMARY KEY,
			source_id       TEXT NOT NULL,
			connector       TEXT NOT NULL,
			conversation_id TEXT NOT NULL DEFAULT '',
			fingerprint     TEXT NOT NULL,
			message_ids     TEXT NOT NULL DEFAULT '[]',
			sender          TEXT NOT NULL DEFAULT '',
			occurred_at     INTEGER NOT NULL DEFAULT 0,
			raw_context     TEXT NOT NULL DEFAULT '',
			-- Where this batch ended up. 'insight' is a conclusion worth keeping,
			-- but not work; it stays here until the user explicitly saves it to
			-- knowledge. 'ignore' means genuine noise.
			action          TEXT NOT NULL DEFAULT 'pending'
				CHECK (action IN ('pending','create','append','insight','ignore','failed','reverted')),
			-- What an on-demand analysis decided but has not carried out: '' means
			-- nothing is waiting, 'create'/'append' means a person still has to
			-- confirm it. An insight needs no confirmation — it touches nothing a
			-- person owns — so it is applied directly and never proposed.
			proposed_action TEXT NOT NULL DEFAULT ''
				CHECK (proposed_action IN ('','create','append')),
			title           TEXT NOT NULL DEFAULT '',
			summary         TEXT NOT NULL DEFAULT '',
			item_type       TEXT NOT NULL DEFAULT '',
			project         TEXT NOT NULL DEFAULT '',
			priority        TEXT NOT NULL DEFAULT '',
			reason          TEXT NOT NULL DEFAULT '',
			confidence      REAL NOT NULL DEFAULT 0,
			-- An insight first lives as this record's conclusion. These fields stay
			-- empty until the user explicitly saves it to central knowledge.
			knowledge_document_id TEXT NOT NULL DEFAULT '',
			knowledge_collection  TEXT NOT NULL DEFAULT '',
			todo_id         TEXT REFERENCES todos(id) ON DELETE SET NULL,
			status          TEXT NOT NULL DEFAULT 'pending'
				CHECK (status IN ('pending','processed','failed')),
			-- Read state belongs to the collection result, not to the Todo it may
			-- have created. Zero means the result has not been acknowledged.
			read_at         INTEGER NOT NULL DEFAULT 0,
			-- A manually settled result stays in the audit ledger and handled-message
			-- set, but leaves the active collection list. Zero means active.
			archived_at     INTEGER NOT NULL DEFAULT 0,
			-- How many times processing this batch has been tried. A failed item
			-- is deliberately left out of the handled set so the next run picks it
			-- up again, which is right for a connector that was briefly down and
			-- wrong for a batch that fails the same way every minute: without a
			-- ceiling the second case spends a model call per run forever and
			-- keeps the checkpoint from ever advancing. Reaching
			-- MaxCollectionAttempts stops the automatic retry and leaves the item
			-- to an explicit reprocess.
			attempts        INTEGER NOT NULL DEFAULT 0,
			error           TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			UNIQUE (connector, fingerprint)
		)`,
		`CREATE INDEX idx_collection_items_updated ON collection_items(updated_at DESC)`,
		`CREATE INDEX idx_collection_items_source ON collection_items(source_id,occurred_at DESC)`,
		`CREATE INDEX idx_collection_items_todo ON collection_items(todo_id)`,
		`CREATE INDEX idx_collection_items_unread ON collection_items(read_at,updated_at DESC)`,
		`CREATE INDEX idx_collection_items_archived ON collection_items(archived_at,updated_at DESC)`,

		// One row per source per day: the knowledge document that day's insights
		// were distilled into. A day's digest is a function of every insight that
		// day, so regenerating it rewrites the same document rather than adding a
		// second one. covered_through is the watermark of what the current body
		// already accounts for — insights past it are what makes a digest due.
		`CREATE TABLE collection_digests (
			source_id       TEXT NOT NULL,
			digest_date     TEXT NOT NULL CHECK (digest_date GLOB '` + datePattern + `'),
			document_id     TEXT NOT NULL,
			collection      TEXT NOT NULL DEFAULT '',
			title           TEXT NOT NULL DEFAULT '',
			item_count      INTEGER NOT NULL DEFAULT 0,
			covered_through INTEGER NOT NULL DEFAULT 0,
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			PRIMARY KEY (source_id, digest_date)
		)`,
		`CREATE INDEX idx_collection_digests_updated ON collection_digests(updated_at DESC)`,

		// The chat as it was said, kept so reading and searching a conversation
		// works without a connector. collection_items.raw_context is not a substitute: it
		// holds only the lines the classifier judged worth acting on. A chat
		// message never changes, so its identity is its whole key and a repeated
		// sync is a no-op. Rows older than the retention window are pruned after
		// each sync; see PruneCollectionMessages.
		`CREATE TABLE collection_messages (
			connector         TEXT NOT NULL,
			conversation_id   TEXT NOT NULL,
			message_id        TEXT NOT NULL,
			-- A conversation can be read without ever being added as a source, so
			-- source_id may be empty and conversation_name carries the label.
			source_id         TEXT NOT NULL DEFAULT '',
			conversation_name TEXT NOT NULL DEFAULT '',
			sender            TEXT NOT NULL DEFAULT '',
			created_at        INTEGER NOT NULL DEFAULT 0,
			content           TEXT NOT NULL DEFAULT '',
			synced_at         INTEGER NOT NULL,
			PRIMARY KEY (connector, conversation_id, message_id)
		)`,
		`CREATE INDEX idx_collection_messages_time ON collection_messages(conversation_id, created_at DESC)`,
		`CREATE INDEX idx_collection_messages_created ON collection_messages(created_at DESC)`,

		// --- shared memory ---
		// An event log, not a state table: "when did I forget this" is part of the
		// record. The effective set is every remember/supersede event that no later
		// event targets, which is a single query — see EffectiveMemories.
		`CREATE TABLE memory_events (
			id         TEXT PRIMARY KEY,
			op         TEXT NOT NULL CHECK (op IN ('remember','supersede','forget')),
			scope      TEXT NOT NULL,
			content    TEXT NOT NULL DEFAULT '',
			-- remember creates a memory; supersede and forget act on one that
			-- exists. This is the one reference in either of these two features
			-- that the database can actually enforce: both sides are rows.
			target_id  TEXT REFERENCES memory_events(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			CHECK ((op = 'remember') = (target_id IS NULL)),
			CHECK ((op = 'forget') = (content = ''))
		)`,
		`CREATE INDEX idx_memory_events_scope ON memory_events(scope, created_at)`,
		// A memory can be acted on once: superseding or forgetting it takes it out
		// of force, so a second event targeting it has nothing to act on. Two
		// processes that both check "is it still in force" and then both write
		// would otherwise leave two live replacements of the same memory.
		`CREATE UNIQUE INDEX idx_memory_events_target ON memory_events(target_id)`,
		`CREATE TABLE memory_event_tags (
			event_id TEXT NOT NULL REFERENCES memory_events(id) ON DELETE CASCADE,
			position INTEGER NOT NULL,
			tag      TEXT NOT NULL,
			PRIMARY KEY (event_id, tag)
		)`,
		`CREATE TABLE memory_event_metadata (
			event_id TEXT NOT NULL REFERENCES memory_events(id) ON DELETE CASCADE,
			key      TEXT NOT NULL,
			value    TEXT NOT NULL,
			PRIMARY KEY (event_id, key)
		)`,

		// --- knowledge retrieval feedback ---
		// document_id names a markdown file and session_id names an agent session.
		// Neither can be a foreign key: the documents live outside the database and
		// sessions only appear here once `atm sync` has indexed them, so a real
		// reference can predate its referent. `atm knowledge doctor` reports rows
		// whose document is gone instead of skipping them silently.
		`CREATE TABLE knowledge_feedback (
			id          TEXT PRIMARY KEY,
			document_id TEXT NOT NULL,
			session_id  TEXT NOT NULL,
			query       TEXT NOT NULL DEFAULT '',
			outcome     TEXT NOT NULL
				CHECK (outcome IN ('retrieved','adopted','corrected','rejected')),
			note        TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL
		)`,
		// The log this replaces was keyed by hand at read time: one retrieval per
		// (document, session, query) and one verdict per (document, session), last
		// write winning. Those keys are now the schema's, so the dedup happens on
		// write instead of in a full scan on every read.
		`CREATE UNIQUE INDEX idx_knowledge_feedback_retrieval
			ON knowledge_feedback(document_id, session_id, query) WHERE outcome = 'retrieved'`,
		`CREATE UNIQUE INDEX idx_knowledge_feedback_verdict
			ON knowledge_feedback(document_id, session_id) WHERE outcome <> 'retrieved'`,
		`CREATE INDEX idx_knowledge_feedback_document ON knowledge_feedback(document_id)`,

		// One review per session, which the primary key now says outright: the
		// append log it replaces reduced to the same thing while reading itself.
		// session_id has no foreign key for the same reason as above.
		`CREATE TABLE session_reviews (
			session_id  TEXT PRIMARY KEY,
			outcome     TEXT NOT NULL CHECK (outcome IN ('none','memory','knowledge','mixed')),
			note        TEXT NOT NULL DEFAULT '',
			reviewed_at TEXT NOT NULL
		)`,

		`DELETE FROM schema_version`,
		fmt.Sprintf(`INSERT INTO schema_version (version) VALUES (%d)`, SchemaVersion),
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}
	return InstallWorkspaceChangeTracking(tx)
}
