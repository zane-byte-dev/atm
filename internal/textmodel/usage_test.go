package textmodel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// captureCalls points the sink at a slice for one test.
func captureCalls(t *testing.T) *[]Call {
	t.Helper()
	var seen []Call
	previous := Sink
	Sink = func(call Call) { seen = append(seen, call) }
	t.Cleanup(func() { Sink = previous })
	return &seen
}

func TestSinkReceivesTheReportedUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"ok\"}"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1200,"completion_tokens":90,"prompt_cache_hit_tokens":896}}`)
	}))
	defer server.Close()
	seen := captureCalls(t)
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)
	t.Setenv(ModelEnv, "deepseek-test")

	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite"); err != nil {
		t.Fatal(err)
	}

	if len(*seen) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(*seen))
	}
	call := (*seen)[0]
	if call.Task != TaskTodoRefine || call.Model != "deepseek-test" || call.Err != "" {
		t.Fatalf("call = %+v", call)
	}
	if call.Usage.InputTokens != 1200 || call.Usage.OutputTokens != 90 || call.Usage.CacheHitTokens != 896 {
		t.Fatalf("usage = %+v", call.Usage)
	}
	if !call.Usage.Reported() || call.StartedAt.IsZero() {
		t.Fatalf("call = %+v", call)
	}
}

// 端点可以配成任何 OpenAI 兼容服务，那边没有 DeepSeek 的 prompt_cache_hit_tokens，
// 缓存命中在 prompt_tokens_details.cached_tokens 里。
func TestSinkReadsOpenAICompatibleCacheField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"ok\"}"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":300,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":128}}}`)
	}))
	defer server.Close()
	seen := captureCalls(t)
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)

	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0].Usage.CacheHitTokens != 128 {
		t.Fatalf("calls = %+v", *seen)
	}
}

// 网关可以不返回 usage 块。那时「花了多少」是不可知，但这一次调用确实发生过，
// 所以行还是要有——Reported() 才是区分「零」和「不知道」的那个开关。
func TestSinkRecordsCallsWithNoUsageBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"ok\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	seen := captureCalls(t)
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)

	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite"); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(*seen))
	}
	if (*seen)[0].Usage.Reported() {
		t.Fatalf("usage = %+v, want nothing reported", (*seen)[0].Usage)
	}
}

// HTTP 4xx 也要记：一次限流同样占了一次调用，而且报错里可能有它报的 usage。
func TestSinkRecordsFailedCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"rate limit reached","type":"rate_limit"}}`)
	}))
	defer server.Close()
	seen := captureCalls(t)
	t.Setenv(APIKeyEnv, "key")
	t.Setenv(BaseURLEnv, server.URL)

	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite"); err == nil {
		t.Fatal("expected the HTTP 429 to surface as an error")
	}
	if len(*seen) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(*seen))
	}
	if !strings.Contains((*seen)[0].Err, "429") {
		t.Fatalf("err = %q, want the status in it", (*seen)[0].Err)
	}
}

// 连不上也要记，而且这不是「花了多少」的问题：token 全零，但「这条链路当时是断的」
// 恰恰是这张表要回答的另一半。
func TestSinkRecordsTransportFailures(t *testing.T) {
	seen := captureCalls(t)
	t.Setenv(APIKeyEnv, "key")
	// 保留给测试用的丢弃地址：连接会立刻被拒。
	t.Setenv(BaseURLEnv, "http://127.0.0.1:1")

	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite"); err == nil {
		t.Fatal("expected the dial failure to surface as an error")
	}
	if len(*seen) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(*seen))
	}
	if (*seen)[0].Err == "" || (*seen)[0].Usage.Reported() {
		t.Fatalf("call = %+v", (*seen)[0])
	}
}

// 没配 key、任务名不认这些连请求都没组装出来，也没花钱。记下来只会把「调用了多少次
// 模型」搅浑，所以一行都不该有。
func TestSinkIgnoresCallsThatNeverLeftTheProcess(t *testing.T) {
	seen := captureCalls(t)
	// 必须把凭据目录也指走：只清环境变量的话 APIKey() 会退回去读真实的
	// ~/.atm/credentials.json，在开发机上那里通常真有一把 key。
	previous := config.AtmDir
	config.AtmDir = t.TempDir()
	t.Cleanup(func() { config.AtmDir = previous })
	t.Setenv(APIKeyEnv, "")
	t.Setenv(DeepSeekAPIKeyEnv, "")
	t.Setenv(BaseURLEnv, "http://127.0.0.1:1")

	if _, err := Run(context.Background(), TaskTodoRefine, time.Second, testSchema, "rewrite"); err == nil {
		t.Fatal("expected a missing credential to fail")
	}
	if _, err := Run(context.Background(), "no-such-task", time.Second, testSchema, "x"); err == nil {
		t.Fatal("expected an unknown task to fail")
	}
	if len(*seen) != 0 {
		t.Fatalf("recorded %+v, want nothing", *seen)
	}
}
