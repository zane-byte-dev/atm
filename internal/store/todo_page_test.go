package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestReadTodoPageBoundsRowsCountsProjectsAndDependencies(t *testing.T) {
	withTempStore(t)
	beforeAliases := config.ProjectAliases
	config.ProjectAliases = map[string]string{"legacy-atm": "atm"}
	t.Cleanup(func() { config.ProjectAliases = beforeAliases })
	seedTodos(t,
		Todo{ID: "t1", Title: "one", Priority: "P2", Status: TodoStatusOpen, Project: "legacy-atm", Created: "2026-09-01"},
		Todo{ID: "t2", Title: "two", Priority: "P1", Status: TodoStatusReview, Project: "atm", Created: "2026-09-02", DependsOn: []string{"t1"}},
		Todo{ID: "t9", Title: "nine", Priority: "P1", Status: TodoStatusOpen, Project: "other", Created: "2026-09-03"},
		Todo{ID: "t10", Title: "ten", Priority: "P0", Status: TodoStatusOpen, Project: "atm", Created: "2026-09-03"},
		Todo{ID: "t11", Title: "done", Priority: "P2", Status: TodoStatusDone, Project: "atm", Created: "2026-09-04"},
	)
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t9"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	page, err := ReadTodoPage(context.Background(), TodoPageQuery{Project: "ATM", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Todos) != 1 || page.Todos[0].ID != "t2" || !reflect.DeepEqual(page.Todos[0].DependsOn, []string{"t1"}) {
		t.Fatalf("page = %+v", page)
	}
	if page.Counts["all"] != 4 || page.Counts["open"] != 2 || page.Counts["review"] != 1 || page.Counts["done"] != 1 || page.Counts["archived"] != 0 {
		t.Fatalf("counts = %+v", page.Counts)
	}
	if !reflect.DeepEqual(page.Projects, []string{"atm", "legacy-atm", "other"}) {
		t.Fatalf("projects = %v", page.Projects)
	}

	all, err := ReadTodoPage(context.Background(), TodoPageQuery{Status: "all", Project: "ATM", Limit: 10})
	if err != nil || all.Total != 4 || len(all.Todos) != 4 || all.Todos[0].ID != "t11" {
		t.Fatalf("all = %+v, err = %v", all, err)
	}

	archived, err := ReadTodoPage(context.Background(), TodoPageQuery{Status: "archived", Limit: 10})
	if err != nil || archived.Total != 1 || len(archived.Todos) != 1 || archived.Todos[0].ID != "t9" || archived.Counts["all"] != 4 {
		t.Fatalf("archived = %+v, err = %v", archived, err)
	}
}
