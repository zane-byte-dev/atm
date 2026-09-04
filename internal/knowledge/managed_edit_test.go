package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestManagedEditProcessHelper(t *testing.T) {
	dir := os.Getenv("ATM_KNOWLEDGE_EDIT_TEST_DIR")
	if dir == "" {
		return
	}
	var input UpdateManagedInput
	if err := json.Unmarshal([]byte(os.Getenv("ATM_KNOWLEDGE_EDIT_TEST_INPUT")), &input); err != nil {
		os.Exit(2)
	}
	_, err := UpdateManaged(context.Background(), dir, input)
	if errors.Is(err, application.ErrConflict) {
		os.Exit(23)
	}
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestManagedEditCASAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	document, err := Add(dir, AddDocumentInput{Title: "Processes", Content: "Initial", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := []*exec.Cmd{}
	for _, body := range []string{"Process one", "Process two"} {
		input, _ := json.Marshal(UpdateManagedInput{DocumentID: document.Metadata.ID, ETag: VersionDocument(*document).ETag, Content: body})
		command := exec.Command(executable, "-test.run=^TestManagedEditProcessHelper$")
		command.Env = append(os.Environ(), "ATM_KNOWLEDGE_EDIT_TEST_DIR="+dir, "ATM_KNOWLEDGE_EDIT_TEST_INPUT="+string(input))
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, command)
	}
	success, conflict := 0, 0
	for _, command := range commands {
		err := command.Wait()
		if err == nil {
			success++
			continue
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 23 {
			conflict++
			continue
		}
		t.Fatal(err)
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestManagedEditConflictsAcrossIndependentWriters(t *testing.T) {
	dir := t.TempDir()
	document, err := Add(dir, AddDocumentInput{Title: "Shared", Content: "Original", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	expected := VersionDocument(*document).ETag
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, content := range []string{"First writer", "Second writer"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: document.Metadata.ID, ETag: expected, Content: content})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, application.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	current, err := Get(dir, document.Metadata.ID)
	if err != nil || (current.Content != "First writer" && current.Content != "Second writer") {
		t.Fatalf("lost winning edit: %+v, %v", current, err)
	}
	before := VersionDocument(*current).ETag
	if _, err := Update(dir, document.Metadata.ID, "Changed from CLI"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: document.Metadata.ID, ETag: before, Content: "Stale Web draft"}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("CLI change overwritten: %v", err)
	}
}

func TestManagedEditKeepsIdentityPathAndImportedSourceReadOnly(t *testing.T) {
	dir := t.TempDir()
	document, err := Add(dir, AddDocumentInput{Title: "Original", Content: "Before", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	title, status := "Renamed", "draft"
	result, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: document.Metadata.ID, ETag: VersionDocument(*document).ETag, Content: "After", Title: &title, Status: &status})
	if err != nil || result.Path != document.Path || result.Metadata.ID != document.Metadata.ID || result.Metadata.Title != title || result.Metadata.Status != status || result.ETag == VersionDocument(*document).ETag {
		t.Fatalf("edit=%+v err=%v", result, err)
	}
	external := filepath.Join(t.TempDir(), "external.md")
	if err := os.WriteFile(external, []byte("# Source\n\nKeep me\n"), 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := Import(dir, external, AddDocumentInput{Collection: "notes"})
	if err != nil || len(imported) != 1 {
		t.Fatalf("import=%+v err=%v", imported, err)
	}
	versioned := VersionDocument(imported[0])
	if versioned.Editable {
		t.Fatal("imported source advertised editable")
	}
	if _, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: versioned.Metadata.ID, ETag: versioned.ETag, Content: "Unauthorized replacement"}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("imported write accepted: %v", err)
	}
	content, _ := os.ReadFile(external)
	if string(content) != "# Source\n\nKeep me\n" {
		t.Fatal("external source changed")
	}
}
