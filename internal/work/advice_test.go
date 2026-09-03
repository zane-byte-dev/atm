package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

const adviceTestURL = "https://code.alibaba-inc.com/hera/wanda/codereview/29711269"

func TestAdviceReferencesNormalizeDeduplicateAndRejectUntrustedTargets(t *testing.T) {
	text := "[CR](" + adviceTestURL + "?tab=comments#note1)。\n" + adviceTestURL + "\n" +
		"https://code.alibaba-inc.com/a/b/c/codereview/12，新的 CR\n" +
		"https://code.alibaba-inc.com.evil.test/hera/wanda/codereview/1\n" +
		"https://user@code.alibaba-inc.com/hera/wanda/codereview/1\n" +
		"https://code.alibaba-inc.com/-x/y/codereview/1\n" +
		"https://code.alibaba-inc.com/../y/codereview/1\n" +
		"https://code.alibaba-inc.com/a/b/codereview/not-a-number\n" +
		"https://code.alibaba-inc.com/a/b/codereview/0"
	got := adviceReferences(text)
	want := []adviceReference{
		{URL: adviceTestURL, Repo: "hera/wanda", ID: 29711269},
		{URL: "https://code.alibaba-inc.com/a/b/c/codereview/12", Repo: "a/b/c", ID: 12},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %+v", got)
	}
}

func adviceFixtureRunner(state, status, comments string, fail string) AdviceRunner {
	return func(_ context.Context, args []string) ([]byte, error) {
		if args[2] == fail {
			return nil, errors.New("network unavailable")
		}
		switch args[2] {
		case "view":
			return []byte(fmt.Sprintf(`{"mergeRequest":{"id":29711269,"title":"Review","state":%q}}`, state)), nil
		case "status":
			return []byte(status), nil
		case "comment":
			return []byte(comments), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
}

func TestAdviceSeparatesMergedApprovalConflictsAndPartialFailure(t *testing.T) {
	for _, test := range []struct{ name, state, status, fail, want string }{
		{"merged beats ready", "merged", `{"mrId":29711269,"readyToMerge":true}`, "", "merged"},
		{"closed beats ready", "closed", `{"mrId":29711269,"readyToMerge":true}`, "", "closed"},
		{"approved", "opened", `{"mrId":29711269,"readyToMerge":true}`, "", "approved"},
		{"blocked", "opened", `{"mrId":29711269,"readyToMerge":false}`, "", "reviewing"},
		{"conflict beats ready", "opened", `{"mrId":29711269,"readyToMerge":true,"conflicted":true}`, "", "conflicted"},
		{"missing ready is unknown", "opened", `{"mrId":29711269}`, "", "reviewing"},
		{"wrong MR ignored", "opened", `{"mrId":1,"readyToMerge":true}`, "", "reviewing"},
		{"status failure keeps merged", "merged", "", "status", "merged"},
		{"view failure cannot imply approval", "", `{"mrId":29711269,"readyToMerge":true}`, "view", "unknown"},
		{"unknown state", "future", `{"mrId":29711269,"readyToMerge":true}`, "", "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := queryAdviceReview(context.Background(), adviceFixtureRunner(test.state, test.status, "[]", test.fail),
				adviceReference{URL: adviceTestURL, Repo: "hera/wanda", ID: 29711269}, nil, "2026-09-03T01:00:00Z", "open")
			if got.State != test.want {
				t.Fatalf("result = %+v", got)
			}
			if got.CommentCount == nil || *got.CommentCount != 0 || got.NewCommentCount != nil {
				t.Fatalf("first observation = %+v", got)
			}
		})
	}
}

