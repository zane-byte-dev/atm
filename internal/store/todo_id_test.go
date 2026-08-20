package store

import (
	"strings"
	"testing"
)

// IDs are stored as `t<digits>`, but that is not how they get typed. A chat quote
// carries `#t65`, a list read aloud becomes `65`, and a shell that capitalised the
// argument hands over `T65`. All three used to fail as "todo not found", which
// reads like the todo is gone rather than like the spelling was rejected.
func TestNormalizeTodoIDAcceptsHowPeopleActuallyWriteIt(t *testing.T) {
	for _, testCase := range []struct {
		input string
		want  string
	}{
		{"t65", "t65"},
		{"65", "t65"},
		{"#t65", "t65"},
		{"#65", "t65"},
		{"T65", "t65"},
		{" t65 ", "t65"},
		{"\tt65\n", "t65"},
		// Leading zeros would otherwise make t007 and t7 different keys.
		{"007", "t7"},
		{"t007", "t7"},
	} {
		if got := NormalizeTodoID(testCase.input); got != testCase.want {
			t.Errorf("NormalizeTodoID(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

// Anything that is not a todo reference comes back untouched, so the error the
// caller sees still quotes what they typed.
func TestNormalizeTodoIDLeavesNonReferencesAlone(t *testing.T) {
	for _, input := range []string{"", "abc", "t", "#", "t65x", "65t", "t6 5", "-65", "t-65", "0", "t0", "#t"} {
		if got := NormalizeTodoID(input); got != strings.TrimSpace(input) {
			t.Errorf("NormalizeTodoID(%q) = %q, want it unchanged", input, got)
		}
	}
}

func TestFindTodoResolvesEverySpelling(t *testing.T) {
	file := &TodoFile{Items: []Todo{{ID: "t65", Title: "Ship the gate"}, {ID: "t7", Title: "Other"}}}
	for _, input := range []string{"t65", "65", "#t65", "#65", "T65", " t65"} {
		found := FindTodo(file, input)
		if found == nil {
			t.Fatalf("FindTodo(%q) = nil", input)
		}
		if found.ID != "t65" {
			t.Errorf("FindTodo(%q) resolved to %s, want t65", input, found.ID)
		}
	}
	if FindTodo(file, "t66") != nil {
		t.Error("FindTodo resolved an id that does not exist")
	}
}

// The archived index is keyed by canonical id, so it has to be consulted with a
// normalised key too — otherwise `atm todo show 1` reports a missing todo instead
// of the archived one it actually found, and loses the restore instruction.
func TestArchivedStatusAndErrorSurviveAShortID(t *testing.T) {
	file := &TodoFile{archived: map[string]string{"t1": "done"}}
	if status, archived := ArchivedStatus(file, "1"); !archived || status != "done" {
		t.Fatalf("ArchivedStatus(1) = %q, %v; want done, true", status, archived)
	}
	err := TodoNotFoundError(file, "#1")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The suggested command must be paste-ready, so it names t1 rather than #1.
	want := "atm todo restore t1"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not suggest %q", err.Error(), want)
	}
}

// The not-found message keeps the caller's own spelling: normalising it there
// would answer a question they did not ask.
func TestNotFoundErrorEchoesWhatWasTyped(t *testing.T) {
	err := TodoNotFoundError(&TodoFile{}, "007")
	if !strings.Contains(err.Error(), "007") {
		t.Errorf("error %q does not quote the input", err.Error())
	}
}
