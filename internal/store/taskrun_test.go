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

func TestInterruptTaskRunIsDistinctAndReleasesTheTodoClaim(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Interrupt a run"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first := TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: t.TempDir(),
		Policy: "guarded", LogPath: "/tmp/run-1.log", Status: TaskRunRunning,
		PID: 4321, StartTS: 10,
	}
	if err := CreateTaskRun(db, first); err != nil {
		t.Fatal(err)
	}
	if err := RecordTaskRunControllerPID(db, first.ID, 9876); err != nil {
		t.Fatal(err)
	}
	persisted, err := GetTaskRun(db, first.ID)
	if err != nil || persisted == nil || persisted.PID != 9876 {
		t.Fatalf("persisted controller pid: run=%#v err=%v", persisted, err)
	}
	if err := InterruptTaskRun(db, first.ID, 20, "interrupted by user"); err != nil {
		t.Fatal(err)
	}
	run, err := GetTaskRun(db, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.Status != TaskRunInterrupted || run.EndTS == nil || *run.EndTS != 20 ||
		run.ExitCode != nil || run.Message != "interrupted by user" {
		t.Fatalf("run = %#v", run)
	}
	if err := InterruptTaskRun(db, first.ID, 21, "again"); err == nil {
		t.Fatal("a finished interruption must not be overwritten")
	}

	second := first
	second.ID = "run-2"
	second.Status = TaskRunStarting
	second.PID = 0
	second.StartTS = 30
	if err := CreateTaskRun(db, second); err != nil {
		t.Fatalf("claim after interruption: %v", err)
	}
}

func TestLatestResumableTaskRunKeepsTheCodexThread(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Continue a run"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessionID := "019fea8d-a0c4-7130-a984-2c8128705934"
	first := TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: t.TempDir(),
		Prompt: "initial", Policy: "guarded", LogPath: "/tmp/run-1.log",
		Status: TaskRunStarting, StartTS: 10, SessionID: &sessionID,
	}
	if err := CreateTaskRun(db, first); err != nil {
		t.Fatal(err)
	}
	if err := FinishTaskRun(db, first.ID, 0, 20, "finished"); err != nil {
		t.Fatal(err)
	}
	withoutSession := first
	withoutSession.ID = "run-2"
	withoutSession.StartTS = 30
	withoutSession.SessionID = nil
	if err := CreateTaskRun(db, withoutSession); err != nil {
		t.Fatal(err)
	}
	if err := FinishTaskRun(db, withoutSession.ID, 1, 40, "failed before session start"); err != nil {
		t.Fatal(err)
	}

	resumable, err := LatestResumableTaskRun(db, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if resumable == nil || resumable.ID != first.ID || resumable.SessionID == nil || *resumable.SessionID != sessionID {
		t.Fatalf("resumable = %#v", resumable)
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

func TestMigrateV38ToV39AddsResumeSessionID(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`ALTER TABLE task_runs DROP COLUMN resume_session_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 38`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v38→v39: %v", err)
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
	resume := "019fea8d-a0c4-7130-a984-2c8128705934"
	if err := CreateTaskRun(db, TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp",
		Policy: "guarded", LogPath: "/tmp/run.log", Status: TaskRunStarting, StartTS: 1,
		ResumeSessionID: &resume,
	}); err != nil {
		t.Fatalf("create run after migration: %v", err)
	}
	run, err := GetTaskRun(db, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.ResumeSessionID == nil || *run.ResumeSessionID != resume {
		t.Fatalf("run = %#v", run)
	}
}

func TestResumableThreadIDAcceptsOnlyWhatCodexCanResume(t *testing.T) {
	threadID := "019fea8d-a0c4-7130-a984-2c8128705934"
	for _, test := range []struct {
		name  string
		value *string
		want  string
	}{
		{name: "bare uuid from session bind", value: &threadID, want: threadID},
		{name: "rollout form from transcript sync", want: threadID,
			value: stringPointer("rollout-2026-08-10T15-19-46-" + threadID)},
		{name: "uppercase is normalized", want: threadID,
			value: stringPointer(strings.ToUpper(threadID))},
		// Codex reads a non-UUID as a thread name and, when no such thread
		// exists, starts a brand-new session instead of failing — so callers
		// must be able to tell that resuming is not possible.
		{name: "opaque id is not resumable", value: stringPointer("codex-desktop-session")},
		{name: "blank", value: stringPointer("   ")},
		{name: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ResumableThreadID(test.value); got != test.want {
				t.Fatalf("ResumableThreadID = %q, want %q", got, test.want)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

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

func TestLinkTaskRunSessionReplacesHeuristicAssociation(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Link a run authoritatively"))
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
	wrong := "wrong-session"
	if _, err := db.Exec(`UPDATE task_runs SET session_id=? WHERE id=?`, wrong, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := LinkTaskRunSession(db, "run-1", "t1", "actual-session"); err != nil {
		t.Fatal(err)
	}
	run, err := GetTaskRun(db, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.SessionID == nil || *run.SessionID != "actual-session" {
		t.Fatalf("run = %#v", run)
	}
	if err := LinkTaskRunSession(db, "run-1", "t2", "other-session"); err == nil {
		t.Fatal("mismatched todo should not relink a run")
	}
}