func TestAdviceCommentBaselinesCountRepliesButNotDraftsOrGlobalThreadsAsUnresolved(t *testing.T) {
	comments := `[
		{"id":1,"note":"root","path":"a.go","closed":0,"createdAt":"2026-09-01T10:00:00Z"},
		{"id":2,"note":"reply","path":"a.go","closed":0,"parentNoteId":1,"createdAt":"2026-09-02T10:00:00Z"},
		{"id":3,"note":"global","closed":0},
		{"id":4,"note":"resolved","path":"b.go","closed":1},
		{"id":5,"note":"draft","isDraft":true},
		{"id":6,"note":"summary","isAiSummary":true},
		{"id":7,"note":"new reply","parentNoteId":1,"path":"a.go","closed":0,"createdAt":"2026-09-03T10:00:00Z"},
		{"id":7,"note":"duplicate","parentNoteId":1,"path":"a.go","closed":0}
	]`
	ref := adviceReference{URL: adviceTestURL, Repo: "hera/wanda", ID: 29711269}
	run := adviceFixtureRunner("opened", `{"mrId":29711269,"readyToMerge":false}`, comments, "")
	first := queryAdviceReview(context.Background(), run, ref, nil, "2026-09-03T12:00:00Z", "open")
	if *first.CommentCount != 5 || *first.UnresolvedCount != 1 || first.NewCommentCount != nil || first.Comments[0].ID != 7 {
		t.Fatalf("first result = %+v", first)
	}
	previous := &AdviceCommentBaseline{URL: adviceTestURL, CheckedAt: "2026-09-02T12:00:00Z", CommentIDs: []int64{1, 2, 3, 4, 99}}
	next := queryAdviceReview(context.Background(), run, ref, previous, "2026-09-03T12:00:00Z", "open")
	if *next.NewCommentCount != 1 || !strings.Contains(next.Suggestion, "1 条新评论") || len(next.Comments) != 3 {
		t.Fatalf("next result = %+v", next)
	}
	unchanged := queryAdviceReview(context.Background(), run, ref, next.Baseline, "2026-09-03T12:01:00Z", "open")
	if *unchanged.NewCommentCount != 0 {
		t.Fatalf("unchanged = %+v", unchanged)
	}
	failed := queryAdviceReview(context.Background(), adviceFixtureRunner("merged", `{"mrId":29711269,"readyToMerge":true}`, "", "comment"),
		ref, previous, "2026-09-03T12:00:00Z", "open")
	if failed.CommentCount != nil || failed.NewCommentCount != nil || !reflect.DeepEqual(failed.Baseline, previous) || len(failed.Errors) != 1 {
		t.Fatalf("failed fetch advanced observation: %+v", failed)
	}
	for _, malformed := range []string{`null`, `{}`, `[{"note":"missing id"}]`, `not json`} {
		got := queryAdviceReview(context.Background(), adviceFixtureRunner("merged", `{"mrId":29711269,"readyToMerge":true}`, malformed, ""),
			ref, previous, "2026-09-03T12:00:00Z", "open")
		if got.CommentCount != nil || !reflect.DeepEqual(got.Baseline, previous) || len(got.Errors) == 0 {
			t.Fatalf("invalid response %s became an observation: %+v", malformed, got)
		}
	}
}

