package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/provider"
)

func sse(t *testing.T, frames []string, check func(*testing.T, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		if check != nil {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("解析请求体失败：%v", err)
			}
			check(t, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
		}
	}))
}

func frame(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func newClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: url, APIKey: "sk-ant"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTextStreamWithCacheUsage(t *testing.T) {
	frames := []string{
		frame("message_start", `{"message":{"usage":{"input_tokens":10,"cache_read_input_tokens":15,"cache_creation_input_tokens":5,"output_tokens":1}}}`),
		frame("content_block_start", `{"index":0,"content_block":{"type":"text"}}`),
		frame("ping", `{}`),
		frame("content_block_delta", `{"index":0,"delta":{"type":"text_delta","text":"你"}}`),
		frame("content_block_delta", `{"index":0,"delta":{"type":"text_delta","text":"好"}}`),
		frame("content_block_stop", `{"index":0}`),
		frame("message_delta", `{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`),
		frame("message_stop", `{}`),
	}
	srv := sse(t, frames, nil)
	defer srv.Close()

	res, err := provider.Complete(context.Background(), newClient(t, srv.URL), provider.Request{
		Model: "claude-test", System: "be brief",
		Messages: []core.Message{core.NewUserTextMessage("你好")},
	})
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	if res.Text != "你好" || res.FinishReason != "stop" {
		t.Fatalf("结果 = %+v", res)
	}
	if res.Usage.Input != 30 || res.Usage.CachedInput != 15 || res.Usage.Output != 12 {
		t.Fatalf("用量映射错误：%+v", res.Usage)
	}
}

func TestToolUseAssembly(t *testing.T) {
	frames := []string{
		frame("message_start", `{"message":{"usage":{"input_tokens":5,"output_tokens":1}}}`),
		frame("content_block_start", `{"index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}`),
		frame("content_block_delta", `{"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`),
		frame("content_block_delta", `{"index":0,"delta":{"type":"input_json_delta","partial_json":"\"a.txt\"}"}}`),
		frame("content_block_stop", `{"index":0}`),
		frame("message_delta", `{"delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`),
		frame("message_stop", `{}`),
	}
	srv := sse(t, frames, nil)
	defer srv.Close()

	res, err := provider.Complete(context.Background(), newClient(t, srv.URL), provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("工具调用数 = %d", len(res.ToolCalls))
	}
	call := res.ToolCalls[0]
	if call.ID != "toolu_1" || call.Name != "read_file" || call.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("调用 = %+v", call)
	}
	if res.FinishReason != "tool_calls" {
		t.Fatalf("结束原因 = %q", res.FinishReason)
	}
}

func TestThinkingSignatureIsSurfaced(t *testing.T) {
	frames := []string{
		frame("message_start", `{"message":{"usage":{"input_tokens":1}}}`),
		frame("content_block_start", `{"index":0,"content_block":{"type":"thinking"}}`),
		frame("content_block_delta", `{"index":0,"delta":{"type":"thinking_delta","thinking":"先看目录"}}`),
		frame("content_block_delta", `{"index":0,"delta":{"type":"signature_delta","signature":"sig-abc"}}`),
		frame("content_block_stop", `{"index":0}`),
		frame("message_stop", `{}`),
	}
	srv := sse(t, frames, nil)
	defer srv.Close()

	stream, err := newClient(t, srv.URL).Stream(context.Background(), provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	var reasoning, signature string
	var text string
	for {
		ch, err := stream.Recv()
		if errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err != nil {
			break
		}
		switch ch.Kind {
		case provider.ChunkReasoning:
			reasoning += ch.Text
		case provider.ChunkSignature:
			signature += ch.Signature
		case provider.ChunkText:
			text += ch.Text
		}
	}
	if reasoning != "先看目录" || signature != "sig-abc" || text != "" {
		t.Fatalf("thinking 解析错误：reasoning=%q signature=%q text=%q", reasoning, signature, text)
	}
}

func TestRequestTranslation(t *testing.T) {
	var got map[string]any
	srv := sse(t, []string{frame("message_stop", `{}`)}, func(_ *testing.T, body map[string]any) { got = body })
	defer srv.Close()

	sig := "sig-1"
	msgs := []core.Message{
		core.NewUserMessage(
			core.Content{Type: core.ContentTypeText, Content: "看图"},
			core.Content{Type: core.ContentTypeImage, Content: "data:image/png;base64,aGVsbG8="},
		),
		{
			Role: core.Assistant, Reasoning: "先想", ReasoningSignature: sig,
			ToolCalls: []core.ToolCall{{ID: "toolu_1", Name: "read_file", Arguments: `{"path":"a.txt"}`}},
		},
		core.NewToolMessage("toolu_1", "read_file", "文件内容"),
	}
	_, err := provider.Complete(context.Background(), newClient(t, srv.URL), provider.Request{
		Model: "claude-test", System: "sys", MaxTokens: 100, Messages: msgs,
		Tools: []provider.Tool{{Name: "read_file", Description: "read", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got["system"] != "sys" {
		t.Fatalf("system = %v", got["system"])
	}
	if got["max_tokens"].(float64) != 100 {
		t.Fatalf("max_tokens = %v", got["max_tokens"])
	}
	rawMsgs := got["messages"].([]any)
	if len(rawMsgs) != 3 {
		t.Fatalf("消息数 = %d（期望 user/assistant/user）", len(rawMsgs))
	}
	last := rawMsgs[2].(map[string]any)
	lastBlocks := last["content"].([]any)
	result := lastBlocks[0].(map[string]any)
	if last["role"] != "user" || result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Fatalf("工具回执映射错误：%+v", last)
	}

	assistant := rawMsgs[1].(map[string]any)
	blocks := assistant["content"].([]any)
	thinking := blocks[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["signature"] != sig {
		t.Fatalf("thinking 块 = %+v", thinking)
	}
	toolUse := blocks[len(blocks)-1].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["name"] != "read_file" {
		t.Fatalf("tool_use 块 = %+v", toolUse)
	}

	user := rawMsgs[0].(map[string]any)["content"].([]any)
	image := user[1].(map[string]any)
	src := image["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/png" {
		t.Fatalf("图片块 = %+v", image)
	}

	tools := got["tools"].([]any)
	first := tools[0].(map[string]any)
	if first["name"] != "read_file" || first["input_schema"] == nil {
		t.Fatalf("工具声明 = %+v", first)
	}
}

func TestHTTPErrorsAndHeaders(t *testing.T) {
	var gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Stream(context.Background(), provider.Request{Model: "m"})
	var pe *provider.Error
	if !errors.As(err, &pe) || pe.Kind != provider.KindRateLimit {
		t.Fatalf("错误分类 = %v", err)
	}
	if !strings.Contains(pe.Message, "slow down") {
		t.Fatalf("错误信息 = %q", pe.Message)
	}
	if gotKey != "sk-ant" || gotVersion != DefaultVersion {
		t.Fatalf("头信息 = %q / %q", gotKey, gotVersion)
	}
}

func TestMaxTokensDefault(t *testing.T) {
	var got map[string]any
	srv := sse(t, []string{frame("message_stop", `{}`)}, func(_ *testing.T, body map[string]any) { got = body })
	defer srv.Close()
	if _, err := provider.Complete(context.Background(), newClient(t, srv.URL), provider.Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if got["max_tokens"].(float64) != DefaultMaxTok {
		t.Fatalf("max_tokens 默认值 = %v", got["max_tokens"])
	}
}
