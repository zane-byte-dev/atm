package work

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

const MaxUploadedImagePixels = 16 * 1024 * 1024

type AddImageInput struct {
	TodoID       string
	ExpectedETag string
	Name         string
	Data         []byte
}

type AddImageResult struct {
	Todo Todo
}

// AddImage accepts bytes only. Validation precedes the write lock; the Todo
// precondition, count check, generated file and association share one serialized
// write. A failed database commit removes only this newly created file.
func (service Service) AddImage(ctx context.Context, call application.Call, input AddImageInput) (AddImageResult, error) {
	if err := validateMetadataCall(ctx, call); err != nil {
		return AddImageResult{}, err
	}
	if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginWeb {
		return AddImageResult{}, application.NewError(application.CodeForbidden, "image upload requires a human Web action")
	}
	if !store.LooksLikeTodoID(input.TodoID) || input.TodoID != store.NormalizeTodoID(input.TodoID) {
		return AddImageResult{}, metadataInvalidArgument("invalid todo ID", "todo_id", input.TodoID)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 255 || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") {
		return AddImageResult{}, metadataInvalidArgument("image requires a plain filename", "name", "")
	}
	mediaType, extension, err := validateUploadedImage(input.Data)
	if err != nil {
		return AddImageResult{}, application.WrapError(application.CodeInvalidArgument, err.Error(), err)
	}
	var result AddImageResult
	var directory *os.Root
	var storedName string
	err = service.Mutate(func(tx *Transaction) error {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		todo, err := tx.Todo(input.TodoID)
		if err != nil {
			return metadataTodoNotFound(input.TodoID, err)
		}
		if err := checkExpectedTodo(call, *todo, input.ExpectedETag); err != nil {
			return err
		}
		if len(todo.Images) >= store.MaxTodoImages {
			return metadataInvalidArgument("a todo may contain at most 10 images", "images", len(todo.Images))
		}
		directory, err = openUploadDirectory(todo.ID)
		if err != nil {
			return metadataUnavailable("open managed image storage", err)
		}
		storedName, err = writeUploadedImage(directory, input.Data, extension)
		if err != nil {
			return metadataUnavailable("store image upload", err)
		}
		todo.Images = append(todo.Images, store.TodoImage{
			Name: name, StoredName: storedName, MediaType: mediaType, SizeBytes: int64(len(input.Data)),
			Path: store.TodoImagePath(todo.ID, storedName),
		})
		result.Todo = cloneTodo(*todo)
		return nil
	})
	if directory != nil {
		if err != nil && storedName != "" {
			_ = directory.Remove(storedName)
		}
		directory.Close()
	}
	if err != nil {
		return AddImageResult{}, metadataApplicationError("attach uploaded image", err)
	}
	if err := syncTodoDocumentIfPresent(&result.Todo); err != nil {
		appErr := application.WrapError(application.CodeUnavailable, "image saved, but document refresh failed; reload before retrying", err)
		appErr.Details = map[string]any{"committed": true, "todo_id": result.Todo.ID, "etag": TodoETag(result.Todo)}
		return AddImageResult{}, appErr
	}
	return result, nil
}

func validateUploadedImage(data []byte) (mediaType, extension string, err error) {
	if len(data) == 0 || int64(len(data)) > store.MaxTodoImageBytes {
		return "", "", fmt.Errorf("image must contain 1 byte to 10 MB")
	}
	mediaType = http.DetectContentType(data)
	formats := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif"}
	extension, ok := formats[mediaType]
	if !ok {
		return "", "", fmt.Errorf("upload supports PNG, JPEG and GIF images")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > MaxUploadedImagePixels {
		return "", "", fmt.Errorf("invalid image or dimensions exceed 16 megapixels / 16384 pixels per side")
	}
	if format == "gif" {
		if err := validateGIFBudget(data); err != nil {
			return "", "", err
		}
		if _, err := gif.DecodeAll(bytes.NewReader(data)); err != nil {
			return "", "", fmt.Errorf("invalid GIF image: %w", err)
		}
	} else if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", "", fmt.Errorf("invalid image contents: %w", err)
	}
	return mediaType, extension, nil
}

// Bound the sum of decoded GIF frames before DecodeAll allocates them.
func validateGIFBudget(data []byte) error {
	bad := func() error { return fmt.Errorf("invalid GIF or animation exceeds 64 frames / 16 megapixels") }
	if len(data) < 13 {
		return bad()
	}
	pos := 13
	if data[10]&0x80 != 0 {
		pos += 3 << (uint(data[10]&7) + 1)
	}
	frames, pixels := 0, int64(0)
	skipBlocks := func() bool {
		for pos < len(data) {
			n := int(data[pos])
			pos++
			if n == 0 {
				return true
			}
			pos += n
		}
		return false
	}
	for pos < len(data) {
		marker := data[pos]
		pos++
		switch marker {
		case 0x3b:
			if frames == 0 {
				return bad()
			}
			return nil
		case 0x21:
			pos++ // extension label
			if !skipBlocks() {
				return bad()
			}
		case 0x2c:
			if pos+9 > len(data) {
				return bad()
			}
			width := int(data[pos+4]) | int(data[pos+5])<<8
			height := int(data[pos+6]) | int(data[pos+7])<<8
			pixels += int64(width) * int64(height)
			frames++
			if frames > 64 || pixels > MaxUploadedImagePixels {
				return bad()
			}
			packed := data[pos+8]
			pos += 9
			if packed&0x80 != 0 {
				pos += 3 << (uint(packed&7) + 1)
			}
			pos++ // LZW minimum code size
			if !skipBlocks() {
				return bad()
			}
		default:
			return bad()
		}
	}
	return bad()
}

func openUploadDirectory(todoID string) (*os.Root, error) {
	info, err := os.Lstat(config.AtmDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("data directory must be a real directory")
	}
	root, err := os.OpenRoot(config.AtmDir)
	if err != nil {
		return nil, err
	}
	for _, component := range []string{"todos", "assets", todoID} {
		if err := root.Mkdir(component, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			root.Close()
			return nil, err
		}
		info, err := root.Lstat(component)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			root.Close()
			return nil, fmt.Errorf("managed image directory cannot be a symlink or file")
		}
		next, err := root.OpenRoot(component)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = next
	}
	return root, nil
}

func writeUploadedImage(directory *os.Root, data []byte, extension string) (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	name := "upload-" + hex.EncodeToString(token[:]) + extension
	temporary := "." + name + ".partial"
	file, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer directory.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	// A hard-link publication is atomic and never overwrites an existing name
	// (including a symlink); both operands stay under the open directory root.
	if err := directory.Link(temporary, filepath.Base(name)); err != nil {
		return "", err
	}
	parent, err := directory.Open(".")
	if err != nil {
		directory.Remove(name)
		return "", err
	}
	err = parent.Sync()
	parent.Close()
	if err != nil {
		directory.Remove(name)
		return "", err
	}
	return name, nil
}
