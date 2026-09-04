package apphost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func uploadedPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestTaskImageUploadRejectsInvalidContentNamesAndIdentity(t *testing.T) {
	h := testHost(t)
	ctx, call := context.Background(), webCall()
	seed(t, card("t1", "Upload", "open", "atm"))
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	valid := uploadedPNG(t)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"fake.png", []byte("<svg><script>alert(1)</script></svg>")},
		{"broken.png", valid[:len(valid)/2]},
		{"../outside.png", valid},
		{"..\\outside.png", valid},
		{"empty.png", nil},
		{"huge.png", make([]byte, store.MaxTodoImageBytes+1)},
	} {
		if _, err := h.UploadTodoImage(ctx, call, UploadImageInput{TodoID: "t1", ExpectedETag: shown.ETag, Name: test.name, Data: test.data}); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted invalid upload %q: %v", test.name, err)
		}
	}
	agent := call
	agent.Actor.Kind = application.ActorAgent
	if _, err := h.UploadTodoImage(ctx, agent, UploadImageInput{TodoID: "t1", ExpectedETag: shown.ETag, Name: "safe.png", Data: valid}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent upload: %v", err)
	}
	if _, err := os.Stat(store.TodoAssetsDir("t1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid uploads touched storage: %v", err)
	}
	result, err := h.UploadTodoImage(ctx, call, UploadImageInput{TodoID: "t1", ExpectedETag: shown.ETag, Name: "plain.png", Data: valid})
	if err != nil || len(result.Todo.Images) != 1 || result.ETag == shown.ETag {
		t.Fatalf("upload=%+v err=%v", result, err)
	}
	attachment := result.Todo.Images[0]
	if attachment.MediaType != "image/png" || attachment.Name != "plain.png" || strings.Contains(attachment.URL, config.AtmDir) {
		t.Fatalf("image=%+v", attachment)
	}
	path, media, err := h.Attachment(ctx, call, attachment.ID)
	if err != nil || media != "image/png" {
		t.Fatalf("attachment=%s %v", media, err)
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, valid) {
		t.Fatalf("stored image mismatch: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private mode=%v err=%v", info, err)
	}
}

func TestTaskImageUploadRejectsSymlinkDirectories(t *testing.T) {
	for _, level := range []string{"todos", "assets", "t1"} {
		t.Run(level, func(t *testing.T) {
			h := testHost(t)
			seed(t, card("t1", "Upload", "open", "atm"))
			shown, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
			if err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			parent := config.AtmDir
			if level != "todos" {
				parent = filepath.Join(parent, "todos")
			}
			if level == "t1" {
				parent = filepath.Join(parent, "assets")
			}
			if err := os.MkdirAll(parent, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(parent, level)); err != nil {
				t.Fatal(err)
			}
			_, err = h.UploadTodoImage(context.Background(), webCall(), UploadImageInput{TodoID: "t1", ExpectedETag: shown.ETag, Name: "plain.png", Data: uploadedPNG(t)})
			if err == nil {
				t.Fatal("followed a managed directory symlink")
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != 0 {
				t.Fatalf("wrote outside managed storage: %+v", entries)
			}
			current, _ := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
			if len(current.Todo.Images) != 0 {
				t.Fatal("failed upload created metadata")
			}
		})
	}
}

func TestTaskImageUploadConcurrencyAndLimit(t *testing.T) {
	h := testHost(t)
	other := New("other-host")
	todo := card("t1", "Upload", "open", "atm")
	for index := 0; index < store.MaxTodoImages-1; index++ {
		todo.Images = append(todo.Images, store.TodoImage{Name: fmt.Sprintf("old-%d.png", index), StoredName: fmt.Sprintf("old-%d.png", index), MediaType: "image/png", SizeBytes: 1})
	}
	seed(t, todo)
	shown, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	valid := uploadedPNG(t)
	errorsCh := make(chan error, 2)
	var workers sync.WaitGroup
	for _, host := range []*Host{h, other} {
		workers.Add(1)
		go func(host *Host) {
			defer workers.Done()
			_, err := host.UploadTodoImage(context.Background(), webCall(), UploadImageInput{TodoID: "t1", ExpectedETag: shown.ETag, Name: "plain.png", Data: valid})
			errorsCh <- err
		}(host)
	}
	workers.Wait()
	close(errorsCh)
	won, conflicted := 0, 0
	for err := range errorsCh {
		if err == nil {
			won++
		} else if errors.Is(err, application.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if won != 1 || conflicted != 1 {
		t.Fatalf("won=%d conflicted=%d", won, conflicted)
	}
	current, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
	if err != nil || len(current.Todo.Images) != store.MaxTodoImages {
		t.Fatalf("images=%+v err=%v", current.Todo.Images, err)
	}
	if _, err := h.UploadTodoImage(context.Background(), webCall(), UploadImageInput{TodoID: "t1", ExpectedETag: current.ETag, Name: "one-more.png", Data: valid}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("image count overflow: %v", err)
	}
	files, _ := os.ReadDir(store.TodoAssetsDir("t1"))
	if len(files) != 1 {
		t.Fatalf("failed or concurrent upload leaked files: %+v", files)
	}
}

func TestTaskImageUploadRollsBackFileOnDatabaseFailure(t *testing.T) {
	h := testHost(t)
	seed(t, card("t1", "Upload", "open", "atm"))
	shown, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER reject_upload BEFORE INSERT ON todo_images BEGIN SELECT RAISE(ABORT, 'injected image failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.UploadTodoImage(context.Background(), webCall(), UploadImageInput{TodoID: "t1", ExpectedETag: shown.ETag, Name: "plain.png", Data: uploadedPNG(t)})
	if err == nil {
		t.Fatal("injected DB failure was ignored")
	}
	files, err := os.ReadDir(store.TodoAssetsDir("t1"))
	if err != nil || len(files) != 0 {
		t.Fatalf("rollback left files: %+v err=%v", files, err)
	}
	current, _ := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
	if len(current.Todo.Images) != 0 || current.ETag != shown.ETag {
		t.Fatal("failed upload changed metadata")
	}
}
