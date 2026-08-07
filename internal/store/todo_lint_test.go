package store

import (
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestValidateTodoLogMessageConstrainsProgressOnly(t *testing.T) {
	if err := ValidateTodoLogMessage(strings.Repeat("界", TodoProgressMaxRunes), "进展"); err != nil {
		t.Fatalf("valid progress rejected: %v", err)
	}
	if err := ValidateTodoLogMessage(strings.Repeat("界", TodoProgressMaxRunes+1), "进展"); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("long progress error = %v", err)
	}
	if err := ValidateTodoLogMessage("result\nevidence", "进展"); err == nil || !strings.Contains(err.Error(), "one paragraph") {
		t.Fatalf("multiline progress error = %v", err)
	}
	if err := ValidateTodoLogMessage(strings.Repeat("detail\n", 100), "分析"); err != nil {
		t.Fatalf("analysis detail rejected: %v", err)
	}
}

// Appending to the generated section used to succeed and then vanish on the next
// metadata sync, so the refusal is the whole point: a rejected write is visible,
// a discarded one is not.
func TestValidateTodoLogMessageRefusesTheGeneratedSection(t *testing.T) {
	err := ValidateTodoLogMessage("补充需求：还要支持并发", todoDocGeneratedSection)
	if err == nil || !strings.Contains(err.Error(), "would be overwritten") {
		t.Fatalf("append to %s error = %v", todoDocGeneratedSection, err)
	}
	if err := ValidateTodoLogMessage("人补充：还要支持并发", "补充"); err != nil {
		t.Fatalf("append to a user-maintained section rejected: %v", err)
	}
}

// Referring to finished work is normal, so archived todos count as known even
// though they are no longer in the working set.
func TestUnknownTodoReferencesAcceptsArchivedTodos(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Live"), Todo{
		ID: "t2", Title: "Archived", Priority: "P1", Status: TodoStatusDone, Created: Today(),
	})
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos.Items) != 1 || todos.Items[0].ID != "t1" {
		t.Fatalf("archived todo still in the working set: %#v", todos.Items)
	}
	unknown := UnknownTodoReferences(todos, "t1 and t2 exist; t404 does not")
	if len(unknown) != 1 || unknown[0] != "t404" {
		t.Fatalf("unknown references = %#v", unknown)
	}
}

func TestLintTodoDocFindsVerboseAndInconsistentProgress(t *testing.T) {
	oldDir := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = oldDir })

	todo := Todo{
		ID:          "t65",
		Title:       "Current title",
		Description: "子任务：\n- [ ] 阶段①",
		Priority:    "P2",
		Status:      "in_progress",
		Project:     "wanda",
		Created:     "2026-07-09",
	}
	long := strings.Repeat("细节", 520)
	doc := `# Old title

- **ID**: t65
- **状态**: in_progress（进行中）
- **优先级**: P2
- **注意力**: waiting
- **领域**: work
- **项目**: wanda
- **创建**: 2026-07-09

## 需求

旧需求

## 分析

待补充

## 进展
- [2026-07-16 09:46] 阶段①准备：` + long + ` t76
- [2026-07-16 10:12] 阶段①完成：已完成

## 备注
`
	issues, err := LintTodoDoc(&TodoFile{Items: []Todo{todo}}, &todo, doc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"doc_metadata_mismatch":                   false,
		"doc_requirement_mismatch":                false,
		"progress_too_long":                       false,
		"unknown_todo_reference":                  false,
		"analysis_missing_with_large_progress":    false,
		"unchecked_checklist_with_completion_log": false,
		"rapid_repeated_phase_logs":               false,
	}
	for _, issue := range issues {
		if _, ok := want[issue.Code]; ok {
			want[issue.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing lint issue %s in %#v", code, issues)
		}
	}
}

// A description with its own section structure is the case the requirement check
// exists to encourage, so it must not be the case that trips it. Boundaries stop
// at the card's own sections, and a "## " inside a fenced block is content.
func TestLintTodoDocAcceptsDescriptionsWithTheirOwnHeadings(t *testing.T) {
	oldDir := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = oldDir })

	description := "## 问题\n\n价格表只覆盖 9 个模型。\n\n" +
		"### 根因\n\n未命中时静默落到默认值。\n\n" +
		"## 验收\n\n```go\nmarker := \"## \" + section\nif next := strings.Index(rest, \"\\n## \"); next >= 0 {\n```\n\n" +
		"上面代码块里的井号不是标题。"
	todo := Todo{
		ID: "t1", Title: "Detailed requirement", Description: description,
		Priority: "P1", Status: "open", Project: "atm", Created: "2026-07-29",
	}
	if _, err := InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	doc, err := ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := LintTodoDoc(&TodoFile{Items: []Todo{todo}}, &todo, doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range issues {
		if issue.Code == "doc_requirement_mismatch" {
			t.Fatalf("false positive on a structured description:\n%s", doc)
		}
	}
	// The check still has teeth: editing the card's 需求 body by hand is exactly
	// the second source of truth it is meant to catch.
	edited := strings.Replace(doc, "价格表只覆盖 9 个模型。", "手工改过的需求。", 1)
	issues, err = LintTodoDoc(&TodoFile{Items: []Todo{todo}}, &todo, edited)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range issues {
		if issue.Code == "doc_requirement_mismatch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hand-edited requirement went undetected in %#v", issues)
	}
}

