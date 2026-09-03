package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestSessionPageReadsCompleteSelectedTurnIntervals(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`INSERT INTO sessions(id,short_id,agent,project,file_path,created_at,created_ts,last_ts) VALUES('page','page','codex','atm','','2026-09-03',1,1)`); err != nil {
		t.Fatal(err)
	}
	for seq, record := range []struct{ role, kind, body string }{
		{"user", "conversation", "first"}, {"assistant", "conversation", strings.Repeat("界", 200000)},
		{"user", "conversation", "# AGENTS.md instructions"}, {"assistant", "final", "<system_context>hidden</system_context>"},
		{"user", "conversation", "selected"}, {"assistant", "progress", "step one"}, {"assistant", "final", "part one"}, {"assistant", "final", "part two"},
		{"user", "conversation", "last"}, {"assistant", "conversation", strings.Repeat("z", 200000)},
	} {
		if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,kind,content) VALUES('page',?,?,?,?)`, seq, record.role, record.kind, record.body); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := GetSessionPage(context.Background(), db, "page", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(page.Turns) != 1 || page.Turns[0].Number != 3 || page.Turns[0].Question != "selected" || page.Turns[0].Final != "part one\n\npart two" || len(page.Turns[0].Progress) != 1 {
		t.Fatalf("incomplete visible page: total=%d turns=%+v", total, page.Turns)
	}
	if page.Inputs != nil || page.Outputs != nil {
		t.Fatal("paged read populated the unbounded legacy input/output projections")
	}
	// Both a page beyond the end and one consisting only of hidden raw turns
	// must retain the same visible total, without selecting neighboring bodies.
	last, total, err := GetSessionPage(context.Background(), db, "page", 99, 1)
	if err != nil || total != 3 || len(last.Turns) != 0 {
		t.Fatalf("past end: total=%d turns=%v error=%v", total, last, err)
	}
}

func BenchmarkSessionPageLongTranscript(b *testing.B) {
	// A temporary schema keeps benchmark writes out of the user's index.
	previousDir, previousDB := config.AtmDir, config.AtmDB
	config.AtmDir = b.TempDir()
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	b.Cleanup(func() { config.AtmDir, config.AtmDB = previousDir, previousDB })
	db, err := Open()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO sessions(id,short_id,agent,project,file_path,created_at,created_ts,last_ts) VALUES('long-page','long-page','codex','atm','','2026-09-03',1,1)`); err != nil {
		b.Fatal(err)
	}
	body := strings.Repeat("long answer ", 12000)
	for i := 0; i < 100; i++ {
		if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content) VALUES('long-page',?,'user','question'),('long-page',?,'assistant',?)`, i*2, i*2+1, body); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("one_visible_turn", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := GetSessionPage(context.Background(), db, "long-page", 50, 1); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("legacy_full_session", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := GetSession(db, "long-page"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
