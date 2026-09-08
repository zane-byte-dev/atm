package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Interleave a committed write after the task reference has established the
// read snapshot, before any detail collections are assembled. No sleeps or
// scheduling assumptions are needed: WAL allows the other connection to commit.
type interleavedTodoReader struct {
	sqlQueryer
	beforeDetail func()
}

func (query *interleavedTodoReader) QueryRow(statement string, args ...any) *sql.Row {
	if query.beforeDetail != nil && strings.Contains(statement, "SELECT id,title,description") {
		write := query.beforeDetail
		query.beforeDetail = nil
		write()
	}
	return query.sqlQueryer.QueryRow(statement, args...)
}

func detailTestPlan(revision int64) TodoPlanRevision {
	return TodoPlanRevision{TodoID: "t1", Revision: revision, BaseRevision: revision - 1,
		SnapshotJSON: `{ "items": [] }`, SnapshotHash: fmt.Sprint(revision),
		RequestID: fmt.Sprint(revision), ActorKind: "agent", Origin: "cli", CreatedAt: revision}
}

func TestReadTodoDetailKeepsCurrentTaskAndRelatedRowsOnOneSnapshot(t *testing.T) {
	withTempStore(t)
	todo := openTodo("t1", "before")
	todo.Status, todo.Tags = TodoStatusInProgress, []string{"before"}
	seedTodos(t, todo, openTodo("t2", "next task"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO sessions(id,short_id,agent,file_path) VALUES('s1','s1','pi','/tmp/s1.jsonl')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts) VALUES('s1',0,'user','before prompt',1)`); err != nil {
		t.Fatal(err)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		if _, err := state.BindSession(TodoSessionBinding{SessionID: "s1", TodoID: "t1", Agent: "pi"}); err != nil {
			return err
		}
		return state.AppendTodoPlanRevision(detailTestPlan(1))
	}); err != nil {
		t.Fatal(err)
	}
	var detail TodoDetail
	written := false
	err = withTodoReadSnapshot(context.Background(), func(query sqlQueryer) error {
		reader := &interleavedTodoReader{sqlQueryer: query, beforeDetail: func() {
			if err := UpdateWorkState(func(state *WorkStateTx) error {
				todo := FindTodo(state.Todos, "t1")
				todo.Title, todo.Status, todo.Tags = "after", TodoStatusReview, []string{"after"}
				if _, err := state.BindSession(TodoSessionBinding{SessionID: "s1", TodoID: "t2", Agent: "pi"}); err != nil {
					return err
				}
				if _, err := state.tx.Exec(`UPDATE messages SET content='after prompt' WHERE session_id='s1'`); err != nil {
					return err
				}
				return state.AppendTodoPlanRevision(detailTestPlan(2))
			}); err != nil {
				t.Fatal(err)
			}
			written = true
		}}
		var err error
		detail, err = readTodoDetail(reader, TodoReadQuery{SessionID: "s1"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !written || detail.Reference.ID != "t1" || detail.Reference.Status != TodoStatusInProgress ||
		detail.Todo.Title != "before" || detail.Todo.Status != TodoStatusInProgress || !reflect.DeepEqual(detail.Todo.Tags, []string{"before"}) ||
		len(detail.Bindings) != 1 || detail.Bindings[0].UnboundAt != nil || len(detail.Sessions) != 1 || detail.Sessions[0].UnboundAt != nil ||
		detail.LatestPlan == nil || detail.LatestPlan.Revision != 1 || !reflect.DeepEqual(detail.UserMessages["s1"], []string{"before prompt"}) {
		t.Fatalf("mixed read snapshot: %+v", detail)
	}
	current, err := ReadTodoDetail(context.Background(), TodoReadQuery{SessionID: "s1"})
	if err != nil || current.Todo.ID != "t2" {
		t.Fatalf("fresh current task = %+v, err=%v", current, err)
	}
	after, err := ReadTodoDetail(context.Background(), TodoReadQuery{TodoID: "t1"})
	if err != nil || after.Todo.Title != "after" || after.Todo.Status != TodoStatusReview ||
		len(after.Bindings) != 1 || after.Bindings[0].UnboundAt == nil || after.LatestPlan == nil || after.LatestPlan.Revision != 2 {
		t.Fatalf("fresh task detail = %+v, err=%v", after, err)
	}
}

func TestReadTodoDetailDoesNotDecodeUnrelatedTaskRows(t *testing.T) {
	withTempStore(t)
	target := openTodo("t1", "target")
	target.Tags, target.DependsOn = []string{"target-tag"}, []string{"t2"}
	target.Links = []TodoLink{{URL: "https://example.test/target", Kind: "reference"}}
	target.Images = []TodoImage{{StoredName: "target.png", Name: "target.png", MediaType: "image/png", SizeBytes: 20}}
	seedTodos(t, target, openTodo("t2", "unrelated"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// SQLite's flexible typing permits older/corrupt unrelated rows that the
	// Go row scanner cannot decode. A target lookup must not touch those rows.
	if _, err := db.Exec(`UPDATE todos SET maintenance_limit='invalid integer' WHERE id='t2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO todo_images(todo_id,position,stored_name,original_name,media_type,size_bytes)
		VALUES('t2',0,'other.png','other.png','image/png','invalid integer')`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTodosReadOnly(); err == nil {
		t.Fatal("fixture must reject the full working-set scan")
	}
	detail, err := ReadTodoDetail(context.Background(), TodoReadQuery{TodoID: "#T01"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Todo.ID != "t1" || !reflect.DeepEqual(detail.Todo.Tags, target.Tags) || !reflect.DeepEqual(detail.Todo.DependsOn, target.DependsOn) ||
		!reflect.DeepEqual(detail.Todo.Links, target.Links) || len(detail.Todo.Images) != 1 || detail.Todo.Images[0].SizeBytes != 20 ||
		detail.Todo.Images[0].Path != TodoImagePath("t1", "target.png") {
		t.Fatalf("target details = %+v", detail)
	}
}

func TestReadTodoReferenceNeedsOnlyTaskIdentity(t *testing.T) {
	withTempStore(t)
	// A reference read works against just the task identity table: no task
	// description, attachments, transcript, bindings or plan schema is needed.
	db, err := sql.Open("sqlite", config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE todos(id TEXT PRIMARY KEY,status TEXT,archived_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO todos VALUES('t1','review',42)`); err != nil {
		t.Fatal(err)
	}
	reference, err := ReadTodoReference(context.Background(), TodoReadQuery{TodoID: "01"})
	if err != nil || !reference.Found || !reference.Archived || reference.ID != "t1" || reference.Status != TodoStatusReview {
		t.Fatalf("reference = %+v, err=%v", reference, err)
	}
	missing, err := ReadTodoReference(context.Background(), TodoReadQuery{TodoID: "#T99"})
	if err != nil || missing.Found || missing.ID != "t99" {
		t.Fatalf("missing reference = %+v, err=%v", missing, err)
	}
}

func BenchmarkReadTodoDetail(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("todos=%d", size), func(b *testing.B) {
			oldDir, oldDB := config.AtmDir, config.AtmDB
			config.AtmDir = b.TempDir()
			config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
			b.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
			if err := UpdateWorkState(func(state *WorkStateTx) error {
				for index := 1; index <= size; index++ {
					todo := openTodo(fmt.Sprintf("t%d", index), "Task detail benchmark")
					todo.Description = strings.Repeat("task description ", 32)
					todo.Tags = []string{"benchmark"}
					todo.Links = []TodoLink{{URL: "https://example.test/task"}}
					state.Todos.Items = append(state.Todos.Items, todo)
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			input := TodoReadQuery{TodoID: fmt.Sprintf("t%d", size)}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				if _, err := ReadTodoDetail(context.Background(), input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