// The only thing a description may not do is reuse the card's own section
// headings verbatim, because nothing in the card could then tell the two apart.
// Everything nearby stays legal, so the restriction costs a structured
// requirement nothing.
func TestValidateTodoDescriptionReservesOnlyTheCardsOwnHeadings(t *testing.T) {
	for _, description := range []string{
		"## 分析\n\n正文",
		"##  进展  ",
		"前言\n\n## 备注\n\n正文",
		"## 需求\n\n正文",
	} {
		if err := ValidateTodoDescription(description); err == nil {
			t.Errorf("accepted a description colliding with a card section: %q", description)
		}
	}
	for _, description := range []string{
		"### 分析\n\n更深一级不冲突",
		"## 分析方案\n\n不是精确匹配",
		"## 问题\n\n不是卡片小节名",
		"正文里提到 ## 分析 但不在行首",
		"```\n## 分析\n```\n围栏里的井号是内容",
		"",
	} {
		if err := ValidateTodoDescription(description); err != nil {
			t.Errorf("rejected a legal description %q: %v", description, err)
		}
	}
}

// The writer shares those boundaries, so a sync replaces the whole 需求 body
// instead of the part above the description's first heading. It used to leave the
// old text in place and add the new text before it, so each sync grew the card by
// another copy.
func TestSyncTodoDocMetadataReplacesAStructuredRequirementInPlace(t *testing.T) {
	oldDir := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = oldDir })

	todo := Todo{
		ID: "t1", Title: "T", Description: "## 问题\n\n旧的问题描述。\n\n## 目标\n\n旧的目标。",
		Priority: "P1", Status: "open", Project: "atm", Created: "2026-07-29",
	}
	if _, err := InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	todo.Description = "## 问题\n\n新的问题描述。\n\n## 目标\n\n新的目标。"
	for i := 0; i < 3; i++ {
		if err := SyncTodoDocMetadata(&todo); err != nil {
			t.Fatal(err)
		}
	}
	content, err := ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "旧的问题描述") {
		t.Errorf("old requirement survived the sync:\n%s", content)
	}
	if got := strings.Count(content, "新的问题描述"); got != 1 {
		t.Errorf("requirement appears %d times, want 1:\n%s", got, content)
	}
	// The sections after 需求 are user-maintained and must survive untouched.
	for _, section := range []string{"## 分析", "## 进展", "## 备注"} {
		if got := strings.Count(content, section); got != 1 {
			t.Errorf("%s appears %d times, want 1:\n%s", section, got, content)
		}
	}
}

// 进展 is parsed with the same helper, so a description or analysis section
// carrying "## " headings must not bleed its lines into the progress entries.
func TestParseTodoProgressEntriesIgnoresHeadingsInOtherSections(t *testing.T) {
	doc := `# T

## 需求

## 问题

不该被当成进展。

## 分析

## 方案

也不该。

## 进展
- [2026-07-29 10:00] 第一条
- [2026-07-29 11:00] 第二条

## 备注

尾注不是进展。
`
	entries := parseTodoProgressEntries(doc)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Text != "第一条" || entries[1].Text != "第二条" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestSyncTodoDocMetadataUsesStructuredTodoAsSource(t *testing.T) {
	oldDir := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = oldDir })

	todo := Todo{ID: "t1", Title: "Old", Description: "Old requirement", Priority: "P2", Status: "open", Project: "old", Created: "2026-07-01"}
	if _, err := InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	content, err := ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	content = strings.Replace(content, "待补充\n\n## 进展", "Architecture stays here.\n\n## 进展", 1)
	content = strings.Replace(content, "## 进展\n", "## 进展\n- [2026-07-01 10:00] Existing milestone\n", 1)
	if err := os.WriteFile(TodoDocPath(todo.ID), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	todo.Title = "New title"
	todo.Description = "New requirement"
	todo.Priority = "P1"
	todo.Status = "in_progress"
	todo.Tags = []string{TodoTagMaintenance}
	todo.Project = "atm"
	if err := SyncTodoDocMetadata(&todo); err != nil {
		t.Fatal(err)
	}
	content, err = ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"# New title",
		"- **优先级**: P1",
		"- **标签**: maintenance",
		"- **项目**: atm",
		// The section is generated from Todo.Description, so it carries a notice
		// telling the reader not to edit it by hand.
		"## 需求\n\n" + todoDocRequirementNotice + "\n\nNew requirement",
		"Architecture stays here.",
		"Existing milestone",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("synced doc missing %q:\n%s", expected, content)
		}
	}
}

// 领域 was retired with schema v35. Documents written before then still carry the
// line, and the structured Todo is the source of truth for the header — so a sync
// has to sweep the field out rather than leave a value nothing can update.
func TestSyncTodoDocMetadataDropsRetiredLaneField(t *testing.T) {
	oldDir := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = oldDir })

	todo := Todo{ID: "t1", Title: "Keep", Priority: "P2", Status: "open", Project: "atm", Created: "2026-07-01"}
	if _, err := InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	content, err := ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	content = strings.Replace(content, "- **项目**: atm", "- **领域**: work\n- **项目**: atm", 1)
	if err := os.WriteFile(TodoDocPath(todo.ID), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SyncTodoDocMetadata(&todo); err != nil {
		t.Fatal(err)
	}
	content, err = ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "领域") {
		t.Errorf("synced doc still carries 领域:\n%s", content)
	}
	if !strings.Contains(content, "- **项目**: atm") {
		t.Errorf("synced doc lost 项目:\n%s", content)
	}
}
