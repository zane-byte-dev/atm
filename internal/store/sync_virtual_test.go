package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func seedVirtualSource(t *testing.T, agent string, count int) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	if agent == "qoderwork" {
		old := config.QoderWorkDB
		config.QoderWorkDB = path
		t.Cleanup(func() { config.QoderWorkDB = old })
		exec(`CREATE TABLE projects(id TEXT PRIMARY KEY,name TEXT,path TEXT)`)
		exec(`CREATE TABLE chats(id TEXT PRIMARY KEY,name TEXT,project_id TEXT,created_at INTEGER,updated_at INTEGER,deleted_at INTEGER)`)
		exec(`CREATE TABLE sub_chats(id TEXT PRIMARY KEY,name TEXT,chat_id TEXT,mode TEXT,model_level TEXT,created_at INTEGER,updated_at INTEGER)`)
		exec(`CREATE TABLE messages(id TEXT PRIMARY KEY,chat_id TEXT,sub_chat_id TEXT,sequence INTEGER,role TEXT,parts TEXT,metadata TEXT,searchable_text TEXT,created_at INTEGER,updated_at INTEGER)`)
		exec(`INSERT INTO projects VALUES('p','atm','/tmp/atm')`)
		exec(`INSERT INTO chats VALUES('c','Task','p',100,120,NULL)`)
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("s%d", i)
			exec(`INSERT INTO sub_chats VALUES(?,?,'c','agent','qwork-auto',100,120)`, id, id)
			for j := 0; j < 20; j++ {
				exec(`INSERT INTO messages VALUES(?,'c',?,?,'user','[]','{}',?,110,110)`, fmt.Sprintf("%s-m%d", id, j), id, j, fmt.Sprintf("Question %d", j))
			}
		}
	} else {
		old := config.QoderDB
		config.QoderDB = path
		t.Cleanup(func() { config.QoderDB = old })
		exec(`CREATE TABLE chat_session(session_id TEXT PRIMARY KEY,session_title TEXT,project_name TEXT,mode TEXT,gmt_create INTEGER,gmt_modified INTEGER,parent_session_id TEXT)`)
		exec(`CREATE TABLE chat_message(id TEXT PRIMARY KEY,session_id TEXT,role TEXT,content TEXT,token_info TEXT,model_info TEXT,gmt_create INTEGER)`)
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("s%d", i)
			exec(`INSERT INTO chat_session VALUES(?,?,'atm','agent',100000,120000,'')`, id, id)
			for j := 0; j < 20; j++ {
				exec(`INSERT INTO chat_message VALUES(?,?,'user',?,'','',110000)`, fmt.Sprintf("%s-m%d", id, j), id, fmt.Sprintf("Question %d", j))
			}
		}
	}
	return db, path
}

func TestVirtualSyncSkipsUnchangedAndUpdatesOnlyChangedSession(t *testing.T) {
	for _, agent := range []string{"qoder", "qoderwork"} {
		t.Run(agent, func(t *testing.T) {
			source, _ := seedVirtualSource(t, agent, 40)
			db := openTempDB(t)
			prefix := ""
			if agent == "qoderwork" {
				prefix = "qoderwork:"
			}
			db.SetMaxOpenConns(1)
			var dbPath string
			var seq int
			var name string
			if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &dbPath); err != nil {
				t.Fatal(err)
			}
			run := func(want int) (time.Duration, int64) {
				t.Helper()
				if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				got, err := SyncAgentContext(context.Background(), db, agent)
				if err != nil || got != want {
					t.Fatalf("synced=%d want=%d err=%v", got, want, err)
				}
				elapsed := time.Since(start)
				var size int64
				if info, err := os.Stat(dbPath + "-wal"); err == nil {
					size = info.Size()
				}
				return elapsed, size
			}
			firstTime, firstWAL := run(40)
			var firstMessage int64
			if err := db.QueryRow("SELECT MIN(id) FROM messages WHERE session_id=?", prefix+"s1").Scan(&firstMessage); err != nil {
				t.Fatal(err)
			}
			secondTime, secondWAL := run(0)
			t.Logf("first: %s / %d WAL bytes; unchanged: %s / %d WAL bytes", firstTime, firstWAL, secondTime, secondWAL)
			if firstWAL > 0 && secondWAL >= firstWAL/5 {
				t.Fatalf("unchanged run still writes heavily: %d vs %d", secondWAL, firstWAL)
			}
			// Edit a message without touching timestamps or its length.
			query := "UPDATE chat_message SET content='Question X' WHERE id='s0-m0'"
			if agent == "qoderwork" {
				query = "UPDATE messages SET searchable_text='Question X' WHERE id='s0-m0'"
			}
			if _, err := source.Exec(query); err != nil {
				t.Fatal(err)
			}
			run(1)
			var unchangedMessage int64
			if err := db.QueryRow("SELECT MIN(id) FROM messages WHERE session_id=?", prefix+"s1").Scan(&unchangedMessage); err != nil {
				t.Fatal(err)
			}
			if unchangedMessage != firstMessage {
				t.Fatal("unmodified sibling session was rebuilt")
			}
			var found int
			if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id=? AND content LIKE '%Question X%'", prefix+"s0").Scan(&found); err != nil || found != 1 {
				t.Fatalf("edit missed: %d %v", found, err)
			}
			query = "DELETE FROM chat_message WHERE id='s0-m0'"
			if agent == "qoderwork" {
				query = "DELETE FROM messages WHERE id='s0-m0'"
			}
			if _, err := source.Exec(query); err != nil {
				t.Fatal(err)
			}
			run(1)
			if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id=?", prefix+"s0").Scan(&found); err != nil || found != 19 {
				t.Fatalf("deletion missed: %d %v", found, err)
			}
			run(0)
			// A checkpoint changes file identity without changing session content.
			if _, err := source.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
				t.Fatal(err)
			}
			run(0)
			query = "DELETE FROM chat_message WHERE session_id='s0'"
			if agent == "qoderwork" {
				query = "DELETE FROM messages WHERE sub_chat_id='s0'"
			}
			if _, err := source.Exec(query); err != nil {
				t.Fatal(err)
			}
			run(1)
			if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id=?", prefix+"s0").Scan(&found); err != nil || found != 0 {
				t.Fatalf("empty source retained old messages: %d %v", found, err)
			}
			run(0)
		})
	}
}

func TestQoderDescendantUsageInvalidatesParent(t *testing.T) {
	source, _ := seedVirtualSource(t, "qoder", 1)
	db := openTempDB(t)
	if _, err := SyncAgentContext(context.Background(), db, "qoder"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO chat_session VALUES('child','Child','atm','agent_sub',100000,120000,'s0')`,
		`INSERT INTO chat_message VALUES('child-m','child','assistant','Done','{"prompt_tokens":100,"completion_tokens":20}','{"model_key":"test"}',110000)`,
	} {
		if _, err := source.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := SyncAgentContext(context.Background(), db, "qoder"); err != nil || n != 1 {
		t.Fatalf("parent not refreshed: %d %v", n, err)
	}
	var tokens int
	if err := db.QueryRow("SELECT input_tokens FROM usage WHERE session_id='s0'").Scan(&tokens); err != nil || tokens != 100 {
		t.Fatalf("child usage lost: %d %v", tokens, err)
	}
}
