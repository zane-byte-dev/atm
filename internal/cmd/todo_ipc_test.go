package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/appipc"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func TestIPCTodoReadAndMetadataWorkflowUsesWorkService(t *testing.T) {
	withIsolatedCommandEnv(t)

	created := runTodoIPCSuccess[appipc.TodoCreateRequest, workapp.Todo](t, "todo.create", appipc.TodoCreateRequest{
		Title: "Typed Todo", Description: "Created through the bounded App workflow",
		Priority: "P0", Project: "atm",
	})
	if created.ID != "t1" || created.Status != store.TodoStatusOpen ||
		created.Creator != store.TodoCreatorMe || created.Title != "Typed Todo" {
		t.Fatalf("created = %+v", created)
	}

	listed := runTodoIPCSuccess[appipc.TodoListRequest, []workapp.Todo](t, "todo.list", appipc.TodoListRequest{
		Status: "all", Query: "bounded App", Limit: 10,
	})
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed = %+v", listed)
	}

	shown := runTodoIPCSuccess[appipc.TodoIDRequest, appipc.TodoShowResponse](t, "todo.show", appipc.TodoIDRequest{
		TodoID: created.ID,
	})
	if shown.Todo.ID != created.ID || shown.Todo.Description != created.Description {
		t.Fatalf("shown = %+v", shown)
	}

	document := runTodoIPCSuccess[appipc.TodoIDRequest, appipc.TodoDocumentResponse](t, "todo.doc", appipc.TodoIDRequest{
		TodoID: created.ID,
	})
	if !document.Exists || !strings.Contains(document.Content, created.Description) {
		t.Fatalf("document = %+v", document)
	}

	title, source := "Updated Typed Todo", "menu bar"
	updated := runTodoIPCSuccess[appipc.TodoUpdateRequest, workapp.Todo](t, "todo.update", appipc.TodoUpdateRequest{
		TodoID: created.ID, Title: &title, Source: &source,
	})
	if updated.Title != title || updated.Status != store.TodoStatusOpen || updated.Source != source {
		t.Fatalf("updated = %+v", updated)
	}
	persisted := runTodoIPCSuccess[appipc.TodoIDRequest, appipc.TodoShowResponse](t, "todo.show", appipc.TodoIDRequest{
		TodoID: created.ID,
	})
	if persisted.Todo.Title != title || persisted.Todo.Status != store.TodoStatusOpen {
		t.Fatalf("persisted = %+v", persisted.Todo)
	}
}

