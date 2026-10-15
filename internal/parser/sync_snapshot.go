package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

type sqliteReader interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func VirtualSourcePath(agent string) string {
	switch agent {
	case "qoder":
		return config.QoderDB
	case "qoderwork":
		return config.QoderWorkDB
	}
	return ""
}

// SQLiteSourceVersion is a cheap gate before opening the source. Include WAL
// identity and nanosecond timestamps, not just the checkpointed database file.
// Content fingerprints below remain authoritative when this gate changes.
func SQLiteSourceVersion(path string) (int64, int64, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	_ = enc.Encode(path)
	_ = enc.Encode(config.ProjectAliases)
	for _, file := range []string{path, path + "-wal", path + "-journal"} {
		info, err := os.Stat(file)
		if os.IsNotExist(err) && file != path {
			_ = enc.Encode("absent")
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		_ = enc.Encode([]int64{info.ModTime().UnixNano(), info.Size()})
		f, err := os.Open(file)
		if err != nil {
			return 0, 0, err
		}
		_, err = io.CopyN(h, f, 100)
		f.Close()
		if err != nil && err != io.EOF {
			return 0, 0, err
		}
	}
	sum := h.Sum(nil)
	return int64(binary.BigEndian.Uint64(sum[:8])), int64(binary.BigEndian.Uint64(sum[8:16])), nil
}

// SyncSnapshot holds one WAL-aware source connection and read transaction for
// discovery, fingerprints and parsing. A fingerprint can never describe newer
// data than the transcript saved with it. No immutable fallback: stale WAL data
// must not be recorded as current.
type SyncSnapshot struct {
	db    *sql.DB
	tx    *sql.Tx
	agent string
	ctx   context.Context
}

func OpenSyncSnapshot(ctx context.Context, agent, path string) (*SyncSnapshot, error) {
	base := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := sql.Open("sqlite", base+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(1000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &SyncSnapshot{db: db, tx: tx, agent: agent, ctx: ctx}, nil
}

func (s *SyncSnapshot) Close() { _ = s.tx.Rollback(); _ = s.db.Close() }

func (s *SyncSnapshot) Discover() ([]string, error) {
	query := ""
	prefix := "qoder://"
	if s.agent == "qoderwork" {
		query = "SELECT sc.id FROM sub_chats sc JOIN chats c ON c.id=sc.chat_id WHERE c.deleted_at IS NULL ORDER BY sc.id"
		prefix = "qoderwork://"
	} else {
		query = "SELECT session_id FROM chat_session" + qoderRootOnly(s.tx)
	}
	rows, err := s.tx.QueryContext(s.ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		paths = append(paths, prefix+id)
	}
	return paths, rows.Err()
}

func (s *SyncSnapshot) Version(path string) (int64, int64, error) {
	id := strings.TrimPrefix(path, s.agent+"://")
	queries := []string{}
	if s.agent == "qoderwork" {
		queries = []string{
			"SELECT sc.*, c.*, p.* FROM sub_chats sc JOIN chats c ON c.id=sc.chat_id JOIN projects p ON p.id=c.project_id WHERE sc.id=?",
			"SELECT * FROM messages WHERE sub_chat_id=? ORDER BY sequence, id",
		}
	} else {
		ids := "SELECT session_id FROM chat_session WHERE session_id=?"
		if qoderHasColumn(s.tx, "chat_session", "parent_session_id") {
			ids = `WITH RECURSIVE tree(id) AS (SELECT session_id FROM chat_session WHERE session_id=? UNION SELECT c.session_id FROM chat_session c JOIN tree t ON c.parent_session_id=t.id) SELECT id FROM tree`
		}
		queries = []string{
			"SELECT * FROM chat_session WHERE session_id IN (" + ids + ") ORDER BY session_id",
			"SELECT * FROM chat_message WHERE session_id IN (" + ids + ") ORDER BY session_id, gmt_create, id",
		}
	}
	h := sha256.New()
	enc := json.NewEncoder(h)
	_ = enc.Encode(config.ProjectAliases)
	for _, query := range queries {
		rows, err := s.tx.QueryContext(s.ctx, query, id)
		if err != nil {
			return 0, 0, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return 0, 0, err
		}
		_ = enc.Encode(cols)
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return 0, 0, err
			}
			if err := enc.Encode(values); err != nil {
				rows.Close()
				return 0, 0, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, 0, err
		}
	}
	sum := h.Sum(nil)
	return int64(binary.BigEndian.Uint64(sum[:8])), int64(binary.BigEndian.Uint64(sum[8:16])), nil
}

func (s *SyncSnapshot) Parse(path string) (*ParsedFile, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	var parsed *ParsedFile
	if s.agent == "qoderwork" {
		parsed = qoderWorkParse(s.tx, path, true)
	} else {
		parsed = qoderParse(s.tx, path, true)
	}
	if parsed == nil {
		return nil, fmt.Errorf("parse %s from source snapshot failed", path)
	}
	return parsed, s.ctx.Err()
}
