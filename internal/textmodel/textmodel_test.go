package textmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
)

const testSchema = `{"type":"object","required":["title"],"properties":{"title":{"type":"string"}}}`

func withCredentialDir(t *testing.T) {
	t.Helper()
	oldAtmDir := config.AtmDir
	config.AtmDir = filepath.Join(t.TempDir(), ".atm")
	t.Cleanup(func() { config.AtmDir = oldAtmDir })
}

func TestRunUsesDedicatedDeepSeekProtocol(t *testing.T) {
	var captured chatRequest
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
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"修复发布检查\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "secret-key")
	t.Setenv(BaseURLEnv, server.URL)
	t.Setenv(ModelEnv, "deepseek-test")

	data, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite this")
	if err != nil {
		t.Fatal(err)
	}
	var answer struct{ Title string }
	if err := json.Unmarshal(data, &answer); err != nil || answer.Title != "修复发布检查" {
		t.Fatalf("answer=%+v err=%v", answer, err)
	}
	if captured.Model != "deepseek-test" || captured.ResponseFormat.Type != "json_object" ||
		captured.Thinking.Type != "disabled" || captured.MaxTokens != tasks[TaskTodoRefine].maxTokens {
		t.Fatalf("request=%+v", captured)
	}
	if len(captured.Messages) != 2 || !strings.Contains(captured.Messages[0].Content, testSchema) ||
		captured.Messages[1].Content != "rewrite this" {
		t.Fatalf("messages=%+v", captured.Messages)
	}
}

func TestCheckConnectionUsesDraftValuesWithoutSavingCredential(t *testing.T) {
	withCredentialDir(t)
	t.Setenv(APIKeyEnv, "wrong-environment-key")
	var captured chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer unsaved-draft-key" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	result, err := CheckConnection(context.Background(), time.Second, ConnectionCheckInput{
		APIKey:  "unsaved-draft-key",
		BaseURL: server.URL + "/",
		Model:   "draft-model",
	})
	if err != nil || !result.OK {
		t.Fatalf("CheckConnection() = %+v, %v", result, err)
	}
	if captured.Model != "draft-model" {
		t.Fatalf("model = %q", captured.Model)
	}
	configured, err := config.TextModelAPIKeyConfigured()
	if err != nil || configured {
		t.Fatalf("draft credential was persisted: configured=%v err=%v", configured, err)
	}
}

func TestCheckConnectionValidatesTypedInputWithoutEchoingKey(t *testing.T) {
	secret := strings.Repeat("s", config.MaxCredentialBytes+1)
	for _, input := range []ConnectionCheckInput{
		{APIKey: "key", BaseURL: "ftp://models.example", Model: "m"},
		{APIKey: "key", BaseURL: "https://models.example", Model: "  "},
		{APIKey: secret, BaseURL: "https://models.example", Model: "m"},
	} {
		_, err := CheckConnection(context.Background(), time.Second, input)
		if !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("CheckConnection() error = %v, want invalid_argument", err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatal("validation error leaked the draft credential")
		}
	}
}

// Classification and digests are the high-volume callers, and both send content
// the sender never meant for a model. The data rule belongs in the system
// message for every task, not only for refine.
func TestRunTellsEveryTaskToTreatInputAsData(t *testing.T) {
	for _, task := range []string{TaskTodoRefine, TaskDecision, TaskDigest} {
		var captured chatRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
		}))
		t.Setenv(APIKeyEnv, "key")
		t.Setenv(BaseURLEnv, server.URL)
		if _, err := Run(context.Background(), task, time.Second, testSchema, "prompt"); err != nil {
			t.Fatal(err)
		}
		server.Close()
		system := captured.Messages[0].Content
		if !strings.Contains(system, "never as instructions") || !strings.Contains(system, "Do not use tools") {
			t.Fatalf("task %s system message = %q", task, system)
		}
		if captured.MaxTokens != tasks[task].maxTokens || captured.MaxTokens <= 0 {
			t.Fatalf("task %s max_tokens = %d", task, captured.MaxTokens)
		}
	}
}

// A worked example anchors a shape, but for a decision it would also anchor an
// action, and "create" versus "ignore" is exactly what must stay open.
func TestRunSendsNoExampleDecision(t *testing.T) {
	var captured chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)
	if _, err := Run(context.Background(), TaskDecision, time.Second, testSchema, "prompt"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(captured.Messages[0].Content, "Example JSON") {
		t.Fatalf("decision system message carries an example: %q", captured.Messages[0].Content)
	}
}

// Every example ships in the system prompt as JSON. A broken one would teach the
// model the wrong shape, and nothing else in the request would notice.
func TestTaskExamplesAreValidJSON(t *testing.T) {
	for name, spec := range tasks {
		if spec.exampleJSON == "" {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(spec.exampleJSON), &object); err != nil {
			t.Fatalf("task %s example is not valid JSON: %v", name, err)
		}
	}
}

func TestRunUsesStandardDeepSeekAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer standard-key" {
			t.Errorf("authorization = %q", got)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "")
	t.Setenv(DeepSeekAPIKeyEnv, "standard-key")
	t.Setenv(BaseURLEnv, server.URL)
	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "prompt"); err != nil {
		t.Fatal(err)
	}
}

func TestRunReadsPrivateCredentialFile(t *testing.T) {
	withCredentialDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer file-key" {
			t.Errorf("authorization = %q", got)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	if err := config.SaveTextModelAPIKey("file-key"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(APIKeyEnv, "")
	t.Setenv(DeepSeekAPIKeyEnv, "")
	t.Setenv(BaseURLEnv, server.URL)
	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "prompt"); err != nil {
		t.Fatal(err)
	}
	if !Configured() {
		t.Fatal("Configured() is false with a saved credential")
	}
}

func TestRunWithoutCredentialSaysWhereToSetOne(t *testing.T) {
	withCredentialDir(t)
	t.Setenv(APIKeyEnv, "")
	t.Setenv(DeepSeekAPIKeyEnv, "")
	_, err := Run(context.Background(), TaskDecision, time.Second, testSchema, "prompt")
	if err == nil || !strings.Contains(err.Error(), DeepSeekAPIKeyEnv) {
		t.Fatalf("missing key error=%v", err)
	}
	if Configured() {
		t.Fatal("Configured() is true with no credential")
	}
}

func TestRunReportsDeepSeekErrorWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid credentials","type":"authentication_error"}}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "do-not-leak")
	t.Setenv(BaseURLEnv, server.URL)
	_, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "prompt")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") ||
		strings.Contains(err.Error(), "invalid credentials") ||
		strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("API error=%v", err)
	}
}

func TestCheckConnectionReturnsTypedSafeProviderFailure(t *testing.T) {
	const secret = "sk-provider-echo"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"message":"Authorization: Bearer %s"}}`, secret)
	}))
	defer server.Close()

	_, err := CheckConnection(context.Background(), time.Second, ConnectionCheckInput{
		APIKey: secret, BaseURL: server.URL, Model: "draft-model",
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Message != "text-model connection check failed" || !appErr.Retryable {
		t.Fatalf("application error = %+v", appErr)
	}
	if strings.Contains(appErr.Message, secret) || strings.Contains(appErr.Message, "Authorization") {
		t.Fatalf("public message leaked provider payload: %q", appErr.Message)
	}
}

func TestRunRejectsUnknownTask(t *testing.T) {
	_, err := Run(context.Background(), "shell-command", time.Second, `{}`, "prompt")
	if err == nil || !strings.Contains(err.Error(), "does not support task") {
		t.Fatalf("unknown task error=%v", err)
	}
}

// A gateway or a substituted model can fence the object even in json_object
// mode. Losing a whole classification to three backticks is not worth it.
func TestRunUnwrapsFencedJSON(t *testing.T) {
	fence := "```"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"%sjson\n{\"title\":\"t\"}\n%s"},"finish_reason":"stop"}]}`, fence, fence)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)
	data, err := Run(context.Background(), TaskDecision, time.Second, testSchema, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	var answer struct{ Title string }
	if err := json.Unmarshal(data, &answer); err != nil || answer.Title != "t" {
		t.Fatalf("answer=%+v err=%v", answer, err)
	}
}

func TestRunRejectsTruncatedAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"half"},"finish_reason":"length"}]}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)
	_, err := Run(context.Background(), TaskDigest, time.Second, testSchema, "prompt")
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("truncated error=%v", err)
	}
}

func TestCheckUsesMinimalSideEffectFreeRequest(t *testing.T) {
	var captured chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "connection-key")
	t.Setenv(BaseURLEnv, server.URL)
	t.Setenv(ModelEnv, "connection-model")

	result, err := Check(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.LatencyMS < 0 {
		t.Fatalf("result=%+v", result)
	}
	if captured.Model != "connection-model" || captured.MaxTokens != tasks[TaskCheck].maxTokens {
		t.Fatalf("request=%+v", captured)
	}
	if len(captured.Messages) != 2 ||
		!strings.Contains(captured.Messages[0].Content, checkSchema) ||
		!strings.Contains(captured.Messages[0].Content, `{"ok":true}`) {
		t.Fatalf("messages=%+v", captured.Messages)
	}
}

func TestCheckRejectsInvalidContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":false}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv(APIKeyEnv, "connection-key")
	t.Setenv(BaseURLEnv, server.URL)

	_, err := Check(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "ok=false") {
		t.Fatalf("contract error=%v", err)
	}
}
