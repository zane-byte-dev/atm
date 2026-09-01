package cmd

import (
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestPaginateValidatesAndSlices(t *testing.T) {
	values, err := paginate([]int{1, 2, 3, 4}, 1, 2)
	if err != nil || len(values) != 2 || values[0] != 2 || values[1] != 3 {
		t.Fatalf("paginate = %#v, err = %v", values, err)
	}
	if _, err := paginate([]int{1}, -1, 1); err == nil {
		t.Fatal("expected negative offset error")
	}
}

func TestTodoListQueryPaginationAndBulkWake(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	oldJSON := jsonOutput
	oldQuery, oldLimit, oldOffset, oldStatus := todoListQueryFlag, todoListLimitFlag, todoListOffsetFlag, todoStatusFlag
	oldBulkReason := todoBulkReasonFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoListQueryFlag, todoListLimitFlag, todoListOffsetFlag, todoStatusFlag = oldQuery, oldLimit, oldOffset, oldStatus
		todoBulkReasonFlag = oldBulkReason
	})
	if err := seedTodos(store.Todo{ID: "t1", Title: "Alpha first", Priority: "P1", Status: "open", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Beta", Priority: "P1", Status: "open", Created: store.Today()},
		store.Todo{ID: "t3", Title: "Alpha dependent", Priority: "P1", Status: store.TodoStatusInProgress, WakeCondition: "waiting for todos: t1, t2", DependsOn: []string{"t1", "t2"}, Created: store.Today()}); err != nil {
		t.Fatal(err)
	}
	jsonOutput = true
	todoStatusFlag = "all"
	todoListQueryFlag = "alpha"
	todoListOffsetFlag = 1
	todoListLimitFlag = 1
	var runErr error
	out := captureStdout(t, func() { runErr = runTodoList(todoListCmd, nil) })
	if runErr != nil {
		t.Fatalf("list: %v", runErr)
	}
	var listed []store.Todo
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed) != 1 || listed[0].ID != "t3" {
		t.Fatalf("listed = %#v, err = %v, out = %q", listed, err, out)
	}
	todoBulkReasonFlag = "batch complete"
	captureStdout(t, func() { runErr = runTodoBulk(todoBulkCmd, []string{"done", "t1", "t2"}) })
	if runErr != nil {
		t.Fatalf("bulk done: %v", runErr)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if store.FindTodo(tf, "t1").Status != "done" || store.FindTodo(tf, "t2").Status != "done" || store.FindTodo(tf, "t3").Status != store.TodoStatusInProgress {
		t.Fatalf("todos after bulk = %#v", tf.Items)
	}
}

func TestKnowledgeListPagination(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	oldLimit, oldOffset := knowledgeListLimit, knowledgeListOffset
	t.Cleanup(func() { jsonOutput, knowledgeListLimit, knowledgeListOffset = oldJSON, oldLimit, oldOffset })
	for _, title := range []string{"A", "B", "C"} {
		if _, err := knowledge.Add(config.AtmDir, knowledge.AddDocumentInput{Title: title, Content: "body", Collection: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	jsonOutput = true
	knowledgeListOffset = 1
	knowledgeListLimit = 1
	var runErr error
	out := captureStdout(t, func() { runErr = knowledgeListCmd.RunE(knowledgeListCmd, nil) })
	if runErr != nil {
		t.Fatalf("knowledge list: %v", runErr)
	}
	var listed []knowledge.DocumentSummary
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("listed = %#v, err = %v, out = %q", listed, err, out)
	}
}
