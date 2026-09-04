package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestParseTodoImageUploadBoundsAndShape(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(*multipart.Writer)
		valid bool
	}{
		{"valid", func(writer *multipart.Writer) {
			writer.WriteField("expected_etag", "todo-v1-current")
			part, _ := writer.CreateFormFile("file", "image.png")
			part.Write([]byte("image bytes validated by Work"))
		}, true},
		{"missing_etag", func(writer *multipart.Writer) {
			part, _ := writer.CreateFormFile("file", "image.png")
			part.Write([]byte("bytes"))
		}, false},
		{"duplicate_file", func(writer *multipart.Writer) {
			writer.WriteField("expected_etag", "x")
			for range 2 {
				part, _ := writer.CreateFormFile("file", "image.png")
				part.Write([]byte("bytes"))
			}
		}, false},
		{"duplicate_etag", func(writer *multipart.Writer) {
			writer.WriteField("expected_etag", "x")
			writer.WriteField("expected_etag", "y")
		}, false},
		{"path_filename", func(writer *multipart.Writer) {
			writer.WriteField("expected_etag", "x")
			part, _ := writer.CreateFormFile("file", "../image.png")
			part.Write([]byte("bytes"))
		}, false},
		{"unexpected_field", func(writer *multipart.Writer) { writer.WriteField("path", "/etc/passwd") }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			test.write(writer)
			writer.Close()
			request := httptest.NewRequest("POST", "/api/v1/tasks/t1/images", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			etag, name, data, err := ParseTodoImageUpload(httptest.NewRecorder(), request)
			if test.valid && (err != nil || etag != "todo-v1-current" || name != "image.png" || len(data) == 0) {
				t.Fatalf("parse=%q %q %q err=%v", etag, name, data, err)
			}
			if !test.valid && err == nil {
				t.Fatal("accepted invalid multipart shape")
			}
		})
	}
	request := httptest.NewRequest("POST", "/", io.NopCloser(bytes.NewReader(nil)))
	request.ContentLength = MaxImageUploadRequestBytes + 1
	if _, _, _, err := ParseTodoImageUpload(httptest.NewRecorder(), request); err == nil {
		t.Fatal("accepted oversized request")
	}
}
