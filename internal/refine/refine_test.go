package refine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestPrepareSimplePolish(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "把那个发布检查修一下", Description: "老是红", Status: store.TodoStatusOpen}
	prepared, err := Prepare(todo, 0, Proposal{
		Title:       "修复发布检查失败",
		Description: "目标：发布检查恢复绿色。\n约束：不改发布流程。\n验收：make release-check 通过。",
		Complexity:  ComplexitySimple,
		Reason:      "单一交付",
	}, Options{AllowSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.TitleChanged || prepared.Title != "修复发布检查失败" {
		t.Fatalf("title = %+v", prepared)
	}
	if !prepared.DescChanged || !strings.Contains(prepared.Description, "验收") {
		t.Fatalf("description = %+v", prepared)
	}
	if prepared.Split || prepared.SplitSkip == "" {
		t.Fatalf("simple work should not split: %+v", prepared)
	}
}

func TestPrepareSplitsComplexOpenTodo(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "做完整的收集闭环", Status: store.TodoStatusOpen}
	prepared, err := Prepare(todo, 0, Proposal{
		Title:       "实现收集闭环",
		Description: "目标：从聊天到 Todo 可回放。",
		Complexity:  ComplexityComplex,
		Plan:        "先契约，再落地，最后补测试。",
		Reason:      "三块可独立关闭的工作",
		Children: []Child{
			{Title: "写分类器契约", Description: "schema 与 prompt"},
			{Title: "实现落地路径", Description: "create/append", DependsOnIndexes: []int{0}},
			{Title: "补回归测试", Description: "覆盖降级", DependsOnIndexes: []int{1}},
		},
	}, Options{AllowSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Split || len(prepared.Children) != 3 {
		t.Fatalf("prepared = %+v", prepared)
	}
	if got := prepared.Children[2].DependsOnIndexes; len(got) != 1 || got[0] != 1 {
		t.Fatalf("child deps = %#v", prepared.Children)
	}
}

func TestPrepareDoesNotSplitInProgress(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "做完整的收集闭环", Status: store.TodoStatusInProgress}
	prepared, err := Prepare(todo, 0, complexProposal(), Options{AllowSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Split || prepared.SplitSkip == "" {
		t.Fatalf("in_progress was split: %+v", prepared)
	}
}

func TestPrepareDoesNotSplitWhenChildrenExist(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "做完整的收集闭环", Status: store.TodoStatusOpen}
	prepared, err := Prepare(todo, 2, complexProposal(), Options{AllowSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Split {
		t.Fatal("re-refine minted more children")
	}
}

func TestPrepareNoSplitFlag(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "做完整的收集闭环", Status: store.TodoStatusOpen}
	prepared, err := Prepare(todo, 0, complexProposal(), Options{AllowSplit: false})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Split {
		t.Fatal("--no-split still split")
	}
}

func TestPrepareRejectsReservedHeading(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "修发布检查", Status: store.TodoStatusOpen}
	_, err := Prepare(todo, 0, Proposal{
		Title:       "修复发布检查",
		Description: "正文\n## 分析\n会被当成卡片标题",
		Complexity:  ComplexitySimple,
	}, Options{})
	if err == nil {
		t.Fatal("reserved heading was accepted")
	}
}

func TestPrepareKeepsOriginalWhenTitleIsUnusable(t *testing.T) {
	todo := store.Todo{ID: "t1", Title: "修复发布检查失败", Status: store.TodoStatusOpen}
	prepared, err := Prepare(todo, 0, Proposal{Title: "ab", Complexity: ComplexitySimple}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.TitleChanged || prepared.Title != todo.Title {
		t.Fatalf("short title leaked: %+v", prepared)
	}
}

func TestNormalizeChildrenRemapsDependenciesAfterDrops(t *testing.T) {
	children := normalizeChildren([]Child{
		{Title: "x"}, // dropped: too short
		{Title: "写分类器契约"},
		{Title: "实现落地路径", DependsOnIndexes: []int{1, 9}},
	}, 5)
	if len(children) != 2 {
		t.Fatalf("children = %#v", children)
	}
	if got := children[1].DependsOnIndexes; len(got) != 1 || got[0] != 0 {
		t.Fatalf("remapped deps = %#v", children[1].DependsOnIndexes)
	}
}

func TestFormatAnalysisNamesCreatedChildren(t *testing.T) {
	note := FormatAnalysis(Prepared{
		Complexity: ComplexityComplex,
		Reason:     "三块独立工作",
		Plan:       "按依赖顺序做。",
		Children: []Child{
			{Title: "写分类器契约"},
			{Title: "实现落地路径", DependsOnIndexes: []int{0}},
		},
		Split: true,
	}, []store.Todo{{ID: "t2", Title: "写分类器契约"}, {ID: "t3", Title: "实现落地路径"}})
	if !strings.Contains(note, "t2") || !strings.Contains(note, "t3") || !strings.Contains(note, "依赖 t2") {
		t.Fatalf("analysis = %q", note)
	}
}

func TestAnalyzeDecodesStubbedModelJSON(t *testing.T) {
	old := runModel
	t.Cleanup(func() { runModel = old })
	runModel = func(_ context.Context, _ string, _ time.Duration, _, _, _ string) ([]byte, error) {
		return json.Marshal(Proposal{
			Title:       "修复发布检查失败",
			Description: "目标：检查变绿。",
			Complexity:  ComplexitySimple,
			Plan:        "",
			Reason:      "一事一做",
			Children:    []Child{},
		})
	}
	todo := store.Todo{ID: "t1", Title: "修一下那个红的", Status: store.TodoStatusOpen}
	prepared, _, err := Analyze(context.Background(), todo, "", 0, Options{AllowSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.TitleChanged || prepared.Title != "修复发布检查失败" || prepared.Split {
		t.Fatalf("prepared = %+v", prepared)
	}
}

func TestCanRefineRejectsClosed(t *testing.T) {
	if err := CanRefine(store.Todo{ID: "t1", Status: store.TodoStatusDone}); err == nil {
		t.Fatal("done todo was refinable")
	}
}

func TestChildSourceRoundTrip(t *testing.T) {
	tf := &store.TodoFile{Items: []store.Todo{
		{ID: "t2", Source: ChildSource("t1")},
		{ID: "t3", Source: "elsewhere"},
	}}
	got := ExistingChildren(tf, "t1")
	if len(got) != 1 || got[0].ID != "t2" {
		t.Fatalf("children = %#v", got)
	}
}

func TestPromptIncludesTitleAndForbidsInvention(t *testing.T) {
	prompt := Prompt(store.Todo{ID: "t9", Title: "修一下那个红的", Project: "atm", Status: "open", Priority: "P1"}, "# 修一下那个红的")
	for _, want := range []string{"t9", "修一下那个红的", "Do not invent", "atm"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func complexProposal() Proposal {
	return Proposal{
		Title:       "实现收集闭环",
		Description: "目标：从聊天到 Todo。",
		Complexity:  ComplexityComplex,
		Plan:        "分三步。",
		Children: []Child{
			{Title: "写分类器契约"},
			{Title: "实现落地路径"},
		},
	}
}
