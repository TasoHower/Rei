// Package responses 实现 OpenAI Responses 协议的流式 provider。
//
// 与 Chat Completions 的差异：输入是一组 item（message / function_call /
// function_call_output），工具调用不再是 assistant 消息的一部分；流式事件是
// 命名的语义事件（response.output_text.delta 等），而不是统一的 delta 包。
package responses

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/provider"
)

const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultPath    = "/responses"
)

// Config 是客户端配置。
type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Headers    map[string]string
}

// Client 是 Responses 客户端。
type Client struct {
	url     string
	apiKey  string
	client  *http.Client
	headers map[string]string
}

// New 构造客户端。
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &Client{url: base + DefaultPath, apiKey: cfg.APIKey, client: client, headers: cfg.Headers}, nil
}

// Stream 发起一次流式请求。
func (c *Client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	body, err := json.Marshal(buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("responses: 编码请求失败：%w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("responses: 构造请求失败：%w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, &provider.Error{Kind: provider.KindCanceled, Message: ctxErr.Error()}
		}
		return nil, &provider.Error{Kind: provider.KindGeneric, Message: err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, provider.ClassifyHTTPError(resp.StatusCode, string(raw))
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	return &responseStream{body: resp.Body, scanner: sc, calls: map[string]*callBuilder{}}, nil
}

// ---------------------------------------------------------------- 请求映射

type apiRequest struct {
	Model           string    `json:"model"`
	Instructions    string    `json:"instructions,omitempty"`
	Input           []any     `json:"input"`
	Tools           []apiTool `json:"tools,omitempty"`
	MaxOutputTokens int       `json:"max_output_tokens,omitempty"`
	Stream          bool      `json:"stream"`
}

type apiTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

func buildRequest(req provider.Request) apiRequest {
	out := apiRequest{
		Model:           req.Model,
		Instructions:    req.System,
		MaxOutputTokens: req.MaxTokens,
		Stream:          true,
	}
	for _, m := range req.Messages {
		out.Input = append(out.Input, translateMessage(m)...)
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, apiTool{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

func translateMessage(m core.Message) []any {
	switch m.Role {
	case core.System:
		if strings.TrimSpace(m.Text()) == "" {
			return nil
		}
		return []any{messageItem("system", []any{map[string]any{"type": "input_text", "text": m.Text()}})}
	case core.User:
		var parts []any
		for _, c := range m.Content {
			if c.Type == core.ContentTypeImage {
				parts = append(parts, map[string]any{"type": "input_image", "image_url": c.Content})
				continue
			}
			if c.Content != "" {
				parts = append(parts, map[string]any{"type": "input_text", "text": c.Content})
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []any{messageItem("user", parts)}
	case core.Assistant:
		var items []any
		if text := strings.TrimSpace(m.Text()); text != "" {
			items = append(items, messageItem("assistant", []any{map[string]any{"type": "output_text", "text": text}}))
		}
		for _, tc := range m.ToolCalls {
			args := tc.Arguments
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			items = append(items, map[string]any{
				"type": "function_call", "call_id": tc.ID, "name": tc.Name, "arguments": args,
			})
		}
		return items
	case core.Tool:
		return []any{map[string]any{
			"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Text(),
		}}
	}
	return nil
}

func messageItem(role string, content []any) map[string]any {
	return map[string]any{"type": "message", "role": role, "content": content}
}

// ---------------------------------------------------------------- 流实现

type callBuilder struct {
	callID string
	name   string
	args   strings.Builder
}

type responseStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	closed  bool

	pending []provider.Chunk
	calls   map[string]*callBuilder

	usage     core.Usage
	requestID string
	finish    string
	done      bool
}

func (s *responseStream) Recv() (provider.Chunk, error) {
	for {
		if len(s.pending) > 0 {
			ch := s.pending[0]
			s.pending = s.pending[1:]
			return ch, nil
		}
		if s.done {
			return provider.Chunk{}, io.EOF
		}
		event, data, err := s.nextEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.finalize()
				continue
			}
			return provider.Chunk{}, err
		}
		if data == "" {
			continue
		}
		if chunk, err := s.handle(event, data); err != nil {
			return provider.Chunk{}, err
		} else if chunk != nil {
			return *chunk, nil
		}
	}
}

func (s *responseStream) nextEvent() (string, string, error) {
	event := ""
	var data []string
	for {
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				return "", "", &provider.Error{Kind: provider.KindUpstream, Message: "读取流失败：" + err.Error()}
			}
			return "", "", io.EOF
		}
		line := strings.TrimRight(s.scanner.Text(), "\r")
		if line == "" {
			if len(data) > 0 {
				return event, strings.Join(data, "\n"), nil
			}
			event = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
}

func (s *responseStream) handle(event, data string) (*provider.Chunk, error) {
	var probe struct {
		Type     string `json:"type"`
		Delta    string `json:"delta"`
		ItemID   string `json:"item_id"`
		Response struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Usage  struct {
				InputTokens        int `json:"input_tokens"`
				OutputTokens       int `json:"output_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
		Item struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		} `json:"item"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &probe); err != nil {
		return nil, nil
	}
	if probe.Type == "" {
		probe.Type = event
	}

	switch probe.Type {
	case "response.output_item.added":
		if probe.Item.Type == "function_call" {
			s.calls[probe.Item.ID] = &callBuilder{callID: probe.Item.CallID, name: probe.Item.Name}
		}
	case "response.function_call_arguments.delta":
		if b, ok := s.calls[probe.ItemID]; ok {
			b.args.WriteString(probe.Delta)
		}
	case "response.function_call_arguments.done":
		if b, ok := s.calls[probe.ItemID]; ok {
			call := s.emitCall(probe.ItemID, b)
			return &provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: &call}, nil
		}
	case "response.output_text.delta":
		if probe.Delta != "" {
			return &provider.Chunk{Kind: provider.ChunkText, Text: probe.Delta}, nil
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if probe.Delta != "" {
			return &provider.Chunk{Kind: provider.ChunkReasoning, Text: probe.Delta}, nil
		}
	case "response.completed":
		s.requestID = probe.Response.ID
		u := probe.Response.Usage
		s.usage = core.Usage{
			Input:       u.InputTokens,
			CachedInput: u.InputTokensDetails.CachedTokens,
			Output:      u.OutputTokens,
			Reasoning:   u.OutputTokensDetails.ReasoningTokens,
		}
		s.finalize()
	case "response.failed", "error":
		msg := "responses: 上游失败"
		if probe.Response.Error != nil && probe.Response.Error.Message != "" {
			msg = probe.Response.Error.Message
		} else if probe.Error != nil && probe.Error.Message != "" {
			msg = probe.Error.Message
		}
		return nil, &provider.Error{Kind: provider.KindUpstream, Message: msg}
	}
	return nil, nil
}

func (s *responseStream) emitCall(itemID string, b *callBuilder) core.ToolCall {
	args := b.args.String()
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	delete(s.calls, itemID)
	return core.ToolCall{ID: b.callID, Name: b.name, Arguments: args}
}

func (s *responseStream) finalize() {
	if s.done {
		return
	}
	s.done = true
	for id, b := range s.calls {
		s.pending = append(s.pending, provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: ptr(s.emitCall(id, b))})
	}
	finish := s.finish
	if finish == "" {
		finish = "stop"
	}
	usage := s.usage
	s.pending = append(s.pending, provider.Chunk{
		Kind: provider.ChunkDone, FinishReason: finish, Usage: &usage, RequestID: s.requestID,
	})
}

func ptr(t core.ToolCall) *core.ToolCall { return &t }

func (s *responseStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}
