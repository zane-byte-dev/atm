package store

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestNormalizeTodoCreatorAcceptsTheVocabularyAndRejectsTheRest(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"me", TodoCreatorMe},
		{" Me ", TodoCreatorMe},
		{"我", TodoCreatorMe},
		{"collect", TodoCreatorCollect},
		{"收集", TodoCreatorCollect},
		{"codex", "codex"},
		// Agent aliases resolve the same way they do for --agent, so a creator
		// and an agent filter can never name the same agent differently.
		{"claude-code", "claude"},
		{"grok", "grokbuild"},
	}
	for _, tc := range cases {
		got, err := NormalizeTodoCreator(tc.in)
		if err != nil {
			t.Fatalf("NormalizeTodoCreator(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeTodoCreator(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// A typo has to fail loudly: silently storing "codx" would make both the
	// creator column and every --creator filter quietly wrong.
	got, err := NormalizeTodoCreator("codx")
	if err == nil {
		t.Fatalf("NormalizeTodoCreator(\"codx\") = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), TodoCreatorCollect) {
		t.Errorf("error should list the vocabulary, got %q", err)
	}
}

func TestTodoCreatorDisplayNamesTheHumanAndLeavesAgentsAlone(t *testing.T) {
	oldOwner := config.OwnerName
	t.Cleanup(func() { config.OwnerName = oldOwner })

	config.OwnerName = ""
	if got := TodoCreatorDisplay(TodoCreatorMe); got != "我" {
		t.Errorf("unconfigured owner = %q, want 我", got)
	}
	config.OwnerName = "墨水"
	if got := TodoCreatorDisplay(TodoCreatorMe); got != "墨水（我）" {
		t.Errorf("configured owner = %q, want 墨水（我）", got)
	}
	if got := TodoCreatorDisplay(TodoCreatorCollect); got != "收集" {
		t.Errorf("collect = %q, want 收集", got)
	}
	if got := TodoCreatorDisplay("codex"); got != "codex" {
		t.Errorf("agent = %q, want codex", got)
	}
	// A todo filed before the field existed shows nothing rather than a name
	// nobody recorded.
	if got := TodoCreatorDisplay(""); got != "" {
		t.Errorf("missing creator = %q, want empty", got)
	}
}

func TestTodoCreatorSurvivesWriteAndRead(t *testing.T) {
	withTempStore(t)

	mine := openTodo("t1", "Filed by hand")
	mine.Creator = TodoCreatorMe
	collected := openTodo("t2", "Filed by collection")
	collected.Creator = TodoCreatorCollect
	legacy := openTodo("t3", "Filed before creator existed")
	seedTodos(t, mine, collected, legacy)

	tf, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("load todos: %v", err)
	}
	want := map[string]string{"t1": TodoCreatorMe, "t2": TodoCreatorCollect, "t3": ""}
	for _, todo := range tf.Items {
		if todo.Creator != want[todo.ID] {
			t.Errorf("%s creator = %q, want %q", todo.ID, todo.Creator, want[todo.ID])
		}
	}

	// Changing a creator is an update of the same row, not a new one.
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").Creator = "codex"
		return nil
	}); err != nil {
		t.Fatalf("update creator: %v", err)
	}
	tf, err = LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("reload todos: %v", err)
	}
	if got := FindTodo(tf, "t1").Creator; got != "codex" {
		t.Errorf("updated creator = %q, want codex", got)
	}
}

func TestTodoDocCarriesTheCreatorAndStaysLintClean(t *testing.T) {
	oldDir, oldOwner := config.AtmDir, config.OwnerName
	config.AtmDir = t.TempDir()
	config.OwnerName = "墨水"
	t.Cleanup(func() { config.AtmDir, config.OwnerName = oldDir, oldOwner })

	todo := Todo{ID: "t1", Title: "Creator in the card", Priority: "P1",
		Status: TodoStatusOpen, Project: "atm", Created: "2026-08-05", Creator: TodoCreatorMe}
	if _, err := InitTodoDoc(&todo); err != nil {
		t.Fatalf("init doc: %v", err)
	}
	doc, err := ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	if !strings.Contains(doc, "- **创建者**: me（我）") {
		t.Fatalf("card is missing the creator:\n%s", doc)
	}
	issues, err := LintTodoDoc(&TodoFile{Items: []Todo{todo}}, &todo, doc)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	for _, issue := range issues {
		if issue.Code == "doc_metadata_mismatch" {
			t.Fatalf("freshly written card reports metadata drift: %s", issue.Detail)
		}
	}

	// The card must not carry the nickname: renaming yourself changes nothing
	// about the record, and a card holding the old name would be reported as
	// drifting from the database until every card was rewritten.
	if strings.Contains(doc, "墨水") {
		t.Fatalf("card baked a mutable config value into a stored artifact:\n%s", doc)
	}
	config.OwnerName = "另一个名字"
	issues, err = LintTodoDoc(&TodoFile{Items: []Todo{todo}}, &todo, doc)
	if err != nil {
		t.Fatalf("lint after rename: %v", err)
	}
	for _, issue := range issues {
		if issue.Code == "doc_metadata_mismatch" {
			t.Fatalf("renaming the owner made an untouched card drift: %s", issue.Detail)
		}
	}
	config.OwnerName = "墨水"

	// The creator follows the database: a card written for one creator must not
	// keep it after the row changes.
	todo.Creator = "codex"
	if err := SyncTodoDocMetadata(&todo); err != nil {
		t.Fatalf("sync doc: %v", err)
	}
	doc, err = ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatalf("reread doc: %v", err)
	}
	if strings.Contains(doc, "me（我）") || !strings.Contains(doc, "- **创建者**: codex") {
		t.Fatalf("card kept a stale creator:\n%s", doc)
	}

	// A todo with no creator has no line at all, and that is not drift either.
	todo.Creator = ""
	if err := SyncTodoDocMetadata(&todo); err != nil {
		t.Fatalf("sync doc without creator: %v", err)
	}
	doc, err = ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatalf("reread doc: %v", err)
	}
	if strings.Contains(doc, "创建者") {
		t.Fatalf("card invented a creator:\n%s", doc)
	}
	issues, err = LintTodoDoc(&TodoFile{Items: []Todo{todo}}, &todo, doc)
	if err != nil {
		t.Fatalf("lint without creator: %v", err)
	}
	for _, issue := range issues {
		if issue.Code == "doc_metadata_mismatch" {
			t.Fatalf("missing creator reported as drift: %s", issue.Detail)
		}
	}
}
