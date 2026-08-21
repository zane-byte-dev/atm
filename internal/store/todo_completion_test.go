package store

import "testing"

func TestGetTodoCompletionsIncludesArchivedAndLegacyRows(t *testing.T) {
	db := openTempDB(t)
	for _, statement := range []string{
		`INSERT INTO todos (id,position,title,priority,status,project,created,creator,closed,done_ts,archived_at)
		 VALUES ('t1',0,'Archived delivery','P1','done','atm','2026-08-01','me','2026-08-20',1787212800,1787212900)`,
		`INSERT INTO todos (id,position,title,priority,status,project,created,creator,closed,done_ts)
		 VALUES ('t2',1,'Timestamp only','P2','done','other','2026-08-02','codex',NULL,1787299200)`,
		`INSERT INTO todos (id,position,title,priority,status,project,created,creator,closed,done_ts)
		 VALUES ('t3',2,'Still open','P2','open','atm','2026-08-03','me',NULL,NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := GetTodoCompletions(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].TodoID != "t2" || rows[0].CompletedDate == "" {
		t.Fatalf("timestamp-only row = %#v", rows[0])
	}
	if rows[1].TodoID != "t1" || rows[1].CompletedDate != "2026-08-20" || rows[1].Project != "atm" {
		t.Fatalf("archived row = %#v", rows[1])
	}
}
