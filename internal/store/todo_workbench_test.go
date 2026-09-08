package store

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestTodoWorkbenchMetadataAndIndependentGroupPages(t *testing.T) {
	withTempStore(t)
	todos := make([]Todo, 0, 300)
	for index := 1; index <= 300; index++ {
		status, project := "open", "one"
		if index > 180 {
			status = "done"
		}
		if index%2 == 0 {
			project = "two"
		}
		todos = append(todos, Todo{ID: fmt.Sprintf("t%d", index), Title: "Task", Status: status, Project: project, Priority: "P1", Created: "2026-09-01"})
	}
	seedTodos(t, todos...)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE todos SET updated_at=CAST(SUBSTR(id,2) AS INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	input := TodoPageQuery{Workbench: true, MetadataOnly: true, GroupBy: "project", Limit: 50}
	meta, err := ReadTodoPage(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Todos) != 0 || meta.Total != 300 || meta.MatchedTotal != 300 || len(meta.Groups) != 2 {
		t.Fatalf("metadata = %+v", meta)
	}
	if group := meta.Groups[0]; group.Key != `["one",""]` || group.Total != 150 || group.ActiveTotal != 90 || group.DoneTotal != 60 {
		t.Fatalf("group = %+v", group)
	}
	if meta.ViewCounts["unfinished"] != 180 || meta.Counts["done"] != 120 {
		t.Fatalf("counts = %+v / %+v", meta.ViewCounts, meta.Counts)
	}
	input.MetadataOnly, input.GroupKey, input.Partition = false, meta.Groups[0].Key, "active"
	first, err := ReadTodoPage(context.Background(), input)
	if err != nil || first.Total != 90 || len(first.Todos) != 50 || first.Facets != nil || first.Groups != nil {
		t.Fatalf("first group page = %+v, %v", first, err)
	}
	input.Offset = 50
	second, err := ReadTodoPage(context.Background(), input)
	if err != nil || len(second.Todos) != 40 || first.Todos[49].ID != "t99" || second.Todos[0].ID != "t101" {
		t.Fatalf("second group page = %+v, %v", second, err)
	}
	seen := map[string]bool{}
	for _, todo := range append(first.Todos, second.Todos...) {
		if seen[todo.ID] || todo.Project != "one" || todo.Status == "done" {
			t.Fatalf("invalid/duplicate paged task: %+v", todo)
		}
		seen[todo.ID] = true
	}
	input.Partition, input.Limit, input.Offset = "done", 5, 0
	firstDone, err := ReadTodoPage(context.Background(), input)
	if err != nil || firstDone.Total != 60 || len(firstDone.Todos) != 5 || firstDone.Todos[0].ID != "t299" || firstDone.Todos[4].ID != "t291" {
		t.Fatalf("initial completed page = %+v, %v", firstDone, err)
	}
	input.Offset = 5
	nextDone, err := ReadTodoPage(context.Background(), input)
	if err != nil || nextDone.Todos[0].ID != "t289" {
		t.Fatalf("next completed page = %+v, %v", nextDone, err)
	}
	input.MetadataOnly, input.GroupBy = true, "project_status"
	input.GroupKey, input.Partition = "", ""
	swimlanes, err := ReadTodoPage(context.Background(), input)
	if err != nil || len(swimlanes.Groups) != 4 || swimlanes.Groups[0].Key != `["one","open"]` {
		t.Fatalf("swimlane metadata = %+v, %v", swimlanes, err)
	}
}

func TestTodoWorkbenchFiltersFacetReplacementAndMetadataSearch(t *testing.T) {
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t1", Title: "Älpha", Status: "in_progress", WakeCondition: "release", Project: `a"b`, Creator: "codex", Tags: []string{"red", "shared"}, Priority: "P0", Created: "2026-09-01", Description: "document-only-search"},
		Todo{ID: "t2", Title: "Review", Status: "review", Project: `a"b`, Creator: "codex", Tags: []string{"blue", "shared"}, Priority: "P1", Created: "2026-09-02"},
		Todo{ID: "t3", Title: "Done", Status: "done", Project: "other", Creator: "human", Tags: []string{"red"}, Priority: "P2", Created: "2026-09-03"},
		Todo{ID: "t4", Title: "Archived", Status: "open", Project: `a"b`, Creator: "codex", Tags: []string{"red"}, Priority: "P0", Created: "2026-09-01"},
		Todo{ID: "t5", Title: "No date", Status: "open", Project: "other", Priority: "P1", Created: ""},
	)
	if err := UpdateWorkState(func(tx *WorkStateTx) error { _, err := tx.ArchiveTodos([]string{"t4"}); return err }); err != nil {
		t.Fatal(err)
	}
	base := TodoPageQuery{Workbench: true, Limit: 20}
	for _, test := range []struct {
		name string
		edit func(*TodoPageQuery)
		ids  []string
	}{
		{"all defaults include done", func(q *TodoPageQuery) {}, []string{"t1", "t2", "t5", "t3"}},
		{"waiting", func(q *TodoPageQuery) { q.Focus = "waiting" }, []string{"t1"}},
		{"status and priority multi", func(q *TodoPageQuery) { q.Status, q.Priority = "review,done", "P0,P1" }, []string{"t2"}},
		{"exact project creator tag date", func(q *TodoPageQuery) {
			q.Project, q.Creator, q.Tag, q.From, q.To = `a"b`, "codex", "shared", "2026-09-02", "2026-09-02"
		}, []string{"t2"}},
		{"unicode metadata", func(q *TodoPageQuery) { q.Query = "ÄLPHA" }, []string{"t1"}},
		{"metadata field boundary", func(q *TodoPageQuery) { q.Query = `t1 a"b codex red` }, []string{"t1"}},
		{"not fulltext", func(q *TodoPageQuery) { q.Query = "document-only-search" }, []string{}},
		{"archive separation", func(q *TodoPageQuery) { q.Status = "archived" }, []string{"t4"}},
		{"no alias or case expansion", func(q *TodoPageQuery) { q.Project = "OTHER" }, []string{}},
		{"date excludes empty", func(q *TodoPageQuery) { q.To = "2026-09-01" }, []string{"t1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.edit(&input)
			result, err := ReadTodoPage(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, todo := range result.Todos {
				ids = append(ids, todo.ID)
			}
			if !reflect.DeepEqual(ids, test.ids) {
				t.Fatalf("got %v, want %v", ids, test.ids)
			}
		})
	}
	base.MetadataOnly, base.Project, base.Focus, base.Status, base.GroupBy = true, `a"b`, "waiting", "in_progress", "project_status"
	meta, err := ReadTodoPage(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if meta.MatchedTotal != 1 || meta.ViewCounts["all"] != 2 || meta.ViewCounts["review"] != 1 || meta.ViewCounts["waiting"] != 1 {
		t.Fatalf("view count replacement = %+v", meta)
	}
	if !reflect.DeepEqual(meta.Facets["status"], []TodoPageFacet{{"open", 0}, {"in_progress", 1}, {"review", 1}, {"done", 0}, {"archived", 1}}) {
		t.Fatalf("status replacement did not clear focus/archive correctly: %+v", meta.Facets["status"])
	}
	if !reflect.DeepEqual(meta.Facets["project"], []TodoPageFacet{{`a"b`, 1}, {"other", 0}}) || len(meta.Facets["tag"]) != 3 {
		t.Fatalf("full-scope zero-count options missing: %+v", meta.Facets)
	}
	base.MetadataOnly, base.GroupKey = false, meta.Groups[0].Key
	group, err := ReadTodoPage(context.Background(), base)
	if err != nil || group.Total != 1 || group.Todos[0].ID != "t1" {
		t.Fatalf("escaped project group key = %+v, %v", group, err)
	}
}

func TestTodoWorkbenchStableSortAndValidation(t *testing.T) {
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t2", Title: "任务10", Status: "open", Priority: "P1", Created: "2026-09-02"},
		Todo{ID: "t9", Title: "任务2", Status: "open", Priority: "P2", Created: "2026-09-01"},
		Todo{ID: "t10", Title: "任务2", Status: "open", Priority: "P0", Created: "2026-09-03"},
	)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE todos SET updated_at=CASE id WHEN 't2' THEN 20 ELSE 10 END`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for _, test := range []struct {
		sort, direction string
		ids             []string
	}{
		{"title", "asc", []string{"t9", "t10", "t2"}},
		{"title", "desc", []string{"t2", "t9", "t10"}},
		{"created", "asc", []string{"t9", "t2", "t10"}},
		{"priority", "desc", []string{"t9", "t2", "t10"}},
		{"updated", "asc", []string{"t9", "t10", "t2"}},
		{"updated", "desc", []string{"t2", "t9", "t10"}},
	} {
		ids := []string{}
		for offset := 0; offset < 3; offset++ {
			result, err := ReadTodoPage(context.Background(), TodoPageQuery{Workbench: true, Sort: test.sort, Direction: test.direction, Limit: 1, Offset: offset})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, result.Todos[0].ID)
		}
		if !reflect.DeepEqual(ids, test.ids) {
			t.Fatalf("sort %s %s: %v, want %v", test.sort, test.direction, ids, test.ids)
		}
	}
	for _, bad := range []TodoPageQuery{
		{Sort: "title;DELETE FROM todos"}, {Status: "archived,open"}, {Focus: "unknown"}, {Priority: "P8"},
		{From: "2026-02-30"}, {GroupKey: `null`}, {GroupBy: "status", GroupKey: `["project","open"]`}, {Partition: "all"},
	} {
		bad.Workbench, bad.Limit = true, 1
		if _, err := ReadTodoPage(context.Background(), bad); err == nil {
			t.Fatalf("invalid selectors accepted: %+v", bad)
		}
	}
}

func TestTodoWorkbenchSelectionRevisionTracksOnlyMatchingMembers(t *testing.T) {
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t1", Title: "one", Status: "open", Priority: "P1", Project: "scope", Created: "2026-09-01"},
		Todo{ID: "t2", Title: "two", Status: "open", Priority: "P1", Project: "scope", Created: "2026-09-01"},
		Todo{ID: "t3", Title: "three", Status: "open", Priority: "P1", Project: "outside", Created: "2026-09-01"},
	)
	input := TodoPageQuery{Workbench: true, MetadataOnly: true, Project: "scope", Limit: 1}
	read := func() TodoPage {
		t.Helper()
		result, err := ReadTodoPage(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := read()
	input.Sort, input.Direction, input.GroupBy, input.Partition, input.Offset = "title", "desc", "project_status", "done", 10
	if got := read(); got.SelectionRevision != before.SelectionRevision || got.MatchedTotal != 2 {
		t.Fatalf("presentation changed membership revision: %+v vs %+v", got, before)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").Description = "edited matching task"
		FindTodo(state.Todos, "t3").Title = "edited unrelated task"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.SelectionRevision != before.SelectionRevision {
		t.Fatalf("non-membership changes reset selection: %+v", got)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t2").Project = "outside"
		FindTodo(state.Todos, "t3").Project = "scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.MatchedTotal != before.MatchedTotal || got.SelectionRevision == before.SelectionRevision {
		t.Fatalf("equal-count replacement missed: %+v vs %+v", got, before)
	}
	input.Project = "empty"
	if got := read(); got.SelectionRevision != EmptyTodoSelectionRevision {
		t.Fatalf("empty scope digest differs: %s", got.SelectionRevision)
	}
}
