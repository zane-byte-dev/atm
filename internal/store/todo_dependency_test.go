package store

import "testing"

func TestTodoDependenciesWakeWaitingTodoWhenAllAreDone(t *testing.T) {
	tf := &TodoFile{Items: []Todo{
		{ID: "t1", Title: "First prerequisite", Status: "done"},
		{ID: "t2", Title: "Second prerequisite", Status: "in_progress"},
		{ID: "t3", Title: "Dependent", Status: TodoStatusInProgress, WakeCondition: "waiting for todos: t1, t2"},
	}}
	if err := AddTodoDependency(tf, "t3", "t1"); err != nil {
		t.Fatalf("add first dependency: %v", err)
	}
	if err := AddTodoDependency(tf, "t3", "t2"); err != nil {
		t.Fatalf("add second dependency: %v", err)
	}
	if events := ReconcileTodoDependencies(tf); len(events) != 0 {
		t.Fatalf("unexpected wake events: %#v", events)
	}
	tf.Items[1].Status = "done"
	events := ReconcileTodoDependencies(tf)
	if len(events) != 1 || events[0].TodoID != "t3" {
		t.Fatalf("wake events = %#v", events)
	}
	dependent := FindTodo(tf, "t3")
	if dependent.Status != TodoStatusInProgress || dependent.WakeCondition != "" {
		t.Fatalf("dependent after wake = %#v", dependent)
	}
}

func TestTodoDependenciesRejectCyclesAndAuditMissingTargets(t *testing.T) {
	tf := &TodoFile{Items: []Todo{
		{ID: "t1", Status: "open"},
		{ID: "t2", Status: "open"},
	}}
	if err := AddTodoDependency(tf, "t1", "t2"); err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	if err := AddTodoDependency(tf, "t2", "t1"); err == nil {
		t.Fatal("expected dependency cycle error")
	}
	tf.Items[0].DependsOn = append(tf.Items[0].DependsOn, "t404")
	issues := AuditTodoDependencies(tf)
	if len(issues) != 1 || issues[0].Code != "dependency_missing" || issues[0].DependsOn != "t404" {
		t.Fatalf("issues = %#v", issues)
	}
}
