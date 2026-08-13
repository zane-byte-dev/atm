package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestApplyTodoRefineSplitsOpenParent(t *testing.T) {
	withIsolatedCommandEnv(t)
	parent := store.Todo{
		ID: "t1", Title: "做完整的收集闭环", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(), Creator: store.TodoCreatorMe,
	}
	if err := seedTodos(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureTodoDoc(&parent); err != nil {
		t.Fatal(err)
	}

	updated, children, err := applyTodoRefine(parent, refine.Prepared{
		Title:        "实现收集闭环",
		Description:  "目标：从聊天到 Todo 可回放。",
		TitleChanged: true,
		DescChanged:  true,
		Complexity:   refine.ComplexityComplex,
		Reason:       "三块独立工作",
		Plan:         "按依赖顺序做。",
		Split:        true,
		Children: []refine.Child{
			{Title: "写分类器契约", Description: "schema 与 prompt"},
			{Title: "实现落地路径", Description: "create/append", DependsOnIndexes: []int{0}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "实现收集闭环" || updated.Status != store.TodoStatusWaiting {
		t.Fatalf("parent = %+v", updated)
	}
	if len(updated.DependsOn) != 2 {
		t.Fatalf("parent deps = %#v", updated.DependsOn)
	}
	if len(children) != 2 {
		t.Fatalf("children = %#v", children)
	}
	if children[0].Source != refine.ChildSource("t1") || children[0].Project != "atm" {
		t.Fatalf("child = %+v", children[0])
	}
	if len(children[1].DependsOn) != 1 || children[1].DependsOn[0] != children[0].ID {
		t.Fatalf("child deps = %#v", children[1].DependsOn)
	}

	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "实现收集闭环") || !strings.Contains(doc, children[0].ID) || !strings.Contains(doc, "模型整理") {
		t.Fatalf("doc = %s", doc)
	}
}

func TestApplyTodoRefineDryRunDoesNotWrite(t *testing.T) {
	withIsolatedCommandEnv(t)
	oldJSON, oldDry, oldSplit := jsonOutput, todoRefineDryRunFlag, todoRefineNoSplitFlag
	t.Cleanup(func() {
		jsonOutput, todoRefineDryRunFlag, todoRefineNoSplitFlag = oldJSON, oldDry, oldSplit
	})
	jsonOutput = true
	todoRefineDryRunFlag = true

	parent := store.Todo{ID: "t1", Title: "修一下那个红的", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()}
	if err := seedTodos(parent); err != nil {
		t.Fatal(err)
	}

	oldRun := refineSwapRunModel(t, refine.Proposal{
		Title: "修复发布检查失败", Description: "目标：检查变绿。",
		Complexity: refine.ComplexitySimple, Reason: "一事一做",
	})
	defer oldRun()

	var stderr bytes.Buffer
	todoRefineCmd.SetErr(&stderr)
	out := captureStdout(t, func() {
		if err := runTodoRefine(todoRefineCmd, []string{"t1"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json %q: %v", out, err)
	}
	if payload["dry_run"] != true || payload["split"] != false {
		t.Fatalf("payload = %s", out)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if tf.Items[0].Title != "修一下那个红的" {
		t.Fatalf("dry-run wrote title: %+v", tf.Items[0])
	}
}

func TestRunTodoRefineAppliesStubbedModel(t *testing.T) {
	withIsolatedCommandEnv(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	jsonOutput = false

	parent := store.Todo{ID: "t1", Title: "修一下那个红的", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()}
	if err := seedTodos(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureTodoDoc(&parent); err != nil {
		t.Fatal(err)
	}
	undo := refineSwapRunModel(t, refine.Proposal{
		Title: "修复发布检查失败", Description: "目标：检查变绿。",
		Complexity: refine.ComplexitySimple, Reason: "一事一做",
	})
	defer undo()

	todoRefineCmd.SetErr(io.Discard)
	if err := runTodoRefine(todoRefineCmd, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if tf.Items[0].Title != "修复发布检查失败" {
		t.Fatalf("todo = %+v", tf.Items[0])
	}
}

func TestRunTodoAddRefineKeepsCreatedTodoWhenModelFails(t *testing.T) {
	withIsolatedCommandEnv(t)
	oldJSON, oldRefine, oldPriority, oldProject, oldDesc := jsonOutput, todoAddRefineFlag, todoAddPriorityFlag, todoAddProjectFlag, todoDescFlag
	t.Cleanup(func() {
		jsonOutput, todoAddRefineFlag = oldJSON, oldRefine
		todoAddPriorityFlag, todoAddProjectFlag, todoDescFlag = oldPriority, oldProject, oldDesc
		todoAddCmd.SetErr(os.Stderr)
	})
	jsonOutput = false
	todoAddRefineFlag = true
	todoAddPriorityFlag = "P1"
	todoAddProjectFlag = "atm"
	todoDescFlag = ""
	t.Setenv("ATM_TEXT_MODEL_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	var stderr bytes.Buffer
	todoAddCmd.SetErr(&stderr)
	out := captureStdout(t, func() {
		if err := runTodoAdd(todoAddCmd, []string{"修一下那个红的检查"}); err == nil {
			t.Fatal("expected refine failure")
		}
	})
	if strings.TrimSpace(out) != "t1" {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(stderr.String(), "Created t1") || !strings.Contains(stderr.String(), "Refine failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(tf.Items) != 1 || tf.Items[0].Title != "修一下那个红的检查" {
		t.Fatalf("todo should remain after refine failure: %+v", tf.Items)
	}
}

func TestRunTodoRefineRejectsClosedTodo(t *testing.T) {
	withIsolatedCommandEnv(t)
	closed := store.Today()
	reason := "done"
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "已经做完了", Priority: "P1", Status: store.TodoStatusDone,
		Created: store.Today(), Closed: &closed, ClosedReason: &reason,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runTodoRefine(todoRefineCmd, []string{"t1"}); err == nil {
		t.Fatal("closed todo was refined")
	}
}

func TestApplyTodoRefineDoesNotNotifyByCreatingDuplicateChildren(t *testing.T) {
	withIsolatedCommandEnv(t)
	parent := store.Todo{
		ID: "t1", Title: "实现收集闭环", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(), DependsOn: []string{"t9"},
	}
	if err := seedTodos(parent, store.Todo{
		ID: "t9", Title: "已有依赖", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := refine.Prepare(parent, 0, refine.Proposal{
		Title: "实现收集闭环", Description: "目标：闭环。", Complexity: refine.ComplexityComplex,
		Plan: "分步。", Children: []refine.Child{{Title: "写分类器契约"}, {Title: "实现落地路径"}},
	}, refine.Options{AllowSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Split {
		t.Fatal("existing deps should block split")
	}
}

func refineSwapRunModel(t *testing.T, proposal refine.Proposal) func() {
	t.Helper()
	// Analyze is in the refine package; command tests exercise its production
	// HTTP boundary with a tiny stand-in for the DeepSeek endpoint.
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		encoded, _ := json.Marshal(string(body))
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%s},"finish_reason":"stop"}]}`, encoded)
	}))
	oldKey, keyPresent := os.LookupEnv("ATM_TEXT_MODEL_API_KEY")
	oldURL, urlPresent := os.LookupEnv("ATM_TEXT_MODEL_BASE_URL")
	os.Setenv("ATM_TEXT_MODEL_API_KEY", "test-key")
	os.Setenv("ATM_TEXT_MODEL_BASE_URL", server.URL)
	return func() {
		server.Close()
		if keyPresent {
			os.Setenv("ATM_TEXT_MODEL_API_KEY", oldKey)
		} else {
			os.Unsetenv("ATM_TEXT_MODEL_API_KEY")
		}
		if urlPresent {
			os.Setenv("ATM_TEXT_MODEL_BASE_URL", oldURL)
		} else {
			os.Unsetenv("ATM_TEXT_MODEL_BASE_URL")
		}
	}
}

func TestRefineTimeoutConstantMatchesPolicy(t *testing.T) {
	if refine.DefaultTimeout < 30*time.Second {
		t.Fatalf("timeout too short: %s", refine.DefaultTimeout)
	}
}
