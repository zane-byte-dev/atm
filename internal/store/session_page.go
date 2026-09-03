package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/parser"
)

// sessionVisiblePageSQL computes only turn identities and a count. In schema
// 54 there is no persisted visible-turn flag: visibility depends on the same
// wrapper removal as parser.VisibleUserText, including final-answer precedence.
// Keep that count in SQLite; no unselected message body crosses the query
// boundary into transcript formatting. The ordinary-text fast path also avoids
// copying long answers through every recursive wrapper-removal step.
//
// The differential ShowPage tests exercise this predicate against the existing
// parser so changes to its request markers/prefixes/tags cannot silently change
// page totals. Unicode whitespace is Go's unicode.IsSpace set used by TrimSpace.
const sessionVisiblePageSQL = `WITH RECURSIVE
spaces(chars) AS (VALUES(char(9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288))),
prefixes(prefix) AS (VALUES
 ('<recommended_plugins>'),('# AGENTS.md instructions'),('<permissions instructions>'),
 ('<app-context>'),('<environment_context>'),('<skills_instructions>'),('<image name='),('</image>'),
 ('Message Type: NEW_TASK'),('Message Type: MESSAGE'),('Message Type: FINAL_ANSWER'),
 ('Within the root conversation'),('You are an agent in a team of agents collaborating'),
 ('Some conversation entries were omitted.')),
rules(phase,opener,closer) AS (VALUES
 (0,'## My request for Codex:',''),(1,'## My request:',''),(2,'# My request:',''),(3,'',''),
 (4,'<ide_opened_file>','</ide_opened_file>'),(5,'<ide_selection>','</ide_selection>'),
 (6,'<system-reminder>','</system-reminder>'),(7,'<system_context>','</system_context>')),
effective AS MATERIALIZED (
 SELECT seq,role,kind FROM messages,spaces
 WHERE session_id=? AND scope='local' AND kind!='control' AND trim(content,chars)!=''
 AND (role='assistant' OR (role='user' AND kind='conversation'))
),
numbered AS MATERIALIZED (
 SELECT seq,role,kind,
 SUM(CASE WHEN role='user' THEN 1 ELSE 0 END) OVER (ORDER BY seq ROWS UNBOUNDED PRECEDING)
 + COALESCE((SELECT CASE WHEN role='assistant' THEN 1 ELSE 0 END FROM effective ORDER BY seq LIMIT 1),0) AS turn_no
 FROM effective
),
turn_bounds AS (SELECT turn_no,MIN(seq) first_seq,MAX(seq) last_seq FROM numbered GROUP BY turn_no),
plain_questions AS MATERIALIZED (
 SELECT n.turn_no FROM numbered n JOIN messages m ON m.session_id=? AND m.seq=n.seq,spaces
 WHERE n.role='user' AND instr(m.content,'<')=0 AND instr(m.content,'My request')=0
 AND NOT EXISTS(SELECT 1 FROM prefixes WHERE substr(ltrim(m.content,chars),1,length(prefix))=prefix)
),
fields AS (
 SELECT n.turn_no,trim(m.content,chars) body FROM numbered n JOIN messages m ON m.session_id=? AND m.seq=n.seq,spaces
 WHERE n.role='user' OR (n.kind='progress' AND NOT EXISTS(SELECT 1 FROM plain_questions p WHERE p.turn_no=n.turn_no))
 UNION ALL
 SELECT n.turn_no,
 CASE WHEN COUNT(*) FILTER (WHERE n.kind='final')>0
 THEN group_concat(trim(m.content,chars),char(10)||char(10) ORDER BY n.seq) FILTER (WHERE n.kind='final')
 ELSE group_concat(trim(m.content,chars),char(10)||char(10) ORDER BY n.seq) FILTER (WHERE n.kind NOT IN ('final','progress')) END
 FROM numbered n JOIN messages m ON m.session_id=? AND m.seq=n.seq,spaces
 WHERE n.role='assistant' AND NOT EXISTS(SELECT 1 FROM plain_questions p WHERE p.turn_no=n.turn_no) GROUP BY n.turn_no
),
clean(turn_no,phase,body) AS (
 SELECT turn_no,
 CASE WHEN instr(body,'<')=0 AND instr(body,'My request')=0
 AND NOT EXISTS(SELECT 1 FROM prefixes WHERE substr(ltrim(body,chars),1,length(prefix))=prefix)
 THEN 8 ELSE 0 END,body FROM fields,spaces WHERE body IS NOT NULL
 UNION ALL
 SELECT turn_no,
 CASE WHEN body='' THEN 8 WHEN r.phase=3 THEN 4 WHEN instr(body,r.opener)=0 THEN r.phase+1 ELSE r.phase END,
 CASE WHEN body='' THEN ''
 WHEN r.phase=3 THEN CASE WHEN EXISTS(SELECT 1 FROM prefixes WHERE substr(ltrim(body,chars),1,length(prefix))=prefix) THEN '' ELSE body END
 WHEN instr(body,r.opener)=0 THEN body
 WHEN r.phase<3 THEN substr(body,instr(body,r.opener)+length(r.opener))
 WHEN instr(body,r.closer)=0 THEN substr(body,1,instr(body,r.opener)-1)
 ELSE substr(body,1,instr(body,r.opener)-1)||substr(body,instr(body,r.closer)+length(r.closer)) END
 FROM clean JOIN rules r ON clean.phase=r.phase CROSS JOIN spaces WHERE clean.phase<8
),
visible AS MATERIALIZED (SELECT DISTINCT turn_no FROM clean,spaces WHERE phase=8 AND trim(body,chars)!=''),
selected AS (SELECT b.turn_no,b.first_seq,b.last_seq FROM turn_bounds b JOIN visible USING(turn_no) ORDER BY b.turn_no LIMIT ? OFFSET ?)
SELECT totals.total,selected.turn_no,selected.first_seq,selected.last_seq
FROM (SELECT COUNT(*) total FROM visible) totals LEFT JOIN selected ON 1 ORDER BY selected.turn_no`

