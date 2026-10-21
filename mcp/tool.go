package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

// 错误摘要上限：远端返回的错误可能很长，进上下文前先截断。
const maxErrorRunes = 500

// serverTool 把一个 MCP 工具适配成本地 tools.Tool。
type serverTool struct {
	mu       sync.Mutex
	client   *Client
	mgr      *Manager
	server   string
	name     string
	desc     string
	schema   json.RawMessage
	readOnly bool
}

func newServerTool(client *Client, server string, info ToolInfo) *serverTool {
	desc := info.Description
	if strings.TrimSpace(desc) == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s.", info.Name, server)
	}
	schema := info.InputSchema
	if len(schema) == 0 || !json.Valid(schema) {
		schema = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return &serverTool{
		client:   client,
		server:   server,
		name:     info.Name,
		desc:     desc,
		schema:   schema,
		readOnly: info.Annotations.ReadOnlyHint,
	}
}

// Name 使用 mcp__<server>__<tool> 命名，避免与内置工具冲突。
func (t *serverTool) Name() string { return "mcp__" + t.server + "__" + t.name }

func (t *serverTool) Description() string { return t.desc }

func (t *serverTool) Parameters() json.RawMessage { return t.schema }

// CanParallel 只信任服务端声明的 readOnlyHint。
func (t *serverTool) CanParallel() bool { return t.readOnly }

func (t *serverTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	args := json.RawMessage(call.Arguments)
	if strings.TrimSpace(string(args)) == "" {
		args = json.RawMessage(`{}`)
	}
	t.mu.Lock()
	client := t.client
	t.mu.Unlock()

	res, err := client.CallTool(ctx, t.name, args)
	if err != nil && t.mgr != nil {
		// 连接可能已断：重连一次再试，仍失败才上报。
		if fresh, rerr := t.mgr.Reconnect(ctx, t.server); rerr == nil {
			t.mu.Lock()
			t.client = fresh
			t.mu.Unlock()
			res, err = fresh.CallTool(ctx, t.name, args)
		}
	}
	if err != nil {
		return tools.ErrorResult("mcp %s/%s: %s", t.server, t.name, truncate(err.Error(), maxErrorRunes))
	}

	var text []string
	var images []core.Content
	for _, block := range res.Content {
		switch block.Type {
		case "text":
			text = append(text, block.Text)
		case "image":
			if block.Data == "" {
				continue
			}
			mime := block.MimeType
			if mime == "" {
				mime = "image/png"
			}
			images = append(images, core.Content{
				Type:     core.ContentTypeImage,
				Content:  "data:" + mime + ";base64," + block.Data,
				MimeType: mime,
			})
			text = append(text, fmt.Sprintf("[image %s, %d bytes]", mime, len(block.Data)))
		case "audio":
			text = append(text, "[audio content omitted]")
		}
	}
	if len(res.StructuredContent) > 0 && string(res.StructuredContent) != "null" {
		text = append(text, "structuredContent: "+string(res.StructuredContent))
	}

	body := strings.Join(text, "\n")
	if res.IsError {
		return tools.Result{Content: truncate(body, maxErrorRunes), IsError: true, Images: images}
	}
	if body == "" {
		body = "(empty result)"
	}
	return tools.Result{Content: body, Images: images}
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
