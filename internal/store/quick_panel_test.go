package store

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

func TestReadQuickTodosBoundsLargeWorkingSetAndHydratesSelectedRows(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const today = "2026-09-04"
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	insert, err := tx.Prepare(`INSERT INTO todos
		(id,position,title,description,priority,status,project,wake_condition,review_at,created)
		VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10_000; i++ {
		status, wake, reviewAt := TodoStatusInProgress, "", ""
		switch i % 5 {
		case 0:
			status = TodoStatusReview
		case 1:
			status = TodoStatusBlocked
		case 2:
			reviewAt = today
		case 3:
			wake = "wait for release"
		}
		description, created := "ordinary", "2026-09-01"
		if i == 4 {
			description = "complete ETag content"
			created = "2026-08-01"
		}
		id := "t" + strconv.Itoa(i)
		if _, err := insert.Exec(id, i-1, "Todo "+id, description, "P1", status,
			"atm", wake, reviewAt, created); err != nil {
			insert.Close()
			tx.Rollback()
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	if err := insert.Close(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO todo_tags(todo_id,position,tag) VALUES('t4',0,'native')`,
		`INSERT INTO todo_dependencies(todo_id,position,dependency_id) VALUES('t4',0,'t5')`,
		`INSERT INTO todo_links(todo_id,position,url,kind,title,relation)
			VALUES('t4',0,'https://example.com/t4','reference','spec','relates')`,
		`INSERT INTO todo_images(todo_id,position,stored_name,original_name,media_type,size_bytes)
			VALUES('t4',0,'preview.png','source.png','image/png',123)`,
		// This association belongs to a row outside all three returned pages. It
		// must not leak into a selected task while child rows are grouped.
		`INSERT INTO todo_tags(todo_id,position,tag) VALUES('t9999',0,'unselected')`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			tx.Rollback()
			t.Fatalf("seed association: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	got, err := ReadQuickTodos(context.Background(), db, today, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != (QuickTodoCounts{
		Review: 2000, Blocked: 2000, Due: 2000,
		Working: 6000, PureWorking: 2000, Waiting: 2000,
	}) {
		t.Fatalf("summary=%+v", got.Summary)
	}
	if len(got.NeedsAction) != 5 || len(got.Working) != 5 || len(got.Waiting) != 5 || !got.Truncated {
		t.Fatalf("page sizes needs=%d working=%d waiting=%d truncated=%v",
			len(got.NeedsAction), len(got.Working), len(got.Waiting), got.Truncated)
	}
	for _, row := range got.NeedsAction {
		if row.MenuState != "review" {
			t.Fatalf("needs-action row=%s/%s, want the highest-ranked review bucket", row.Todo.ID, row.MenuState)
		}
	}
	if got.Working[0].Todo.ID != "t4" || got.Working[0].MenuState != "working" {
		t.Fatalf("first working row=%s/%s, want older t4/working", got.Working[0].Todo.ID, got.Working[0].MenuState)
	}
	for _, row := range got.Waiting {
		if row.MenuState != "waiting" {
			t.Fatalf("waiting row=%s/%s", row.Todo.ID, row.MenuState)
		}
	}

	selected := got.Working[0].Todo
	if selected.Description != "complete ETag content" || len(selected.Tags) != 1 ||
		selected.Tags[0] != "native" || len(selected.DependsOn) != 1 || selected.DependsOn[0] != "t5" ||
		len(selected.Links) != 1 || selected.Links[0].URL != "https://example.com/t4" ||
		len(selected.Images) != 1 || selected.Images[0].StoredName != "preview.png" ||
		selected.Images[0].Path == "" {
		t.Fatalf("selected todo was not fully hydrated: %+v", selected)
	}
	overlap := false
	workingIDs := map[string]bool{}
	for _, row := range got.Working {
		workingIDs[row.Todo.ID] = true
	}
	for _, row := range got.Waiting {
		if workingIDs[row.Todo.ID] {
			overlap = true
		}
	}
	if !overlap {
		t.Fatal("bounded working and waiting pages unexpectedly had no legacy overlap")
	}
}

