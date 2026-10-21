package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// ---------------------------------------------------------------- 假服务器

// rpcReply 根据方法返回 result（nil 表示只回 202，用于通知）。
func rpcReply(t *testing.T, method string, params json.RawMessage) (any, bool) {
	t.Helper()
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fake", "version": "1.0"},
		}, true
	case "notifications/initialized":
		return nil, false
	case "tools/list":
		return map[string]any{"tools": []map[string]any{{
			"name":        "echo",
			"description": "Echo the input text.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required":   []string{"text"},
			},
			"annotations": map[string]any{"readOnlyHint": true},
		}}}, true
	case "tools/call":
		var p struct {
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		text, _ := p.Arguments["text"].(string)
		if text == "boom" {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": "远端失败"}}, "isError": true}, true
		}
		return map[string]any{"content": []map[string]any{
			{"type": "text", "text": "echo: " + text},
			{"type": "image", "data": "aGVsbG8=", "mimeType": "image/png"},
		}}, true
	}
	return nil, true
}

func decodeRPC(t *testing.T, body []byte) (map[string]any, string, json.RawMessage) {
	t.Helper()
	var req struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("解析 JSON-RPC 失败：%v", err)
	}
	resp := map[string]any{"jsonrpc": "2.0"}
	if req.ID != nil {
		resp["id"] = *req.ID
	} else {
		resp["id"] = nil
	}
	return resp, req.Method, req.Params
}

func writeRPC(w http.ResponseWriter, t *testing.T, body []byte) {
	resp, method, params := decodeRPC(t, body)
	ps, _ := json.Marshal(params)
	result, ok := rpcReply(t, method, ps)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp["result"] = result
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------- HTTP

func TestHTTPRegisterAndCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		writeRPC(w, t, body)
	}))
	defer srv.Close()

	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{
		"demo": {Type: "http", URL: srv.URL, Headers: map[string]string{"X-Token": "t"}},
	}})
	defer func() { _ = mgr.Close() }()

	n, err := mgr.RegisterTools(context.Background(), eng)
	if err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if n != 1 {
		t.Fatalf("注册工具数 = %d", n)
	}
	if got := strings.Join(eng.SortedNames(), ","); got != "mcp__demo__echo" {
		t.Fatalf("工具名 = %s", got)
	}

	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "mcp__demo__echo", Arguments: `{"text":"你好"}`}})[0]
	if res.Result.IsError {
		t.Fatalf("调用失败：%s", res.Result.Content)
	}
	if !strings.Contains(res.Result.Content, "echo: 你好") {
		t.Fatalf("结果 = %q", res.Result.Content)
	}
	if len(res.Result.Images) != 1 || !strings.HasPrefix(res.Result.Images[0].Content, "data:image/png;base64,") {
		t.Fatalf("图片块 = %+v", res.Result.Images)
	}
}

func TestHTTPToolErrorIsFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		writeRPC(w, t, body)
	}))
	defer srv.Close()

	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{"demo": {Type: "http", URL: srv.URL}}})
	if _, err := mgr.RegisterTools(context.Background(), eng); err != nil {
		t.Fatal(err)
	}
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "mcp__demo__echo", Arguments: `{"text":"boom"}`}})[0]
	if !res.Result.IsError || !strings.Contains(res.Result.Content, "远端失败") {
		t.Fatalf("远端错误未透传：%+v", res.Result)
	}
}

// ---------------------------------------------------------------- SSE

func TestSSERegisterAndCall(t *testing.T) {
	// GET 事件流与 POST 端点共享一个 writer：POST 直接把响应写进事件流。
	var (
		mu     sync.Mutex
		stream http.ResponseWriter
		flush  http.Flusher
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("不支持 flush")
			return
		}
		_, _ = w.Write([]byte("event: endpoint\ndata: /message?session=1\n\n"))
		f.Flush()

		mu.Lock()
		stream, flush = w, f
		mu.Unlock()

		<-r.Context().Done()
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		resp, method, params := decodeRPC(t, body)
		ps, _ := json.Marshal(params)
		if result, ok := rpcReply(t, method, ps); ok {
			resp["result"] = result
			raw, _ := json.Marshal(resp)
			mu.Lock()
			if stream != nil {
				_, _ = fmt.Fprintf(stream, "event: message\ndata: %s\n\n", raw)
				flush.Flush()
			}
			mu.Unlock()
		}
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{"sse-demo": {Type: "sse", URL: srv.URL + "/sse"}}})
	defer func() { _ = mgr.Close() }()

	if _, err := mgr.RegisterTools(context.Background(), eng); err != nil {
		t.Fatalf("SSE 注册失败：%v", err)
	}
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "mcp__sse-demo__echo", Arguments: `{"text":"sse"}`}})[0]
	if res.Result.IsError || !strings.Contains(res.Result.Content, "echo: sse") {
		t.Fatalf("SSE 调用 = %+v", res.Result)
	}
}

// ---------------------------------------------------------------- stdio

