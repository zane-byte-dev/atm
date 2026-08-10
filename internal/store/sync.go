package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
)

func SyncAll(db *sql.DB) (int, error) {
	return runTrackedSync(db, SyncScopeAll, func() (int, error) {
		total := 0
		for _, a := range parser.All() {
			n, err := runTrackedSync(db, a.Name(), func() (int, error) {
				return syncAgent(db, a)
			})
			if err != nil {
				return total, err
			}
			total += n
		}
		return total, RepriceUsage(db)
	})
}

func SyncAgent(db *sql.DB, agent string) (int, error) {
	a := parser.Get(agent)
	if a == nil {
		return 0, fmt.Errorf("unknown agent: %s", agent)
	}
	return runTrackedSync(db, agent, func() (int, error) {
		n, err := syncAgent(db, a)
		if err != nil {
			return n, err
		}
		// Rates are global, so a single-agent sync reprices everything too. See
		// RepriceUsage for why stored cost cannot be left to the insert path.
		return n, RepriceUsage(db)
	})
}

// sourceVersioner lets an agent report a composite change fingerprint when the
// discovered path is only one of several files that make up a session (e.g. Grok
// chat_history + updates + summary). When ok is false, sync falls back to
// Stat(path).
type sourceVersioner interface {
	SourceVersion(path string) (mtime, size int64, ok bool)
}

func syncAgent(db *sql.DB, a parser.Agent) (int, error) {
	files := a.Discover()
	agent := a.Name()

	synced := 0
	onDisk := map[string]bool{}
	versioner, _ := a.(sourceVersioner)

	for _, fp := range files {
		onDisk[fp] = true
		var mtime, size int64
		virtual := strings.Contains(fp, "://")
		if virtual {
			// Virtual path (e.g. qoder://session-id): use 0 mtime to always re-parse
			mtime = 0
			size = 0
		} else {
			info, err := os.Stat(fp)
			if err != nil {
				continue
			}
			mtime = info.ModTime().Unix()
			size = info.Size()
			if versioner != nil {
				if vm, vs, ok := versioner.SourceVersion(fp); ok {
					mtime, size = vm, vs
				}
			}
		}

		var oldMtime, oldSize, oldOffset int64
		row := db.QueryRow("SELECT mtime_unix, size_bytes, offset_bytes FROM sync_state WHERE file_path = ?", fp)
		known := row.Scan(&oldMtime, &oldSize, &oldOffset) == nil
		if known && !virtual && oldMtime == mtime && oldSize == size {
			continue
		}

		// Incremental append: append-only transcripts that only grew can be parsed
		// from the previous offset instead of re-read in full. Each agent decides
		// whether it supports this (ParseAppend returns nil otherwise). The
		// boundary guard rejects offsets that no longer land on a record boundary,
		// which indicates the file was rewritten rather than appended.
		if known && !virtual && oldOffset > 0 && size > oldOffset &&
			parser.OffsetOnRecordBoundary(fp, oldOffset) {
			if p := a.ParseAppend(fp, oldOffset); p != nil {
				if err := appendSession(db, p, fp, agent, mtime, size); err != nil {
					return synced, fmt.Errorf("append %s file %s: %w", agent, fp, err)
				}
				synced++
				continue
			}
		}

		parsed := a.ParseFile(fp)
		if parsed == nil {
			continue
		}
		if err := upsertSession(db, parsed, fp, agent, mtime, size); err != nil {
			return synced, fmt.Errorf("sync %s file %s: %w", agent, fp, err)
		}
		synced++
	}

	if err := forgetRemovedSources(db, agent, onDisk); err != nil {
		return synced, fmt.Errorf("forget removed %s sources: %w", agent, err)
	}
	return synced, nil
}

