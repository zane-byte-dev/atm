package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTodoCreateRecordSurvivesTodoChangesAndDeletion(t *testing.T) {
	withTempStore(t)
	todo := openTodo("t1", "Original title")
	result, err := json.Marshal(todo)
	if err != nil {
		t.Fatal(err)
	}
	want := TodoCreateRecord{
		Key: "create-request", PayloadHash: "original-hash", TodoID: todo.ID,
		ResultJSON: string(result), CreatedAt: 123,
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		if got, err := state.FindTodoCreate(want.Key); err != nil || got != nil {
			t.Fatalf("new request = %#v, %v", got, err)
		}
		state.Todos.Items = append(state.Todos.Items, todo)
		// The Todo has not reached SQLite yet. A foreign key here would break
		// creation, and later cascade deletion would break replay.
		return state.RecordTodoCreate(want)
	}); err != nil {
		t.Fatal(err)
	}

	assertReplay := func(t *testing.T) {
		t.Helper()
		// UpdateWorkState opens a fresh connection, as a later request would.
		if err := UpdateWorkState(func(state *WorkStateTx) error {
			got, err := state.FindTodoCreate(want.Key)
			if err != nil {
				return err
			}
			if got == nil || *got != want {
				t.Fatalf("replayed record = %#v, want %#v", got, want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertReplay(t)

	for _, step := range []struct {
		name   string
		mutate func(*WorkStateTx) error
	}{
		{"edit", func(state *WorkStateTx) error {
			FindTodo(state.Todos, todo.ID).Title = "Later title"
			return nil
		}},
		{"archive", func(state *WorkStateTx) error {
			_, err := state.ArchiveTodos([]string{todo.ID})
			return err
		}},
		{"delete", func(state *WorkStateTx) error {
			_, err := state.PermanentlyDeleteTodos([]string{todo.ID})
			return err
		}},
	} {
		t.Run(step.name, func(t *testing.T) {
			if err := UpdateWorkState(step.mutate); err != nil {
				t.Fatal(err)
			}
			assertReplay(t)
		})
	}

	duplicate := want
	duplicate.PayloadHash = "changed-hash"
	duplicate.ResultJSON = `{"id":"t2"}`
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		return state.RecordTodoCreate(duplicate)
	}); err == nil {
		t.Fatal("duplicate key overwrote the original response")
	}
	assertReplay(t)
}

func TestTodoCreateRecordRollsBackWhenTodoWriteFails(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Existing"))
	err := UpdateWorkState(func(state *WorkStateTx) error {
		todo := openTodo("t2", "Invalid")
		todo.Status = "invalid-status"
		state.Todos.Items = append(state.Todos.Items, todo)
		return state.RecordTodoCreate(TodoCreateRecord{
			Key: "failed-request", PayloadHash: "hash", TodoID: todo.ID,
			ResultJSON: `{"id":"t2"}`, CreatedAt: 123,
		})
	})
	if err == nil || !strings.Contains(err.Error(), "constraint failed") {
		t.Fatalf("Todo write error = %v", err)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		got, err := state.FindTodoCreate("failed-request")
		if err != nil {
			return err
		}
		if got != nil {
			t.Fatalf("failed creation left a replay record: %#v", got)
		}
		if len(state.Todos.Items) != 1 || state.Todos.Items[0].ID != "t1" {
			t.Fatalf("failed creation changed Todos: %#v", state.Todos.Items)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFreshSchemaAndV54MigrationIncludeTodoCreateIdempotency(t *testing.T) {
	for _, migrateFromV54 := range []bool{false, true} {
		name := "fresh"
		if migrateFromV54 {
			name = "migrate_v54"
		}
		t.Run(name, func(t *testing.T) {
			withTempStore(t)
			seedTodos(t, openTodo("t1", "Existing"))
			db, err := Open()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if migrateFromV54 {
				for _, statement := range []string{
					`DROP TABLE work_create_idempotency`,
					`UPDATE schema_version SET version=54`,
				} {
					if _, err := db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
				if err := migrate(db); err != nil {
					t.Fatalf("migrate v54: %v", err)
				}
			}
			var version, columns, references, todos int
			if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('work_create_idempotency')`).Scan(&columns); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_list('work_create_idempotency')`).Scan(&references); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM todos WHERE id='t1'`).Scan(&todos); err != nil {
				t.Fatal(err)
			}
			if version != SchemaVersion || columns != 5 || references != 0 || todos != 1 {
				t.Fatalf("version=%d columns=%d references=%d todos=%d", version, columns, references, todos)
			}
		})
	}
}
