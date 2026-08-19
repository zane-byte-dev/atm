package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/zane-byte-dev/atm/internal/config"
)

const (
	MaxTodoImages     = 10
	MaxTodoImageBytes = int64(10 * 1024 * 1024)
)

var todoImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".heic": "image/heic",
}

// TodoAssetsDir is the managed directory for one Todo's images.
func TodoAssetsDir(todoID string) string {
	return filepath.Join(config.AtmDir, "todos", "assets", todoID)
}

func TodoImagePath(todoID, storedName string) string {
	return filepath.Join(TodoAssetsDir(todoID), storedName)
}

// ImportTodoImages validates and privately copies local files into ATM's managed
// attachment directory. The returned cleanup function is for transaction
// rollback only; after a successful Todo write the caller must leave the files.
func ImportTodoImages(todoID string, paths []string) ([]TodoImage, func(), error) {
	cleanup := func() { _ = CleanupTodoAssets(todoID) }
	if len(paths) == 0 {
		return nil, cleanup, nil
	}
	if !validTodoAssetID(todoID) {
		return nil, cleanup, fmt.Errorf("invalid todo ID for image storage: %q", todoID)
	}
	if len(paths) > MaxTodoImages {
		return nil, cleanup, fmt.Errorf("too many images: got %d, maximum is %d", len(paths), MaxTodoImages)
	}

	type sourceImage struct {
		path, name, mediaType string
		size                  int64
	}
	sources := make([]sourceImage, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, cleanup, fmt.Errorf("image path must not be empty")
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, cleanup, fmt.Errorf("reading image %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, cleanup, fmt.Errorf("image is not a regular file: %s", path)
		}
		if info.Size() > MaxTodoImageBytes {
			return nil, cleanup, fmt.Errorf("image %s is %d bytes; maximum is %d bytes", path, info.Size(), MaxTodoImageBytes)
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		expected, ok := todoImageTypes[ext]
		if !ok {
			return nil, cleanup, fmt.Errorf("unsupported image format %q for %s (use PNG, JPEG, WebP, GIF, or HEIC)", ext, path)
		}
		mediaType, err := detectTodoImageType(path)
		if err != nil {
			return nil, cleanup, err
		}
		if mediaType != expected {
			return nil, cleanup, fmt.Errorf("image content for %s is %s, not %s", path, mediaType, expected)
		}
		sources = append(sources, sourceImage{path: path, name: info.Name(), mediaType: mediaType, size: info.Size()})
	}

	dir := TodoAssetsDir(todoID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, cleanup, fmt.Errorf("creating image directory: %w", err)
	}
	images := make([]TodoImage, 0, len(sources))
	for index, source := range sources {
		storedName, err := todoStoredImageName(index, source.name)
		if err != nil {
			cleanup()
			return nil, cleanup, err
		}
		destination := TodoImagePath(todoID, storedName)
		if err := copyTodoImage(source.path, destination, source.size); err != nil {
			cleanup()
			return nil, cleanup, err
		}
		images = append(images, TodoImage{
			Name:       source.name,
			Path:       destination,
			MediaType:  source.mediaType,
			SizeBytes:  source.size,
			StoredName: storedName,
		})
	}
	return images, cleanup, nil
}

func detectTodoImageType(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening image %s: %w", path, err)
	}
	defer file.Close()
	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("reading image header %s: %w", path, err)
	}
	header = header[:n]
	if isHEIC(header) {
		return "image/heic", nil
	}
	mediaType := strings.SplitN(http.DetectContentType(header), ";", 2)[0]
	for _, allowed := range todoImageTypes {
		if mediaType == allowed {
			return mediaType, nil
		}
	}
	return mediaType, nil
}

func isHEIC(header []byte) bool {
	if len(header) < 12 || string(header[4:8]) != "ftyp" {
		return false
	}
	for _, brand := range []string{"heic", "heix", "hevc", "hevx", "heim", "heis"} {
		if string(header[8:12]) == brand {
			return true
		}
		for offset := 16; offset+4 <= len(header); offset += 4 {
			if string(header[offset:offset+4]) == brand {
				return true
			}
		}
	}
	return false
}

func todoStoredImageName(index int, original string) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("creating image name: %w", err)
	}
	base := filepath.Base(original)
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	var cleaned strings.Builder
	for _, r := range stem {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			cleaned.WriteRune(r)
		} else {
			cleaned.WriteByte('_')
		}
	}
	if cleaned.Len() == 0 {
		cleaned.WriteString("image")
	}
	return fmt.Sprintf("%02d-%s-%s%s", index+1, hex.EncodeToString(random), cleaned.String(), ext), nil
}

func copyTodoImage(source, destination string, expectedSize int64) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("opening image %s: %w", source, err)
	}
	defer in.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".import-*")
	if err != nil {
		return fmt.Errorf("creating managed image: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return fmt.Errorf("securing managed image: %w", err)
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(in, MaxTodoImageBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return fmt.Errorf("copying image %s: %w", source, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing managed image: %w", closeErr)
	}
	if written != expectedSize || written > MaxTodoImageBytes {
		return fmt.Errorf("image %s changed while importing", source)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("storing image %s: %w", source, err)
	}
	return nil
}

// CleanupTodoAssets removes images only for a validated canonical Todo ID.
func CleanupTodoAssets(todoID string) error {
	if !validTodoAssetID(todoID) {
		return fmt.Errorf("invalid todo ID for image cleanup: %q", todoID)
	}
	if err := os.RemoveAll(TodoAssetsDir(todoID)); err != nil {
		return fmt.Errorf("removing images for %s: %w", todoID, err)
	}
	return nil
}

func validTodoAssetID(value string) bool {
	if len(value) < 2 || value[0] != 't' {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
