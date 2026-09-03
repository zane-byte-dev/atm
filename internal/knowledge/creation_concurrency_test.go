package knowledge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAddConcurrentProcessesPreserveBothDocuments(t *testing.T) {
	dataDir := t.TempDir()
	results := runKnowledgeCreationProcesses(t, dataDir, "add")
	paths := make(map[string]bool)
	ids := make(map[string]bool)
	for _, result := range results {
		if result.Error != "" || result.Document == nil {
			t.Fatalf("worker %s: document = %#v, error = %s", result.Worker, result.Document, result.Error)
		}
		loaded, err := Get(dataDir, result.Document.Metadata.ID)
		if err != nil {
			t.Fatalf("read worker %s returned document: %v", result.Worker, err)
		}
		if loaded.Content != creationWorkerContent(result.Worker) || loaded.Metadata.Title != "Shared title" || loaded.Collection != "shared" {
			t.Fatalf("worker %s document was not preserved: %#v", result.Worker, loaded)
		}
		if loaded.Path != result.Path || ids[loaded.Metadata.ID] || paths[loaded.Path] {
			t.Fatalf("workers did not retain distinct returned IDs and paths: %#v", results)
		}
		ids[loaded.Metadata.ID], paths[loaded.Path] = true, true
	}
	assertCreationDirectoryEntries(t, filepath.Join(knowledgeRoot(dataDir), "shared"), 2)
}

func TestCreateDocumentAtConcurrentCollisionPreservesBothDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Shared title.md")
	documents := []*Document{
		{Metadata: DocumentMetadata{ID: "document:first", SchemaVersion: KnowledgeSchemaVersion, Title: "Shared title"}, Content: creationWorkerContent("first")},
		{Metadata: DocumentMetadata{ID: "document:second", SchemaVersion: KnowledgeSchemaVersion, Title: "Shared title"}, Content: creationWorkerContent("second")},
	}
	start := make(chan struct{})
	results := make(chan error, len(documents))
	for _, document := range documents {
		go func(document *Document) {
			<-start
			// Both callers already chose the same destination. One must retry
			// publication even if the goroutines are scheduled serially.
			results <- createDocumentAt(path, document)
		}(document)
	}
	close(start)
	for range documents {
		if err := <-results; err != nil {
			t.Fatalf("create at colliding destination: %v", err)
		}
	}
	if documents[0].Path == documents[1].Path || (documents[0].Path != path && documents[1].Path != path) {
		t.Fatalf("expected the original destination and a distinct retry: %q, %q", documents[0].Path, documents[1].Path)
	}
	for _, document := range documents {
		loaded, err := readDocument(document.Path)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Metadata.ID != document.Metadata.ID || loaded.Content != document.Content {
			t.Fatalf("document %s was overwritten: %#v", document.Metadata.ID, loaded)
		}
	}
	assertCreationDirectoryEntries(t, filepath.Dir(path), 2)
}

func TestCreateCollectionConcurrentProcessesHaveOneWinner(t *testing.T) {
	dataDir := t.TempDir()
	results := runKnowledgeCreationProcesses(t, dataDir, "collection")
	var winner *CollectionInfo
	for _, result := range results {
		if result.Error != "" {
			if result.Collection != nil || !strings.Contains(result.Error, "already exists") {
				t.Fatalf("unexpected losing result: %#v", result)
			}
			continue
		}
		if winner != nil || result.Collection == nil {
			t.Fatalf("expected exactly one successful create: %#v", results)
		}
		winner = result.Collection
	}
	if winner == nil {
		t.Fatalf("neither collection creation succeeded: %#v", results)
	}
	dir := filepath.Join(knowledgeRoot(dataDir), "shared")
	stored, err := readCollectionManifest(filepath.Join(dir, "_collection.md"), "shared")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, winner) {
		t.Fatalf("winner metadata was overwritten: got %#v, want %#v", stored, winner)
	}
	assertCreationDirectoryEntries(t, dir, 1)
}

func TestCreateCollectionRejectsExistingNonemptyDirectory(t *testing.T) {
	for _, withManifest := range []bool{false, true} {
		t.Run(fmt.Sprintf("manifest=%t", withManifest), func(t *testing.T) {
			dataDir := t.TempDir()
			dir := filepath.Join(knowledgeRoot(dataDir), "shared")
			if withManifest {
				if _, err := CreateCollection(dataDir, "shared", EditCollectionInput{Name: stringPointer("Original"), Description: stringPointer("Keep this metadata")}); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(dir, "unrelated"), 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(dir, "unrelated", "sentinel.txt")
			if err := os.WriteFile(sentinel, []byte("existing unrelated content\n"), 0600); err != nil {
				t.Fatal(err)
			}
			paths := []string{sentinel}
			if withManifest {
				paths = append(paths, filepath.Join(dir, "_collection.md"))
			}
			before := make(map[string][]byte)
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = data
			}
			if created, err := CreateCollection(dataDir, "shared", EditCollectionInput{Name: stringPointer("Replacement")}); err == nil || created != nil {
				t.Fatalf("existing directory accepted: created = %#v, error = %v", created, err)
			}
			for path, want := range before {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("existing content changed at %s: data = %q, error = %v", path, got, err)
				}
			}
			assertCreationDirectoryEntries(t, dir, len(paths))
		})
	}
}