func TestIPCTodoRequestsRejectCLIAndHiddenMutationFields(t *testing.T) {
	withIsolatedCommandEnv(t)

	for _, test := range []struct {
		name  string
		verb  string
		input string
	}{
		{
			name: "create status", verb: "todo.create",
			input: `{"title":"Must stay open","status":"review"}`,
		},
		{
			name: "create creator", verb: "todo.create",
			input: `{"title":"Must stay human","creator":"agent@codex"}`,
		},
		{
			name: "doc initialize", verb: "todo.doc",
			input: `{"todo_id":"t1","initialize":true}`,
		},
		{
			name: "list argv", verb: "todo.list",
			input: `{"status":"all","argv":["todo","delete","t1"]}`,
		},
		{
			name: "update action", verb: "todo.update",
			input: `{"todo_id":"t1","action":"delete"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := rawTodoIPC(test.verb, test.input, &output)
			if !errors.Is(err, application.ErrInvalidArgument) || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v\n%s", err, output.String())
			}
		})
	}

	created := runTodoIPCSuccess[appipc.TodoCreateRequest, workapp.Todo](t, "todo.create", appipc.TodoCreateRequest{
		Title: "First accepted Todo",
	})
	if created.ID != "t1" || created.Status != store.TodoStatusOpen || created.Creator != store.TodoCreatorMe || created.Priority != "P2" {
		t.Fatalf("rejected requests mutated defaults or consumed an ID: %+v", created)
	}
	var output bytes.Buffer
	for _, status := range []string{"in_progress", "review", "done"} {
		output.Reset()
		err := rawTodoIPC("todo.update", `{"todo_id":"t1","status":"`+status+`"}`, &output)
		if !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("lifecycle bypass %s = %v\n%s", status, err, output.String())
		}
	}
	shown := runTodoIPCSuccess[appipc.TodoIDRequest, appipc.TodoShowResponse](t, "todo.show", appipc.TodoIDRequest{
		TodoID: created.ID,
	})
	if shown.Todo.Status != store.TodoStatusOpen {
		t.Fatalf("rejected lifecycle bypass changed state: %+v", shown.Todo)
	}
}

func TestIPCTodoLifecycleRequiresReopenAndAcceptanceEvidence(t *testing.T) {
	withIsolatedCommandEnv(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Review guard", Priority: "P2", Status: store.TodoStatusReview,
		Creator: store.TodoCreatorMe, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := rawTodoIPC("todo.start", `{"todo_id":"t1"}`, &output)
	if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "reopen_reason") && !strings.Contains(err.Error(), "--reopen-reason") {
		t.Fatalf("missing reopen evidence = %v\n%s", err, output.String())
	}
	reopened := runTodoIPCSuccess[appipc.TodoStartRequest, workapp.Todo](t, "todo.start", appipc.TodoStartRequest{
		TodoID: "t1", ReopenReason: "review found a boundary regression",
	})
	if reopened.Status != store.TodoStatusInProgress {
		t.Fatalf("reopened = %+v", reopened)
	}
	output.Reset()
	err = rawTodoIPC("todo.done", `{"todo_id":"t1","reason":"通过 ATM 菜单栏完成"}`, &output)
	if !errors.Is(err, application.ErrInvalidArgument) || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("generic acceptance = %v\n%s", err, output.String())
	}
	completed := runTodoIPCSuccess[appipc.TodoDoneRequest, workapp.Todo](t, "todo.done", appipc.TodoDoneRequest{
		TodoID: "t1", Reason: "reviewed the fix and reran lifecycle tests",
	})
	if completed.Status != store.TodoStatusDone || completed.ClosedReason == nil || *completed.ClosedReason == "" {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestIPCTodoRefineDeliversEffectsAndHonorsSplitLimit(t *testing.T) {
	withIsolatedCommandEnv(t)
	parent := store.Todo{
		ID: "t1", Title: "模糊的大任务", Description: "需要梳理", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Creator: store.TodoCreatorMe, Created: store.Today(),
	}
	if err := seedTodos(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureTodoDoc(&parent); err != nil {
		t.Fatal(err)
	}

	var modelInput workapp.RefinementModelInput
	installIPCRefinementModel(t, func(
		_ context.Context,
		input workapp.RefinementModelInput,
	) (workapp.RefinementModelOutput, error) {
		modelInput = input
		return workapp.RefinementModelOutput{Proposal: ipcRefinementProposal(), Source: "test model"}, nil
	})

	response := runTodoIPCSuccess[appipc.TodoRefineRequest, appipc.TodoRefineResponse](
		t, "todo.refine", appipc.TodoRefineRequest{
			TodoID: "t1", AllowSplit: true, MaxChildren: 2, Hint: "  补验收标准  ",
		},
	)
	if !response.Changed || !response.Split || response.DryRun || len(response.Children) != 2 {
		t.Fatalf("response = %+v", response)
	}
	if response.Todo.Title != "实现可验收闭环" || response.Todo.Status != store.TodoStatusOpen {
		t.Fatalf("refined parent = %+v", response.Todo)
	}
	if modelInput.Options.Hint != "补验收标准" || !modelInput.Options.AllowSplit || modelInput.Options.MaxChildren != 2 {
		t.Fatalf("model options = %+v", modelInput.Options)
	}
	for _, child := range response.Children {
		if !store.TodoDocExists(child.ID) {
			t.Fatalf("child document %s was not delivered", child.ID)
		}
	}
	content, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "实现可验收闭环") || !strings.Contains(content, "模型整理") {
		t.Fatalf("refinement projection missing from parent doc:\n%s", content)
	}
	pending, err := store.ListPendingWorkEffects("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("delivered refinement effect was not acknowledged: %+v", pending)
	}
}

func TestIPCTodoRefineDryRunBoundsHintAndRejectsTransportEscape(t *testing.T) {
	withIsolatedCommandEnv(t)
	parent := store.Todo{
		ID: "t1", Title: "保持原样", Description: "dry run must not persist", Priority: "P1",
		Status: store.TodoStatusOpen, Creator: store.TodoCreatorMe, Created: store.Today(),
	}
	if err := seedTodos(parent); err != nil {
		t.Fatal(err)
	}

	modelCalls := 0
	var modelInput workapp.RefinementModelInput
	installIPCRefinementModel(t, func(
		_ context.Context,
		input workapp.RefinementModelInput,
	) (workapp.RefinementModelOutput, error) {
		modelCalls++
		modelInput = input
		return workapp.RefinementModelOutput{Proposal: ipcRefinementProposal(), Source: "test model"}, nil
	})

	longHint := "  " + strings.Repeat("界", 600) + "  "
	raw, err := json.Marshal(appipc.TodoRefineRequest{
		TodoID: "t1", AllowSplit: false, MaxChildren: 1, Hint: longHint, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := rawTodoIPC("todo.refine", string(raw), &output); err != nil {
		t.Fatalf("todo.refine dry run: %v\n%s", err, output.String())
	}
	var wireEnvelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &wireEnvelope); err != nil {
		t.Fatal(err)
	}
	if children, ok := wireEnvelope.Data["children"].([]any); !ok || len(children) != 0 {
		t.Fatalf("no-child response must encode an empty array, not null: %#v\n%s", wireEnvelope.Data["children"], output.String())
	}
	proposalWire, ok := wireEnvelope.Data["proposal"].(map[string]any)
	if !ok {
		t.Fatalf("proposal wire shape = %#v", wireEnvelope.Data["proposal"])
	}
	proposalChildren, ok := proposalWire["children"].([]any)
	if !ok || len(proposalChildren) == 0 {
		t.Fatalf("proposal children wire shape = %#v", proposalWire["children"])
	}
	firstChild, _ := proposalChildren[0].(map[string]any)
	if dependencies, ok := firstChild["depends_on_indexes"].([]any); !ok || len(dependencies) != 0 {
		t.Fatalf("empty dependency indexes must encode as an array: %#v", firstChild["depends_on_indexes"])
	}
	var envelope struct {
		Data appipc.TodoRefineResponse `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	response := envelope.Data
	if !response.DryRun || !response.Changed || response.Split || response.SplitSkip != "split disabled" {
		t.Fatalf("response = %+v", response)
	}
	if response.Proposal == nil || response.Todo.Title != parent.Title || len(response.Children) != 0 {
		t.Fatalf("dry-run response = %+v", response)
	}
	if utf8.RuneCountInString(modelInput.Options.Hint) != 500 || strings.TrimSpace(modelInput.Options.Hint) != modelInput.Options.Hint {
		t.Fatalf("bounded hint has %d runes", utf8.RuneCountInString(modelInput.Options.Hint))
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	persisted := store.FindTodo(todos, "t1")
	if persisted == nil || persisted.Title != parent.Title || persisted.Description != parent.Description {
		t.Fatalf("dry run mutated todo: %+v", persisted)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("dry run materialized a todo document")
	}
	if pending, err := store.ListPendingWorkEffects("t1"); err != nil || len(pending) != 0 {
		t.Fatalf("dry run left effects: %+v, err=%v", pending, err)
	}

	output.Reset()
	err = rawTodoIPC("todo.refine", `{"todo_id":"t1","allow_split":true,"max_children":6}`, &output)
	if !errors.Is(err, application.ErrInvalidArgument) || modelCalls != 1 {
		t.Fatalf("unsafe fan-out error=%v model_calls=%d\n%s", err, modelCalls, output.String())
	}
	for _, input := range []string{
		`{"todo_id":"t1","allow_split":true,"timeout":300}`,
		`{"todo_id":"t1","allow_split":true,"argv":["todo","done","t1"]}`,
	} {
		output.Reset()
		err = rawTodoIPC("todo.refine", input, &output)
		if !errors.Is(err, application.ErrInvalidArgument) || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("transport escape error=%v\n%s", err, output.String())
		}
	}
}

func installIPCRefinementModel(t *testing.T, model workapp.RefinementModelFunc) {
	t.Helper()
	previous := workapp.Default.RefinementModel
	workapp.Default.RefinementModel = model
	ipcServer = newAppIPCServer()
	t.Cleanup(func() { workapp.Default.RefinementModel = previous })
}

func ipcRefinementProposal() refine.Proposal {
	return refine.Proposal{
		Title: "实现可验收闭环", Description: "目标：把输入转成可以逐项验收的交付物。",
		Complexity: refine.ComplexityComplex, Reason: "三项工作可以独立验收", Plan: "先契约，再实现，最后回归。",
		Children: []refine.Child{
			{Title: "定义 typed 契约", Description: "锁定输入输出。"},
			{Title: "实现持久化路径", Description: "落地状态和投影。", DependsOnIndexes: []int{0}},
			{Title: "补齐回归验证", Description: "覆盖真实跨语言解码。", DependsOnIndexes: []int{1}},
		},
	}
}

func runTodoIPCSuccess[Request any, Response any](t *testing.T, verb string, request Request) Response {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := rawTodoIPC(verb, string(raw), &output); err != nil {
		t.Fatalf("%s: %v\n%s", verb, err, output.String())
	}
	var envelope struct {
		Verb  string          `json:"verb"`
		Data  Response        `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s envelope: %v\n%s", verb, err, output.String())
	}
	if envelope.Verb != verb || len(envelope.Error) != 0 && string(envelope.Error) != "null" {
		t.Fatalf("envelope = %+v", envelope)
	}
	return envelope.Data
}

func rawTodoIPC(verb, input string, output *bytes.Buffer) error {
	if output == nil {
		output = &bytes.Buffer{}
	}
	previousIn, previousOut := ipcCmd.InOrStdin(), ipcCmd.OutOrStdout()
	ipcCmd.SetIn(strings.NewReader(input))
	ipcCmd.SetOut(output)
	defer func() {
		ipcCmd.SetIn(previousIn)
		ipcCmd.SetOut(previousOut)
	}()
	return runIPC(ipcCmd, []string{verb})
}