func upsertSession(db *sql.DB, p *parser.ParsedFile, fp, agent string, mtime, size int64) error {
	p.Project = config.CanonicalProject(p.Project)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// A full parse replaces the session outright, so both of its identities have
	// to be cleared first. By path, for the usual case of a file being re-read.
	// By id, because a session can arrive under a *new* path: the agents derive
	// their session ids from the transcript filename, so moving a checkout or
	// repointing a source path in ~/.atm/config.json re-presents the same ids
	// elsewhere. Without this the plain INSERT below fails on sessions.id, and
	// since that error aborts syncAgent before it reaches forgetRemovedSources,
	// the agent's sync stays wedged on every retry.
	if err := execTx(tx, "DELETE FROM sessions WHERE file_path = ? OR id = ?", fp, p.SessionID); err != nil {
		return err
	}
	if err := execTx(tx, "DELETE FROM sync_state WHERE file_path = ?", fp); err != nil {
		return err
	}

	lastTS := p.LastTS
	if lastTS == 0 {
		lastTS = mtime
	}
	if _, err = tx.Exec(`INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SessionID, p.ShortID, p.Agent, p.Project, fp, p.CreatedAt, p.CreatedTS, p.Summary, lastTS); err != nil {
		return err
	}
	if err := linkTaskRun(tx, p.SessionID, p.Agent, p.Project, p.CreatedTS); err != nil {
		return err
	}
	seq := 0
	if len(p.Messages) > 0 {
		for _, m := range p.Messages {
			if err := execTx(tx, "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)", p.SessionID, seq, m.Role, m.Content, m.TS); err != nil {
				return err
			}
			seq++
		}
	} else {
		for i, input := range p.Inputs {
			if err := execTx(tx, "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, 'user', ?, ?)",
				p.SessionID, seq, input.Content, input.TS); err != nil {
				return err
			}
			seq++
			if i < len(p.Outputs) {
				if err := execTx(tx, "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, 'assistant', ?, ?)",
					p.SessionID, seq, p.Outputs[i].Content, p.Outputs[i].TS); err != nil {
					return err
				}
				seq++
			}
		}
	}

	for name, count := range p.Tools {
		if err := execTx(tx, "INSERT OR REPLACE INTO tools (session_id, name, count) VALUES (?, ?, ?)",
			p.SessionID, name, count); err != nil {
			return err
		}
	}
	if err := insertSkillEvents(tx, p); err != nil {
		return err
	}

	if err := syncUsage(tx, p, false); err != nil {
		return err
	}

	if err := execTx(tx, `INSERT OR REPLACE INTO sync_state (file_path, agent, mtime_unix, size_bytes, offset_bytes) VALUES (?, ?, ?, ?, ?)`,
		fp, agent, mtime, size, p.EndOffset); err != nil {
		return err
	}

	return tx.Commit()
}

// appendSession inserts only the newly parsed messages/tools/usage onto an
// existing session without rewriting prior rows.
func appendSession(db *sql.DB, p *parser.ParsedFile, fp, agent string, mtime, size int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The write lock has to be held before MAX(seq) is read. A deferred
	// transaction that reads first takes a snapshot, and SQLite then rejects its
	// write with SQLITE_BUSY_SNAPSHOT if another sync committed in between —
	// which aborts the whole sync rather than waiting. Taking the lock up front
	// turns that into the second sync queueing behind the first.
	if err := acquireWorkWriteLock(tx); err != nil {
		return err
	}

	var seq int
	if err := tx.QueryRow("SELECT COALESCE(MAX(seq), -1) + 1 FROM messages WHERE session_id = ?", p.SessionID).Scan(&seq); err != nil {
		return err
	}

	if len(p.Messages) > 0 {
		for _, m := range p.Messages {
			if err := execTx(tx, "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)", p.SessionID, seq, m.Role, m.Content, m.TS); err != nil {
				return err
			}
			seq++
		}
	} else {
		for i, input := range p.Inputs {
			if err := execTx(tx, "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, 'user', ?, ?)",
				p.SessionID, seq, input.Content, input.TS); err != nil {
				return err
			}
			seq++
			if i < len(p.Outputs) {
				if err := execTx(tx, "INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, 'assistant', ?, ?)",
					p.SessionID, seq, p.Outputs[i].Content, p.Outputs[i].TS); err != nil {
					return err
				}
				seq++
			}
		}
	}

	for name, count := range p.Tools {
		if err := execTx(tx, `INSERT INTO tools (session_id, name, count) VALUES (?, ?, ?)
			ON CONFLICT(session_id, name) DO UPDATE SET count = count + excluded.count`,
			p.SessionID, name, count); err != nil {
			return err
		}
	}
	if err := insertSkillEvents(tx, p); err != nil {
		return err
	}

	if p.LastTS != 0 {
		if err := execTx(tx, "UPDATE sessions SET last_ts = ? WHERE id = ?", p.LastTS, p.SessionID); err != nil {
			return err
		}
	}

	if err := syncUsage(tx, p, true); err != nil {
		return err
	}

	if err := execTx(tx, `UPDATE sync_state SET mtime_unix = ?, size_bytes = ?, offset_bytes = ? WHERE file_path = ?`,
		mtime, size, p.EndOffset, fp); err != nil {
		return err
	}

	return tx.Commit()
}

// syncUsage records the file's token usage: the individual requests, then the
// session's rollup.
//
// When the parser produced per-request events, the rollup is recomputed from the
// rows that are actually in the table rather than from the parser's own running
// totals. The unique fingerprint index rejects requests another transcript
// already reported, and a rollup carried in from the parser would not know that
// — it would keep reporting the duplicates the events no longer contain.
//
// add distinguishes the two callers: an append parse describes only the new tail
// of a file, so a parser-supplied rollup has to be added to what is already
// stored, where a full parse replaces it. Recomputing from events needs no such
// distinction, since the events of the whole session are already there.
func syncUsage(tx *sql.Tx, p *parser.ParsedFile, add bool) error {
	if err := insertUsageEvents(tx, p); err != nil {
		return err
	}
	if len(p.UsageEvents) > 0 {
		return rollupUsageFromEvents(tx, p.SessionID, p.Usage.Model)
	}
	if !hasUsage(p.Usage) {
		return nil
	}
	cost := CalcCost(p.Usage.Model, p.Usage.InputTokens, p.Usage.OutputTokens,
		p.Usage.CacheCreateTokens, p.Usage.CacheReadTokens)
	query := `INSERT OR REPLACE INTO usage (session_id, model, input_tokens, output_tokens,
		cache_create_tokens, cache_read_tokens, cost_usd, request_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if add {
		query = `INSERT INTO usage (session_id, model, input_tokens, output_tokens,
			cache_create_tokens, cache_read_tokens, cost_usd, request_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET model = excluded.model,
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			cache_create_tokens = cache_create_tokens + excluded.cache_create_tokens,
			cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
			cost_usd = cost_usd + excluded.cost_usd,
			request_count = request_count + excluded.request_count`
	}
	return execTx(tx, query, p.SessionID, p.Usage.Model, p.Usage.InputTokens, p.Usage.OutputTokens,
		p.Usage.CacheCreateTokens, p.Usage.CacheReadTokens, cost, p.Usage.RequestCount)
}

// rollupUsageFromEvents rebuilds one session's usage row from its events. The
// row is dropped first so a session whose every event was a duplicate of another
// transcript's ends up with no usage row at all, rather than keeping a stale one.
func rollupUsageFromEvents(tx *sql.Tx, sessionID, model string) error {
	if err := execTx(tx, `DELETE FROM usage WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	return execTx(tx, `INSERT INTO usage (session_id, model, input_tokens, output_tokens,
		cache_create_tokens, cache_read_tokens, cost_usd, request_count)
		SELECT session_id, ?, SUM(input_tokens), SUM(output_tokens), SUM(cache_create_tokens),
			SUM(cache_read_tokens), SUM(cost_usd), SUM(request_count)
		FROM usage_events WHERE session_id = ? GROUP BY session_id`, model, sessionID)
}

// insertUsageEvents inserts the parsed requests, letting the fingerprint index
// drop the ones another transcript already accounted for.
func insertUsageEvents(tx *sql.Tx, p *parser.ParsedFile) error {
	for _, u := range p.UsageEvents {
		cost := CalcCost(u.Model, u.InputTokens, u.OutputTokens, u.CacheCreateTokens, u.CacheReadTokens)
		if err := execTx(tx, `INSERT OR IGNORE INTO usage_events (session_id, model, ts, input_tokens,
			output_tokens, cache_create_tokens, cache_read_tokens, cost_usd, fingerprint, request_count,
			duration_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.SessionID, u.Model, u.TS, u.InputTokens, u.OutputTokens,
			u.CacheCreateTokens, u.CacheReadTokens, cost, u.Fingerprint, parser.EventRequestCount(u),
			u.DurationMS); err != nil {
			return err
		}
	}
	return nil
}

func insertSkillEvents(tx *sql.Tx, p *parser.ParsedFile) error {
	for _, event := range p.Skills {
		if event.Name == "" {
			continue
		}
		if err := execTx(tx, `INSERT INTO skill_events (session_id, name, ts) VALUES (?, ?, ?)`,
			p.SessionID, event.Name, event.TS); err != nil {
			return err
		}
	}
	return nil
}

func hasUsage(u parser.Usage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheCreateTokens > 0 || u.CacheReadTokens > 0
}

func execTx(tx *sql.Tx, query string, args ...any) error {
	_, err := tx.Exec(query, args...)
	return err
}

// forgetRemovedSources drops the sync bookkeeping for transcripts that are no
// longer on disk, and deliberately keeps the sessions they produced.
//
// The agents rotate their own logs — Claude Code prunes ~/.claude/projects after
// a month — so treating a missing file as a deleted session would make ATM's
// history shrink on its own: spend that happened would stop being counted, and
// searchable transcripts would disappear. The mirror outliving its source is the
// point. `sessions` has ON DELETE CASCADE children (messages, tools, usage,
// usage_events, skill_events), so deleting the row here is what used to take the
// token history with it.
//
// Dropping the sync_state row is what marks the session as retained history: it
// is that table's primary key, and upsertSession rewrites it in the same
// transaction that deletes it, so a sessions row whose file_path has no
// sync_state row means exactly one thing — the transcript is gone from disk.
// GetRetainedSessionCounts reads that, and doctor reports it.
//
// A file that comes back is parsed in full rather than resumed from an offset,
// since offset_bytes went with the sync_state row. upsertSession then deletes
// the retained session before reinserting it, so messages restart at seq 0
// instead of colliding with idx_messages_session_seq.
func forgetRemovedSources(db *sql.DB, agent string, onDisk map[string]bool) error {
	rows, err := db.Query("SELECT file_path FROM sync_state WHERE agent = ?", agent)
	if err != nil {
		return err
	}
	defer rows.Close()
	var removed []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return err
		}
		if !onDisk[fp] {
			removed = append(removed, fp)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, fp := range removed {
		if _, err := db.Exec("DELETE FROM sync_state WHERE file_path = ?", fp); err != nil {
			return err
		}
	}
	return nil
}
