package refine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuiltinTextModelUsesDedicatedDeepSeekProtocol(t *testing.T) {
	var captured deepSeekChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"修复发布检查\",\"description\":\"目标：检查变绿。\",\"complexity\":\"simple\",\"plan\":\"\",\"reason\":\"单一交付\",\"children\":[]}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(textModelAPIKeyEnv, "secret-key")
	t.Setenv(textModelBaseURLEnv, server.URL)
	t.Setenv(textModelModelEnv, "deepseek-test")

	data, err := runBuiltinTextModel(context.Background(), "ignored", time.Second,
		"todo-refine", proposalJSONSchema, "rewrite this")
	if err != nil {
		t.Fatal(err)
	}
	var proposal Proposal
	if err := json.Unmarshal(data, &proposal); err != nil || proposal.Title != "修复发布检查" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if captured.Model != "deepseek-test" || captured.ResponseFormat.Type != "json_object" ||
		captured.Thinking.Type != "disabled" || captured.MaxTokens != defaultTextModelMaxTokens {
		t.Fatalf("request=%+v", captured)
	}
	if len(captured.Messages) != 2 || !strings.Contains(captured.Messages[0].Content, proposalJSONSchema) ||
		captured.Messages[1].Content != "rewrite this" {
		t.Fatalf("messages=%+v", captured.Messages)
	}
}

func TestBuiltinTextModelUsesStandardDeepSeekAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer standard-key" {
			t.Errorf("authorization = %q", got)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(textModelAPIKeyEnv, "")
	t.Setenv(deepSeekAPIKeyEnv, "standard-key")
	t.Setenv(textModelBaseURLEnv, server.URL)
	if _, err := runBuiltinTextModel(context.Background(), "", time.Second,
		"todo-refine", proposalJSONSchema, "prompt"); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinTextModelDoesNotFallBackToAgent(t *testing.T) {
	t.Setenv(textModelAPIKeyEnv, "")
	t.Setenv(deepSeekAPIKeyEnv, "")
	_, err := runBuiltinTextModel(context.Background(), "codex", time.Second,
		"todo-refine", proposalJSONSchema, "prompt")
	if err == nil || !strings.Contains(err.Error(), deepSeekAPIKeyEnv) {
		t.Fatalf("missing key error=%v", err)
	}
}

func TestBuiltinTextModelReportsDeepSeekErrorWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid credentials","type":"authentication_error"}}`)
	}))
	defer server.Close()
	t.Setenv(textModelAPIKeyEnv, "do-not-leak")
	t.Setenv(textModelBaseURLEnv, server.URL)
	_, err := runBuiltinTextModel(context.Background(), "", time.Second,
		"todo-refine", proposalJSONSchema, "prompt")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401: invalid credentials") ||
		strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("API error=%v", err)
	}
}

func TestBuiltinTextModelRejectsOtherFunctions(t *testing.T) {
	_, err := runBuiltinTextModel(context.Background(), "", time.Second,
		"collection-decision", `{}`, "prompt")
	if err == nil || !strings.Contains(err.Error(), "does not support task") {
		t.Fatalf("other task error=%v", err)
	}
}

func TestCheckBuiltinTextModelUsesMinimalSideEffectFreeRequest(t *testing.T) {
	var captured deepSeekChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(textModelAPIKeyEnv, "connection-key")
	t.Setenv(textModelBaseURLEnv, server.URL)
	t.Setenv(textModelModelEnv, "connection-model")

	result, err := CheckBuiltinTextModel(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.LatencyMS < 0 {
		t.Fatalf("result=%+v", result)
	}
	if captured.Model != "connection-model" || captured.MaxTokens != textModelCheckMaxTokens {
		t.Fatalf("request=%+v", captured)
	}
	if len(captured.Messages) != 2 ||
		!strings.Contains(captured.Messages[0].Content, textModelCheckSchema) ||
		!strings.Contains(captured.Messages[0].Content, `{"ok":true}`) {
		t.Fatalf("messages=%+v", captured.Messages)
	}
}

func TestCheckBuiltinTextModelRejectsInvalidContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":false}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(textModelAPIKeyEnv, "connection-key")
	t.Setenv(textModelBaseURLEnv, server.URL)

	_, err := CheckBuiltinTextModel(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "ok=false") {
		t.Fatalf("contract error=%v", err)
	}
}