func TestAdviceReadsTaskLinksAndDocumentWithoutChangingWorkState(t *testing.T) {
	withTempWorkStore(t)
	todo := store.Todo{ID: "t1", Title: "Review CR", Description: adviceTestURL, Status: "open", Priority: "P2", Created: store.Today(),
		Links: []store.TodoLink{{URL: "https://code.alibaba-inc.com/other/repo/codereview/2"}}}
	seedWorkTodos(t, todo)
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendTodoLog(&todo, "新增 CR https://code.alibaba-inc.com/doc/repo/codereview/3", "分析"); err != nil {
		t.Fatal(err)
	}
	docBefore, _ := os.ReadFile(store.TodoDocPath(todo.ID))
	stateBefore, _ := store.LoadTodosReadOnly()
	var mu sync.Mutex
	commands := [][]string{}
	service := Service{AdviceRunner: func(_ context.Context, args []string) ([]byte, error) {
		mu.Lock()
		commands = append(commands, slices.Clone(args))
		mu.Unlock()
		return nil, errors.New("offline")
	}}
	result, err := service.Advice(context.Background(), bindingCall(application.ActorHuman, ""), AdviceInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reviews) != 3 || len(commands) != 9 {
		t.Fatalf("result = %+v; commands = %v", result, commands)
	}
	for _, args := range commands {
		if args[0] != "repo" || args[1] != "mr" || !slices.Contains([]string{"view", "status", "comment"}, args[2]) ||
			!slices.Contains(args, "--repo") || !slices.Contains(args, "--no-update-check") {
			t.Fatalf("unexpected external operation: %v", args)
		}
	}
	docAfter, _ := os.ReadFile(store.TodoDocPath(todo.ID))
	stateAfter, _ := store.LoadTodosReadOnly()
	if string(docBefore) != string(docAfter) || !reflect.DeepEqual(stateBefore, stateAfter) {
		t.Fatal("advice mutated task state")
	}
	if _, err := service.Advice(context.Background(), bindingCall(application.ActorHuman, ""), AdviceInput{TodoID: "t999"}); err == nil {
		t.Fatal("missing todo should fail")
	}
}

func TestAdviceNoLinksDoesNotInvokeA1AndLimitsFanout(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{ID: "t1", Title: "Local task", Status: "open", Priority: "P2", Created: store.Today()})
	called := false
	service := Service{AdviceRunner: func(context.Context, []string) ([]byte, error) { called = true; return nil, errors.New("unexpected") }}
	result, err := service.Advice(context.Background(), bindingCall(application.ActorHuman, ""), AdviceInput{TodoID: "t1"})
	if err != nil || called || len(result.Reviews) != 0 || result.Summary == "" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("advice initialized a task document")
	}

	links := []store.TodoLink{}
	for i := 1; i <= 8; i++ {
		links = append(links, store.TodoLink{URL: fmt.Sprintf("https://code.alibaba-inc.com/a/b/codereview/%d", i)})
	}
	seedWorkTodos(t, store.Todo{ID: "t2", Title: "Many links", Links: links, Status: "open", Priority: "P2", Created: store.Today()})
	service.AdviceRunner = func(context.Context, []string) ([]byte, error) { return nil, errors.New("offline") }
	result, err = service.Advice(context.Background(), bindingCall(application.ActorHuman, ""), AdviceInput{TodoID: "t2"})
	if err != nil || len(result.Reviews) != 5 || !strings.Contains(result.Summary, "8") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestAdviceBufferCapsOutputWithoutShortWrites(t *testing.T) {
	buffer := &adviceBuffer{limit: 10}
	for i := 0; i < 3; i++ {
		n, err := buffer.Write([]byte("123456"))
		if n != 6 || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if buffer.Len() != 10 || !buffer.exceeded {
		t.Fatalf("buffer = %+v", buffer)
	}
}

func TestAdviceIgnoresInvalidOrFutureBaseline(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{ID: "t1", Title: "Review", Description: adviceTestURL, Status: "open", Priority: "P2", Created: store.Today()})
	service := Service{AdviceRunner: adviceFixtureRunner("merged", `{"mrId":29711269,"readyToMerge":true}`, `[{"id":1}]`, "")}
	for _, checkedAt := range []string{"bad date", time.Now().Add(time.Hour).Format(time.RFC3339)} {
		result, err := service.Advice(context.Background(), bindingCall(application.ActorHuman, ""), AdviceInput{
			TodoID: "t1", Previous: []AdviceCommentBaseline{{URL: adviceTestURL, CheckedAt: checkedAt, CommentIDs: []int64{}}},
		})
		if err != nil || result.Reviews[0].NewCommentCount != nil {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), `"comment_ids":null`) {
			t.Fatalf("nil comment baseline: %s", encoded)
		}
	}
}
