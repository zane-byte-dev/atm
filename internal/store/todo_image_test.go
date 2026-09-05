package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tinyPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}

func TestImportTodoImagesCopiesPrivateManagedFiles(t *testing.T) {
	withTempStore(t)
	source := filepath.Join(t.TempDir(), "screen shot.png")
	if err := os.WriteFile(source, tinyPNG, 0644); err != nil {
		t.Fatal(err)
	}

	images, cleanup, err := ImportTodoImages("t12", []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Name != "screen shot.png" || images[0].MediaType != "image/png" ||
		images[0].SizeBytes != int64(len(tinyPNG)) || !strings.HasPrefix(images[0].Path, TodoAssetsDir("t12")) {
		t.Fatalf("images = %#v", images)
	}
	info, err := os.Stat(images[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(TodoAssetsDir("t12")); !os.IsNotExist(err) {
		t.Fatalf("asset directory remains after rollback cleanup: %v", err)
	}
}

func TestImportTodoImagesRejectsInvalidInputsBeforeCopying(t *testing.T) {
	withTempStore(t)
	dir := t.TempDir()
	wrongContent := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(wrongContent, []byte("not an image"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ImportTodoImages("t1", []string{wrongContent}); err == nil || !strings.Contains(err.Error(), "image content") {
		t.Fatalf("content validation error = %v", err)
	}

	large := filepath.Join(dir, "large.png")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxTodoImageBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, _, err := ImportTodoImages("t1", []string{large}); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("size validation error = %v", err)
	}

	paths := make([]string, MaxTodoImages+1)
	if _, _, err := ImportTodoImages("t1", paths); err == nil || !strings.Contains(err.Error(), "too many images") {
		t.Fatalf("count validation error = %v", err)
	}
	if _, err := os.Stat(TodoAssetsDir("t1")); !os.IsNotExist(err) {
		t.Fatalf("invalid imports created assets: %v", err)
	}
}

func TestTodoImagesRoundTripAndSurviveTrash(t *testing.T) {
	withTempStore(t)
	source := filepath.Join(t.TempDir(), "design.png")
	if err := os.WriteFile(source, tinyPNG, 0644); err != nil {
		t.Fatal(err)
	}
	images, _, err := ImportTodoImages("t1", []string{source})
	if err != nil {
		t.Fatal(err)
	}
	seedTodos(t, Todo{ID: "t1", Title: "Image task", Priority: "P1", Status: TodoStatusOpen, Created: Today(), Images: images})

	loaded, err := LoadTodosReadOnly()
	if err != nil || len(loaded.Items) != 1 || len(loaded.Items[0].Images) != 1 {
		t.Fatalf("loaded = %#v, err=%v", loaded, err)
	}
	if loaded.Items[0].Images[0].Path != images[0].Path {
		t.Fatalf("path = %q, want %q", loaded.Items[0].Images[0].Path, images[0].Path)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	archived, err := LoadArchivedTodos()
	if err != nil || len(archived) != 1 || len(archived[0].Images) != 1 {
		t.Fatalf("archived = %#v, err=%v", archived, err)
	}
	if _, err := os.Stat(archived[0].Images[0].Path); err != nil {
		t.Fatalf("trashed image missing: %v", err)
	}
}