func TestAtomicWriteNewRejectsExistingEntriesWithoutTempLeaks(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "destination")
			contentPath := path
			switch kind {
			case "symlink":
				contentPath = filepath.Join(dir, "target")
				if err := os.Symlink(contentPath, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				contentPath = filepath.Join(path, "sentinel")
			}
			want := []byte("existing bytes must survive\n")
			if err := os.WriteFile(contentPath, want, 0640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := atomicWriteNew(path, []byte("replacement"), 0600); !errors.Is(err, os.ErrExist) {
				t.Fatalf("existing %s error = %v, want os.ErrExist", kind, err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.ModTime() != after.ModTime() {
				t.Fatalf("existing %s entry changed: before = %#v, after = %#v, error = %v", kind, before, after, err)
			}
			got, err := os.ReadFile(contentPath)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("existing %s contents changed: got %q, error = %v", kind, got, err)
			}
			wantEntries := 1
			if kind == "symlink" {
				wantEntries = 2
			}
			assertCreationDirectoryEntries(t, dir, wantEntries)
		})
	}
}

type knowledgeCreationResult struct {
	Ready      bool            `json:"ready,omitempty"`
	Worker     string          `json:"worker,omitempty"`
	Document   *Document       `json:"document,omitempty"`
	Collection *CollectionInfo `json:"collection,omitempty"`
	Path       string          `json:"path,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// Only this explicitly selected helper test runs in each child process. The
// operations under test use the supplied temporary corpus and never open a DB.
func TestKnowledgeCreationProcess(t *testing.T) {
	operation := os.Getenv("ATM_KNOWLEDGE_CREATION_TEST_OPERATION")
	if operation == "" {
		t.Skip("subprocess helper")
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(knowledgeCreationResult{Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	dataDir := os.Getenv("ATM_KNOWLEDGE_CREATION_TEST_DIR")
	worker := os.Getenv("ATM_KNOWLEDGE_CREATION_TEST_WORKER")
	result := knowledgeCreationResult{Worker: worker}
	var err error
	switch operation {
	case "add":
		result.Document, err = Add(dataDir, AddDocumentInput{Title: "Shared title", Content: creationWorkerContent(worker), Collection: "shared"})
		if result.Document != nil {
			result.Path = result.Document.Path
		}
	case "collection":
		result.Collection, err = CreateCollection(dataDir, "shared", EditCollectionInput{
			Name: stringPointer("Collection " + worker), Description: stringPointer("Metadata from " + worker),
			Role: stringPointer("Role " + worker), Topics: stringsPointer([]string{"topic-" + worker}),
			UseWhen: stringsPointer([]string{"Use for " + worker}), AvoidWhen: stringsPointer([]string{"Avoid except " + worker}),
			Instructions: stringsPointer([]string{"Use evidence from " + worker}),
		})
	default:
		t.Fatalf("unknown creation operation %q", operation)
	}
	if err != nil {
		result.Error = err.Error()
	}
	if err := encoder.Encode(result); err != nil {
		t.Fatal(err)
	}
}

func runKnowledgeCreationProcesses(t *testing.T, dataDir, operation string) []knowledgeCreationResult {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	type process struct {
		cmd     *exec.Cmd
		stdin   io.WriteCloser
		stdout  io.ReadCloser
		decoder *json.Decoder
		stderr  bytes.Buffer
		waited  bool
	}
	processes := make([]*process, 0, 2)
	for index := 0; index < 2; index++ {
		p := &process{cmd: exec.CommandContext(ctx, executable, "-test.run=^TestKnowledgeCreationProcess$")}
		p.cmd.Env = append(os.Environ(), "ATM_KNOWLEDGE_CREATION_TEST_OPERATION="+operation,
			"ATM_KNOWLEDGE_CREATION_TEST_DIR="+dataDir, fmt.Sprintf("ATM_KNOWLEDGE_CREATION_TEST_WORKER=%d", index))
		p.cmd.Stderr = &p.stderr
		p.stdin, err = p.cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		p.stdout, err = p.cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		p.decoder = json.NewDecoder(p.stdout)
		if err := p.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if !p.waited {
				_ = p.cmd.Process.Kill()
				_ = p.cmd.Wait()
			}
		})
		processes = append(processes, p)
	}
	for _, p := range processes {
		var ready knowledgeCreationResult
		if err := p.decoder.Decode(&ready); err != nil || !ready.Ready {
			t.Fatalf("child process readiness: %#v, error = %v", ready, err)
		}
	}
	for _, p := range processes {
		if _, err := io.WriteString(p.stdin, "start\n"); err != nil {
			t.Fatal(err)
		}
		_ = p.stdin.Close()
	}
	results := make([]knowledgeCreationResult, len(processes))
	for index, p := range processes {
		if err := p.decoder.Decode(&results[index]); err != nil {
			t.Fatalf("decode child %d result: %v", index, err)
		}
		_, _ = io.Copy(io.Discard, p.stdout)
		err := p.cmd.Wait()
		p.waited = true
		if err != nil {
			t.Fatalf("child %d: %v\n%s", index, err, p.stderr.String())
		}
	}
	return results
}

func creationWorkerContent(worker string) string {
	return "# Evidence from " + worker + "\n\n" + strings.Repeat("Unique evidence "+worker+". ", 1024) + "End."
}

func assertCreationDirectoryEntries(t *testing.T, dir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != want {
		t.Fatalf("expected %d published entries and no temporary files in %s; found %v", want, dir, entries)
	}
}