func TestReadQuickTodosPreservesLegacyWorkingOverlap(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for position, row := range []struct {
		id, wake, reviewAt string
	}{
		{id: "t1", reviewAt: "2026-09-04"},
		{id: "t2", wake: "external release"},
		{id: "t3"},
		{id: "t4", reviewAt: "2026-09-03"},
		{id: "t5", wake: "dependency finishes"},
		{id: "t6"},
	} {
		if _, err := db.Exec(`INSERT INTO todos
			(id,position,title,priority,status,wake_condition,review_at,created)
			VALUES(?,?,?,'P1','in_progress',?,?,?)`, row.id, position, row.id,
			row.wake, row.reviewAt, "2026-09-01"); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ReadQuickTodos(context.Background(), db, "2026-09-04", 5)
	if err != nil {
		t.Fatal(err)
	}
	assertQuickTodoIDs(t, got.NeedsAction, []string{"t1", "t4"}, "due")
	assertQuickTodoIDs(t, got.Working, []string{"t1", "t2", "t3", "t4", "t5"}, "")
	assertQuickTodoIDs(t, got.Waiting, []string{"t2", "t5"}, "waiting")
	if got.Working[0].MenuState != "due" || got.Working[1].MenuState != "waiting" ||
		got.Working[2].MenuState != "working" {
		t.Fatalf("working classifications=%q,%q,%q", got.Working[0].MenuState,
			got.Working[1].MenuState, got.Working[2].MenuState)
	}
	if got.Summary.Working != 6 || got.Summary.Due != 2 || got.Summary.Waiting != 2 ||
		got.Summary.PureWorking != 2 || !got.Truncated {
		t.Fatalf("summary=%+v", got.Summary)
	}
}

func TestReadQuickTodosRanksBucketsBeforePriority(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for position, row := range []struct {
		id, priority, status, reviewAt, created string
	}{
		{id: "t20", priority: "P2", status: TodoStatusReview, created: "2026-08-01"},
		{id: "t50", priority: "P0", status: TodoStatusReview, created: "2026-09-01"},
		{id: "t5", priority: "P0", status: TodoStatusReview, created: "2026-09-01"},
		{id: "t2", priority: "P0", status: TodoStatusBlocked, created: "2026-08-01"},
		{id: "t3", priority: "P0", status: TodoStatusInProgress, reviewAt: "2026-09-04", created: "2026-08-01"},
	} {
		_, err := db.Exec(`INSERT INTO todos(id,position,title,priority,status,review_at,created)
			VALUES(?,?,?,?,?,?,?)`, row.id, position, row.id, row.priority, row.status,
			row.reviewAt, row.created)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadQuickTodos(context.Background(), db, "2026-09-04", 5)
	if err != nil {
		t.Fatal(err)
	}
	assertQuickTodoIDs(t, got.NeedsAction, []string{"t5", "t50", "t20", "t2", "t3"}, "")
	if got.Truncated {
		t.Fatal("small complete result marked truncated")
	}
}

func assertQuickTodoIDs(t *testing.T, rows []QuickTodoRow, ids []string, state string) {
	t.Helper()
	if len(rows) != len(ids) {
		t.Fatalf("rows=%d ids=%d", len(rows), len(ids))
	}
	for i, id := range ids {
		if rows[i].Todo.ID != id || state != "" && rows[i].MenuState != state {
			t.Fatalf("row[%d]=%s/%s, want %s/%s", i, rows[i].Todo.ID, rows[i].MenuState, id, state)
		}
	}
}

func TestQuickPanelScalarQueriesPropagateErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := ReadQuickTodos(context.Background(), db, "2026-09-04", 5); err == nil {
		t.Fatal("ReadQuickTodos succeeded without schema")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadQuickTodos(ctx, db, "2026-09-04", 5); err == nil {
		t.Fatal("ReadQuickTodos ignored canceled context")
	}
}