// TestStdioHelper 在本进程内扮演 MCP 服务器（由子进程模式触发）。
func TestStdioHelper(t *testing.T) {
	if os.Getenv("REI_MCP_STDIO_HELPER") != "1" {
		return
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64<<10), 8<<20)
	out := bufio.NewWriter(os.Stdout)
	for in.Scan() {
		line := in.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		resp, method, params := decodeRPC(t, line)
		ps, _ := json.Marshal(params)
		result, ok := rpcReply(t, method, ps)
		if ok {
			resp["result"] = result
			raw, _ := json.Marshal(resp)
			_, _ = out.Write(append(raw, '\n'))
			_ = out.Flush()
		}
	}
	os.Exit(0)
}

func TestStdioRegisterAndCall(t *testing.T) {
	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{
		"local": {
			Type:    "stdio",
			Command: os.Args[0],
			Args:    []string{"-test.run=TestStdioHelper"},
			Env:     map[string]string{"REI_MCP_STDIO_HELPER": "1"},
		},
	}})
	defer func() { _ = mgr.Close() }()

	if _, err := mgr.RegisterTools(context.Background(), eng); err != nil {
		t.Fatalf("stdio 注册失败：%v", err)
	}
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "mcp__local__echo", Arguments: `{"text":"stdio"}`}})[0]
	if res.Result.IsError || !strings.Contains(res.Result.Content, "echo: stdio") {
		t.Fatalf("stdio 调用 = %+v", res.Result)
	}
}

// ---------------------------------------------------------------- 配置

func TestConfigNormalize(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServerConfig
		wantErr bool
		want    string
	}{
		{"默认 stdio", ServerConfig{Command: "node"}, false, TransportStdio},
		{"streamable 别名", ServerConfig{Type: "streamable_http", URL: "http://x"}, false, TransportHTTP},
		{"sse", ServerConfig{Type: "sse", URL: "http://x"}, false, TransportSSE},
		{"stdio 缺 command", ServerConfig{Type: "stdio"}, true, ""},
		{"http 缺 url", ServerConfig{Type: "http"}, true, ""},
		{"未知类型", ServerConfig{Type: "grpc", URL: "x"}, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.cfg.Normalize()
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if !c.wantErr && got.Type != c.want {
				t.Fatalf("type = %q, want %q", got.Type, c.want)
			}
		})
	}
}

func TestDisabledServerIsSkipped(t *testing.T) {
	off := false
	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{
		"off": {Type: "http", URL: "http://127.0.0.1:1", Enabled: &off},
	}})
	n, err := mgr.RegisterTools(context.Background(), eng)
	if n != 0 || err != nil {
		t.Fatalf("禁用的服务器不应尝试连接：n=%d err=%v", n, err)
	}
}

func TestCooldownReusesLastError(t *testing.T) {
	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{
		"dead": {Type: "http", URL: "http://127.0.0.1:1/mcp"},
	}}, WithConnectTimeout(200*time.Millisecond), WithCooldown(time.Minute))

	if _, err := mgr.RegisterTools(context.Background(), eng); err == nil {
		t.Fatal("不可达服务器应报错")
	}
	start := time.Now()
	if _, err := mgr.RegisterTools(context.Background(), eng); err == nil {
		t.Fatal("第二次也应报错")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("冷却窗口内不应重新连接，耗时 %s", elapsed)
	}
}

func TestToolReconnectsAfterBrokenConnection(t *testing.T) {
	var callCount int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		_, method, _ := decodeRPC(t, body)
		if method == "tools/call" && atomic.AddInt32(&callCount, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeRPC(w, t, body)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{"flaky": {Type: "http", URL: srv.URL}}})
	defer func() { _ = mgr.Close() }()

	if _, err := mgr.RegisterTools(context.Background(), eng); err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "mcp__flaky__echo", Arguments: `{"text":"again"}`}})[0]
	if res.Result.IsError {
		t.Fatalf("重连后应成功：%s", res.Result.Content)
	}
	if !strings.Contains(res.Result.Content, "echo: again") {
		t.Fatalf("结果 = %q", res.Result.Content)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("调用次数 = %d，期望 2（失败 + 重连后重试）", callCount)
	}
}

func TestToolCallTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		_, method, _ := decodeRPC(t, body)
		if method == "tools/call" {
			time.Sleep(200 * time.Millisecond)
		}
		writeRPC(w, t, body)
	}))
	defer srv.Close()

	eng := tools.NewEngine()
	mgr := NewManager(Config{Servers: map[string]ServerConfig{"slow": {Type: "http", URL: srv.URL}}}, WithCallTimeout(30*time.Millisecond))
	defer func() { _ = mgr.Close() }()
	if _, err := mgr.RegisterTools(context.Background(), eng); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "mcp__slow__echo", Arguments: `{"text":"x"}`}})[0]
	if !res.Result.IsError {
		t.Fatalf("超时应失败：%+v", res.Result)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("超时未生效，耗时 %s", elapsed)
	}
}
