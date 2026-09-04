package web

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Upload is a typed byte-upload edge. Authentication, origin, CSRF and the
// database write gate must run before parsing a request or invoking it.
type Upload func(context.Context, application.Call, string, string, string, []byte) (any, error)

const MaxImageUploadRequestBytes = store.MaxTodoImageBytes + 32*1024

// ParseTodoImageUpload reads exactly one file and one optimistic precondition.
// It never creates multipart temporary files or accepts a server-side path.
func ParseTodoImageUpload(w http.ResponseWriter, r *http.Request) (etag, name string, data []byte, err error) {
	bad := func(message string) (string, string, []byte, error) {
		return "", "", nil, application.NewError(application.CodeInvalidArgument, message)
	}
	if r.ContentLength > MaxImageUploadRequestBytes {
		return bad("image upload request is too large; maximum image size is 10 MB")
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxImageUploadRequestBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return bad("image upload requires multipart/form-data")
	}
	seenFile, seenETag := false, false
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return bad("invalid or oversized image upload")
		}
		_, disposition, parseErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if parseErr != nil {
			part.Close()
			return bad("invalid upload part")
		}
		switch part.FormName() {
		case "expected_etag":
			if seenETag || disposition["filename"] != "" {
				part.Close()
				return bad("expected_etag must appear once as a text field")
			}
			seenETag = true
			value, readErr := io.ReadAll(io.LimitReader(part, 257))
			part.Close()
			if readErr != nil || len(value) > 256 {
				return bad("expected_etag is too large")
			}
			etag = strings.TrimSpace(string(value))
		case "file":
			if seenFile {
				part.Close()
				return bad("upload exactly one image per request")
			}
			seenFile = true
			name = disposition["filename"]
			data, err = io.ReadAll(io.LimitReader(part, store.MaxTodoImageBytes+1))
			part.Close()
			if err != nil || int64(len(data)) > store.MaxTodoImageBytes {
				return bad("image exceeds the 10 MB limit")
			}
		default:
			part.Close()
			return bad("unknown image upload field")
		}
	}
	if !seenFile || len(data) == 0 || name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") {
		return bad("a non-empty image with a plain filename is required")
	}
	if !seenETag || etag == "" {
		return bad("expected_etag is required")
	}
	return etag, name, data, nil
}
