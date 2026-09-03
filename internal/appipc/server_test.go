package appipc

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/textmodel"
)

// Method names are the desktop protocol vocabulary. Keeping the exact list at
// the composition owner makes a move between files harmless while a wire rename
// remains an explicit protocol change.
func TestNamesAreTheDeclaredDesktopSet(t *testing.T) {
	want := []string{
		"agent.hook.install",
		"agent.hook.status",
		"agent.hook.uninstall",
		"collect.history",
		"collect.item.archive",
		"collect.item.correct",
		"collect.item.delete",
		"collect.item.promote",
		"collect.item.read",
		"collect.item.reprocess",
		"collect.item.revert",
		"collect.item.save_conclusion",
		"collect.run",
		"collect.snapshot",
		"collect.source.delete",
		"collect.source.enabled",
		"collect.source.muted",
		"collect.source.save",
		"collect.source.search",
		"config.credential.delete",
		"config.credential.save",
		"config.save",
		"config.settings",
		"config.text_model.check",
		"dashboard.snapshot",
		"day.data.delete",
		"day.data.export",
		"day.feedback",
		"day.privacy.set",
		"day.show",
		"day.snapshot",
		"day.source.delete",
		"day.source.set",
		"doctor.check",
		"guard.list",
		"guard.rule.list",
		"guard.status",
		"knowledge.catalog",
		"knowledge.collection.delete",
		"knowledge.collection.save",
		"knowledge.document.delete",
		"knowledge.document.get",
		"knowledge.document.import",
		"knowledge.document.save",
		"knowledge.feedback",
		"knowledge.governance",
		"knowledge.query",
		"memory.recall",
		"memory.supersede",
		"quota.snapshot",
		"session.list",
		"session.search",
		"session.show",
		"session.timeline",
		"todo.advice",
		"todo.archive",
		"todo.create",
		"todo.delete",
		"todo.doc",
		"todo.done",
		"todo.link.remove",
		"todo.link.save",
		"todo.list",
		"todo.refine",
		"todo.restore",
		"todo.show",
		"todo.start",
		"todo.title",
		"todo.update",
	}
	got := New(Dependencies{}).Names()
	if !slices.Equal(got, want) {
		t.Fatalf("methods = %v, want %v", got, want)
	}
}

func TestTextModelMethodUsesInjectedChecker(t *testing.T) {
	var received textmodel.ConnectionCheckInput
	server := New(Dependencies{
		CheckTextModel: func(
			_ context.Context,
			input textmodel.ConnectionCheckInput,
		) (textmodel.CheckResult, error) {
			received = input
			return textmodel.CheckResult{OK: true, LatencyMS: 7}, nil
		},
	})
	var output bytes.Buffer
	err := server.Serve(
		context.Background(),
		"config.text_model.check",
		strings.NewReader(`{"api_key":"secret","base_url":"https://example.test","model":"draft"}`),
		&output,
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if received.APIKey != "secret" || received.BaseURL != "https://example.test" || received.Model != "draft" {
		t.Fatalf("checker input = %+v", received)
	}
	var envelope struct {
		Data textmodel.CheckResult `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output.String())
	}
	if !envelope.Data.OK || envelope.Data.LatencyMS != 7 {
		t.Fatalf("response = %+v", envelope.Data)
	}
}

func TestTodoTitleMethodUsesInjectedGenerator(t *testing.T) {
	var received string
	server := New(Dependencies{
		GenerateTodoTitle: func(_ context.Context, description string) (string, error) {
			received = description
			return "归档并入任务分组", nil
		},
	})
	var output bytes.Buffer
	err := server.Serve(
		context.Background(),
		"todo.title",
		strings.NewReader(`{"description":"把归档入口移动到任务分组下"}`),
		&output,
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if received != "把归档入口移动到任务分组下" {
		t.Fatalf("generator description = %q", received)
	}
	var envelope struct {
		Data TodoTitleResponse `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output.String())
	}
	if envelope.Data.Title != "归档并入任务分组" {
		t.Fatalf("response = %+v", envelope.Data)
	}
}
