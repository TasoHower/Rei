package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/provider"
)

func frame(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

func server(t *testing.T, frames []string, check func(*testing.T, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
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

func newClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: url, APIKey: "sk"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTextStream(t *testing.T) {
	frames := []string{
		frame("response.output_text.delta", `{"type":"response.output_text.delta","delta":"你"}`),
		frame("response.output_text.delta", `{"type":"response.output_text.delta","delta":"好"}`),
		frame("response.completed", `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":10,"output_tokens":5,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":1}}}}`),
	}
	srv := server(t, frames, nil)
	defer srv.Close()

	stream, err := newClient(t, srv.URL).Stream(context.Background(), provider.Request{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	var text string
	var done provider.Chunk
	for {
		ch, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if ch.Kind == provider.ChunkText {
			text += ch.Text
		}
		if ch.Kind == provider.ChunkDone {
			done = ch
		}
	}
	if text != "你好" {
		t.Fatalf("正文 = %q", text)
	}
	if done.Usage == nil || done.Usage.Input != 10 || done.Usage.CachedInput != 4 || done.Usage.Output != 5 || done.Usage.Reasoning != 1 {
		t.Fatalf("用量 = %+v", done.Usage)
	}
	if done.RequestID != "resp_1" {
		t.Fatalf("request id = %q", done.RequestID)
	}
}

func TestFunctionCallAssembly(t *testing.T) {
	frames := []string{
		frame("response.output_item.added", `{"type":"response.output_item.added","item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"read_file"}}`),
		frame("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"{\"path\":"}`),
		frame("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"\"a.txt\"}"}`),
		frame("response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","item_id":"item_1"}`),
		frame("response.completed", `{"type":"response.completed","response":{"id":"resp_2"}}`),
	}
	srv := server(t, frames, nil)
	defer srv.Close()

	res, err := provider.Complete(context.Background(), newClient(t, srv.URL), provider.Request{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("工具调用数 = %d", len(res.ToolCalls))
	}
	call := res.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "read_file" || call.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("调用 = %+v", call)
	}
}

func TestRequestTranslation(t *testing.T) {
	var got map[string]any
	srv := server(t, []string{frame("response.completed", `{"type":"response.completed","response":{"id":"r"}}`)}, func(_ *testing.T, body map[string]any) { got = body })
	defer srv.Close()

	msgs := []core.Message{
		core.NewUserTextMessage("看图"),
		{Role: core.Assistant, ToolCalls: []core.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.txt"}`}}},
		core.NewToolMessage("call_1", "read_file", "内容"),
	}
	_, err := provider.Complete(context.Background(), newClient(t, srv.URL), provider.Request{
		Model: "gpt-test", System: "be brief", MaxTokens: 256, Messages: msgs,
		Tools: []provider.Tool{{Name: "read_file", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got["instructions"] != "be brief" {
		t.Fatalf("instructions = %v", got["instructions"])
	}
	if got["max_output_tokens"].(float64) != 256 {
		t.Fatalf("max_output_tokens = %v", got["max_output_tokens"])
	}
	input := got["input"].([]any)
	kinds := map[string]bool{}
	for _, item := range input {
		kinds[item.(map[string]any)["type"].(string)] = true
	}
	for _, want := range []string{"message", "function_call", "function_call_output"} {
		if !kinds[want] {
			t.Fatalf("缺少 input item %q：%v", want, kinds)
		}
	}
	tools := got["tools"].([]any)
	first := tools[0].(map[string]any)
	if first["type"] != "function" || first["name"] != "read_file" {
		t.Fatalf("工具声明 = %v", first)
	}
}

func TestHTTPErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Stream(context.Background(), provider.Request{Model: "m"})
	var pe *provider.Error
	if !errors.As(err, &pe) || pe.Kind != provider.KindRateLimit {
		t.Fatalf("错误分类 = %v", err)
	}
}
