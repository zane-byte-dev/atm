package store

import (
	"strings"
	"testing"
)

func TestTaskRunClaimsTodoUntilOutcomeIsRecorded(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Run once"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first := TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: t.TempDir(),
		Policy: "guarded", LogPath: "/tmp/run-1.log", Status: TaskRunStarting, StartTS: 10,
	}
	if err := CreateTaskRun(db, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "run-2"
	second.StartTS = 11
	if err := CreateTaskRun(db, second); err == nil || !strings.Contains(err.Error(), "active agent run") {
		t.Fatalf("second claim error = %v", err)
	}
	if err := MarkTaskRunRunning(db, first.ID, 1234); err != nil {
		t.Fatal(err)
	}
	if err := FinishTaskRun(db, first.ID, 0, 20, "finished"); err != nil {
		t.Fatal(err)
	}
	if err := CreateTaskRun(db, second); err != nil {
		t.Fatalf("claim after completion: %v", err)
	}

	runs, err := ListTaskRuns(db, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != "run-2" || runs[1].Status != TaskRunCompleted ||
		runs[1].PID != 1234 || runs[1].ExitCode == nil || *runs[1].ExitCode != 0 {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestMigrateV35ToV36AddsTaskRuns(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`DROP TABLE task_runs`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 35`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v35→v36: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	if _, err := db.Exec(`INSERT INTO todos (id,position,title,priority,status,created)
		VALUES ('t1',0,'Migrated run','P1','open','2026-08-10')`); err != nil {
		t.Fatal(err)
	}
	if err := CreateTaskRun(db, TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp",
		Policy: "guarded", LogPath: "/tmp/run.log", Status: TaskRunStarting, StartTS: 1,
	}); err != nil {
		t.Fatalf("create run after migration: %v", err)
	}
}

func TestLinkTaskRunAssociatesClosestMatchingSession(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Link a run"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := CreateTaskRun(db, TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: "/tmp/atm",
		Policy: "guarded", LogPath: "/tmp/run.log", Status: TaskRunStarting, StartTS: 100,
	}); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := linkTaskRun(tx, "session-1", "codex", "atm", 105); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	run, err := GetTaskRun(db, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.SessionID == nil || *run.SessionID != "session-1" {
		t.Fatalf("run = %#v", run)
	}
}