type sessionPageBounds struct{ number, first, last int }

// GetSessionPage reads a page of visible indexed turns in one read transaction.
// Metadata scans may inspect all rows to count visibility; the body SELECT uses
// only the selected turn intervals on (session_id,seq), never a whole-session
// content query followed by slicing in Go. No schema change or source read is
// needed for legacy indexes.
func GetSessionPage(ctx context.Context, db *sql.DB, idOrPrefix string, offset, limit int) (*ShowResult, int, error) {
	if offset < 0 || offset > 100000 || limit < 1 || limit > 50 {
		return nil, 0, fmt.Errorf("visible turn page requires offset 0–100000 and limit 1–50")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	stored, err := getSessionMetadata(tx, idOrPrefix)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, sessionVisiblePageSQL, stored.FullID, stored.FullID, stored.FullID, stored.FullID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	bounds := []sessionPageBounds{}
	for rows.Next() {
		var number, first, last sql.NullInt64
		if err := rows.Scan(&total, &number, &first, &last); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if number.Valid {
			bounds = append(bounds, sessionPageBounds{int(number.Int64), int(first.Int64), int(last.Int64)})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	stored.Turns = []SessionTurn{}
	if len(bounds) > 0 {
		stored.Turns, err = readSessionPageTurns(ctx, tx, stored.FullID, bounds)
		if err != nil {
			return nil, 0, err
		}
	}
	stored.Tools, err = scanTools(tx, stored.FullID)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return stored, total, nil
}

func readSessionPageTurns(ctx context.Context, tx *sql.Tx, sessionID string, bounds []sessionPageBounds) ([]SessionTurn, error) {
	intervals := make([]string, 0, len(bounds))
	args := []any{sessionID}
	for _, interval := range bounds {
		intervals = append(intervals, "(seq>=? AND seq<=?)")
		args = append(args, interval.first, interval.last)
	}
	rows, err := tx.QueryContext(ctx, `SELECT seq,role,content,scope,kind FROM messages WHERE session_id=?
		AND scope='local' AND kind!='control' AND (role='assistant' OR (role='user' AND kind='conversation'))
		AND (`+strings.Join(intervals, " OR ")+`) ORDER BY seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := make([]SessionTurn, len(bounds))
	for i, bound := range bounds {
		turns[i].Number = bound.number
	}
	index := 0
	for rows.Next() {
		var seq int
		var role, content, scope, kind string
		if err := rows.Scan(&seq, &role, &content, &scope, &kind); err != nil {
			return nil, err
		}
		for index+1 < len(bounds) && seq > bounds[index].last {
			index++
		}
		content = strings.TrimSpace(content)
		if content == "" || scope != parser.MessageScopeLocal || kind == parser.MessageKindControl {
			continue
		}
		turn := &turns[index]
		if role == "user" && kind == parser.MessageKindConversation {
			turn.Question = content
			continue
		}
		if role != "assistant" {
			continue
		}
		switch kind {
		case parser.MessageKindProgress:
			turn.Progress = append(turn.Progress, content)
		case parser.MessageKindFinal:
			turn.Final = appendMessageBlock(turn.Final, content)
		default:
			turn.Answer = appendMessageBlock(turn.Answer, content)
		}
	}
	return turns, rows.Err()
}
