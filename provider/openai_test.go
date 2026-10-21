package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tasohower/rei/core"
)

// sseServer 起一个按行返回 SSE 的假上游，并把收到的请求体交给检查函数。
func sseServer(t *testing.T, lines []string, check func(*testing.T, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("解析请求体失败：%v", err)
			}
			check(t, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n\n"))
		}
	}))
}

func newTestClient(t *testing.T, url string) *OpenAI {
	t.Helper()
	p, err := NewOpenAI(OpenAIConfig{BaseURL: url, APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("构造客户端失败：%v", err)
	}
	return p
}

func TestCompleteTextStream(t *testing.T) {
	lines := []string{
		`data: {"choices":[{"delta":{"content":"你"}}]}`,
		`data: {"choices":[{"delta":{"content":"好"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
		`data: [DONE]`,
	}
	srv := sseServer(t, lines, func(t *testing.T, body map[string]any) {
		if body["model"] != "test-model" {
			t.Errorf("model = %v", body["model"])
		}
		if body["stream"] != true {
			t.Errorf("stream = %v", body["stream"])
		}
		msgs, _ := body["messages"].([]any)
		if len(msgs) != 2 {
			t.Fatalf("messages 数 = %d，期望 2（system + user）", len(msgs))
		}
	})
	defer srv.Close()

	res, err := Complete(context.Background(), newTestClient(t, srv.URL), Request{
		Model:  "test-model",
		System: "你是助手",
		Messages: []core.Message{
			core.NewUserTextMessage("你好"),
		},
	})
	if err != nil {
		t.Fatalf("Complete 失败：%v", err)
	}
	if res.Text != "你好" {
		t.Fatalf("正文 = %q", res.Text)
	}
	if res.Usage.Input != 10 || res.Usage.Output != 2 {
		t.Fatalf("用量 = %+v", res.Usage)
	}
	if res.FinishReason != "stop" {
		t.Fatalf("结束原因 = %q", res.FinishReason)
	}
}

func TestToolCallIsAssembledFromDeltas(t *testing.T) {
	lines := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.txt\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	srv := sseServer(t, lines, nil)
	defer srv.Close()

	res, err := Complete(context.Background(), newTestClient(t, srv.URL), Request{Model: "m"})
	if err != nil {
		t.Fatalf("Complete 失败：%v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("工具调用数 = %d", len(res.ToolCalls))
	}
	call := res.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "read_file" {
		t.Fatalf("调用 = %+v", call)
	}
	if call.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("参数拼接错误：%q", call.Arguments)
	}
	if res.FinishReason != "tool_calls" {
		t.Fatalf("结束原因 = %q", res.FinishReason)
	}
}

func TestReasoningDeltaAndCachedUsage(t *testing.T) {
	lines := []string{
		`data: {"choices":[{"delta":{"reasoning_content":"先想一下"}}]}`,
		`data: {"choices":[{"delta":{"content":"结论"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":5}}}`,
		`data: [DONE]`,
	}
	srv := sseServer(t, lines, nil)
	defer srv.Close()

	res, err := Complete(context.Background(), newTestClient(t, srv.URL), Request{Model: "m"})
	if err != nil {
		t.Fatalf("Complete 失败：%v", err)
	}
	if res.Reasoning != "先想一下" || res.Text != "结论" {
		t.Fatalf("思考/正文 = %q / %q", res.Reasoning, res.Text)
	}
	if res.Usage.CachedInput != 80 || res.Usage.Reasoning != 5 {
		t.Fatalf("用量 = %+v", res.Usage)
	}
}

func TestHTTPErrorIsClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL).Stream(context.Background(), Request{Model: "m"})
	if err == nil {
		t.Fatal("期望返回错误")
	}
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("错误类型 = %T", err)
	}
	if pe.Kind != KindRateLimit || !pe.Retryable() {
		t.Fatalf("分类错误：%+v", pe)
	}
	if pe.RetryAfter.String() != "2s" {
		t.Fatalf("RetryAfter = %s", pe.RetryAfter)
	}
}

func TestContextExceededIsDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 8192 tokens"}}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL).Stream(context.Background(), Request{Model: "m"})
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != KindContextExceeded {
		t.Fatalf("期望 context_exceeded，得到 %v", err)
	}
	if pe.Retryable() {
		t.Fatal("上下文超限不应标记为可重试")
	}
}

func TestAuthorizationAndCustomHeaders(t *testing.T) {
	var gotAuth, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Gateway")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, APIKey: "sk-1", Headers: map[string]string{"X-Gateway": "g1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Complete(context.Background(), p, Request{Model: "m"}); err != nil {
		t.Fatalf("Complete 失败：%v", err)
	}
	if gotAuth != "Bearer sk-1" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotCustom != "g1" {
		t.Fatalf("自定义头 = %q", gotCustom)
	}
}

func TestNewOpenAIRejectsEmptyBaseURL(t *testing.T) {
	if _, err := NewOpenAI(OpenAIConfig{}); err == nil || !strings.Contains(err.Error(), "BaseURL") {
		t.Fatalf("空 BaseURL 应当报错，得到 %v", err)
	}
}
