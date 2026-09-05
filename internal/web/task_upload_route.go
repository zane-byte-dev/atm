package web

import (
	"github.com/zane-byte-dev/atm/internal/application"
	"net/http"
	"strings"
)

// Authentication and Origin/CSRF validation run before multipart parsing.
func (server *Server) serveImageUpload(w http.ResponseWriter, r *http.Request, call application.Call) {
	if server.options.Upload == nil {
		server.fail(w, 503, "unavailable", "image uploads are unavailable")
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/"), "/images")
	if id == "" || strings.Contains(id, "/") || len(id) > 64 {
		server.fail(w, 404, "not_found", "task not found")
		return
	}
	etag, name, data, err := ParseTodoImageUpload(w, r)
	if err != nil {
		server.applicationError(w, call.RequestID, err)
		return
	}
	result, err := server.options.Upload(r.Context(), call, id, etag, name, data)
	if err != nil {
		server.applicationError(w, call.RequestID, err)
		return
	}
	server.Invalidate("todos")
	server.respondID(w, 200, call.RequestID, result, nil)
}
