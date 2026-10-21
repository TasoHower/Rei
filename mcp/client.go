package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 协议版本与客户端标识。
const (
	protocolVersion = "2024-11-05"
	clientName      = "rei"
	clientVersion   = "0.1.0"
)

// rpcRequest 是 JSON-RPC 2.0 请求/通知。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp: %d %s", e.Code, e.Message) }

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// ToolInfo 是 tools/list 返回的一条工具描述。
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

// ContentBlock 是 tools/call 返回的内容块。
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// CallResult 是一次工具调用结果。
type CallResult struct {
	Content           []ContentBlock  `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

// Client 是一条 MCP 连接上的 JSON-RPC 客户端。
// 调用被串行化：MCP 的请求/响应本身有序，并发只会让 id 匹配逻辑复杂化。
type Client struct {
	server string
	tr     Transport

	mu      sync.Mutex
	nextID  int64
	closed  bool
	timeout time.Duration
}

func newClient(server string, tr Transport) *Client {
	return &Client{server: server, tr: tr}
}

// Start 建立传输并完成 initialize 握手。
func (c *Client) Start(ctx context.Context) error {
	if err := c.tr.Start(ctx); err != nil {
		return err
	}
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		_ = c.tr.Close()
		return err
	}
	// initialized 是通知：没有 id，也没有响应。
	note, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	if err := c.tr.Send(ctx, note); err != nil {
		_ = c.tr.Close()
		return err
	}
	return nil
}

// ListTools 拉取工具清单。
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	raw, err := c.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("mcp: 解析 tools/list 失败：%w", err)
	}
	return payload.Tools, nil
}

// CallTool 调用一个远端工具。
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	var parsed any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsed); err != nil {
			return CallResult{}, fmt.Errorf("mcp: 工具参数不是合法 JSON：%w", err)
		}
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	raw, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": parsed})
	if err != nil {
		return CallResult{}, err
	}
	var res CallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return CallResult{}, fmt.Errorf("mcp: 解析 tools/call 失败：%w", err)
	}
	return res, nil
}

// call 发送一次请求并等待同 id 的响应；期间收到通知则跳过。
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("mcp: 连接已关闭")
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	c.nextID++
	id := c.nextID
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if err := c.tr.Send(ctx, payload); err != nil {
		return nil, err
	}

	for i := 0; i < 1000; i++ {
		msg, err := c.tr.Recv(ctx)
		if err != nil {
			return nil, err
		}
		var resp rpcResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}
		if resp.ID == nil || *resp.ID != id {
			// 服务端请求或其它通知：当前实现不处理，直接跳过。
			continue
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
	return nil, fmt.Errorf("mcp: %s 等待响应超限", method)
}

// Close 关闭底层传输。
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.tr.Close()
}
