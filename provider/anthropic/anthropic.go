// Package anthropic 实现 Anthropic Messages 协议的 provider.Provider。
//
// 与 OpenAI 兼容实现的差异集中在三处：
//   - system 是顶层字段，不是一条消息；
//   - 工具结果必须作为 user 消息里的 tool_result 块回传；
//   - 思考内容带 signature，必须与 thinking 块一起原样回传。
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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

// 默认值。
const (
	DefaultBaseURL = "https://api.anthropic.com"
	DefaultVersion = "2023-06-01"
	DefaultMaxTok  = 8192
)

// Config 是客户端配置。
type Config struct {
	BaseURL    string
	APIKey     string
	Version    string
	HTTPClient *http.Client
	Headers    map[string]string
	// Betas 会拼进 anthropic-beta 头（逗号分隔）。
	Betas []string
}

// Client 是 Anthropic Messages 客户端。
type Client struct {
	baseURL string
	apiKey  string
	version string
	betas   string
	client  *http.Client
	headers map[string]string
}

// New 构造客户端。
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	version := cfg.Version
	if version == "" {
		version = DefaultVersion
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &Client{
		baseURL: base,
		apiKey:  cfg.APIKey,
		version: version,
		betas:   strings.Join(cfg.Betas, ","),
		client:  client,
		headers: cfg.Headers,
	}, nil
}

// ---------------------------------------------------------------- 请求结构

type apiRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type apiMessage struct {
	Role    string     `json:"role"`
	Content []apiBlock `json:"content"`
}

type apiBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// image
	Source *apiImageSource `json:"source,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type apiImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type apiTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Stream 发起一次流式请求。
func (c *Client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("anthropic: 编码请求失败：%w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: 构造请求失败：%w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", c.version)
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}
	if c.betas != "" {
		httpReq.Header.Set("anthropic-beta", c.betas)
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
		pe := provider.ClassifyHTTPError(resp.StatusCode, extractError(string(raw)))
		pe.RetryAfter = provider.ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, pe
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	return &messageStream{body: resp.Body, scanner: scanner, tools: map[int]*toolBuilder{}}, nil
}

func (c *Client) buildRequest(req provider.Request) apiRequest {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTok
	}
	out := apiRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Stream:    true,
	}
	system, messages := translate(req.System, req.Messages)
	out.System = system
	out.Messages = messages

	if req.Temperature > 0 {
		temp := req.Temperature
		out.Temperature = &temp
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, apiTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}
	return out
}

// translate 把统一消息模型翻译成 Anthropic 形态。
//
// 关键点：连续的工具回执合并成一条 user 消息；system 消息在开头并入顶层 system，
// 出现在中间时原地转成 user 文本（Anthropic 不接受对话中段的 system 角色）。
func translate(system string, msgs []core.Message) (string, []apiMessage) {
	var systemParts []string
	if strings.TrimSpace(system) != "" {
		systemParts = append(systemParts, system)
	}

	var out []apiMessage
	appendMessage := func(role string, blocks []apiBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			return
		}
		out = append(out, apiMessage{Role: role, Content: blocks})
	}

	for _, m := range msgs {
		switch m.Role {
		case core.System:
			if len(out) == 0 {
				systemParts = append(systemParts, m.Text())
			} else {
				appendMessage("user", []apiBlock{{Type: "text", Text: m.Text()}})
			}
		case core.User:
			appendMessage("user", contentBlocks(m.Content))
		case core.Assistant:
			var blocks []apiBlock
			if strings.TrimSpace(m.Reasoning) != "" && m.ReasoningSignature != "" {
				blocks = append(blocks, apiBlock{Type: "thinking", Thinking: m.Reasoning, Signature: m.ReasoningSignature})
			}
			blocks = append(blocks, contentBlocks(m.Content)...)
			for _, tc := range m.ToolCalls {
				args := json.RawMessage(tc.Arguments)
				if len(args) == 0 || !json.Valid(args) {
					args = json.RawMessage(`{}`)
				}
				blocks = append(blocks, apiBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: args})
			}
			appendMessage("assistant", blocks)
		case core.Tool:
			// 工具回执必须放在 user 消息里，并且紧跟在 assistant 的 tool_use 之后。
			appendMessage("user", []apiBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   mustJSON(m.Text()),
			}})
		}
	}
	return strings.Join(systemParts, "\n\n"), out
}

func contentBlocks(blocks []core.Content) []apiBlock {
	var out []apiBlock
	for _, b := range blocks {
		switch b.Type {
		case core.ContentTypeImage:
			out = append(out, apiBlock{Type: "image", Source: imageSource(b)})
		default:
			if b.Content == "" {
				continue
			}
			out = append(out, apiBlock{Type: "text", Text: b.Content})
		}
	}
	return out
}

// imageSource 支持 data URL（base64）与 http(s) URL 两种来源。
func imageSource(c core.Content) *apiImageSource {
	if strings.HasPrefix(c.Content, "data:") {
		if comma := strings.Index(c.Content, ","); comma > 0 {
			meta := strings.TrimPrefix(c.Content[:comma], "data:")
			mime := strings.SplitN(meta, ";", 2)[0]
			data := c.Content[comma+1:]
			if _, err := base64.StdEncoding.DecodeString(data); err == nil {
				if c.MimeType != "" {
					mime = c.MimeType
				}
				return &apiImageSource{Type: "base64", MediaType: mime, Data: data}
			}
		}
	}
	return &apiImageSource{Type: "url", URL: c.Content}
}

func mustJSON(s string) json.RawMessage {
	raw, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return raw
}

// extractError 从 Anthropic 错误体里取出 message。
func extractError(body string) string {
	var probe struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &probe); err == nil && probe.Error.Message != "" {
		return probe.Error.Message
	}
	return body
}

// ---------------------------------------------------------------- 流实现

type toolBuilder struct {
	id   string
	name string
	args strings.Builder
}

type messageStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	closed  bool

	pending []provider.Chunk
	tools   map[int]*toolBuilder
	order   []int

	usage     core.Usage
	finish    string
	requestID string
	done      bool
}

func (s *messageStream) Recv() (provider.Chunk, error) {
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
				s.finishStream()
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

// nextEvent 读取一个 SSE 帧，返回 event 名与 data 内容。
func (s *messageStream) nextEvent() (string, string, error) {
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
			data = append(data, strings.TrimPrefix(line, "data:"))
		}
	}
}

func (s *messageStream) handle(event, data string) (*provider.Chunk, error) {
	switch event {
	case "error":
		return nil, &provider.Error{Kind: provider.KindUpstream, Message: extractError(data)}
	case "message_start":
		var payload struct {
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					InputTokens          int `json:"input_tokens"`
					CacheReadInputTokens int `json:"cache_read_input_tokens"`
					CacheCreationTokens  int `json:"cache_creation_input_tokens"`
					OutputTokens         int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if payload.Message.ID != "" {
				s.requestID = payload.Message.ID
			}
			u := payload.Message.Usage
			s.usage.Input = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationTokens
			s.usage.CachedInput = u.CacheReadInputTokens
			s.usage.Output = u.OutputTokens
		}
		return nil, nil

	case "content_block_start":
		var payload struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Thinking  string `json:"thinking"`
				Signature string `json:"signature"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, nil
		}
		if payload.ContentBlock.Type == "tool_use" {
			s.tools[payload.Index] = &toolBuilder{id: payload.ContentBlock.ID, name: payload.ContentBlock.Name}
			s.order = append(s.order, payload.Index)
		}
		return nil, nil

	case "content_block_delta":
		var payload struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				Signature   string `json:"signature"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, nil
		}
		switch payload.Delta.Type {
		case "text_delta":
			return &provider.Chunk{Kind: provider.ChunkText, Text: payload.Delta.Text}, nil
		case "thinking_delta":
			return &provider.Chunk{Kind: provider.ChunkReasoning, Text: payload.Delta.Thinking}, nil
		case "input_json_delta":
			if b, ok := s.tools[payload.Index]; ok {
				b.args.WriteString(payload.Delta.PartialJSON)
			}
		case "signature_delta":
			if payload.Delta.Signature != "" {
				return &provider.Chunk{Kind: provider.ChunkSignature, Signature: payload.Delta.Signature}, nil
			}
		}
		return nil, nil

	case "content_block_stop":
		var payload struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, nil
		}
		if b, ok := s.tools[payload.Index]; ok {
			args := b.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			call := core.ToolCall{ID: b.id, Name: b.name, Arguments: args}
			delete(s.tools, payload.Index)
			return &provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: &call}, nil
		}
		return nil, nil

	case "message_delta":
		var payload struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if payload.Delta.StopReason != "" {
				s.finish = mapStopReason(payload.Delta.StopReason)
			}
			if payload.Usage.OutputTokens > 0 {
				s.usage.Output = payload.Usage.OutputTokens
			}
		}
		return nil, nil

	case "message_stop":
		s.finishStream()
		return nil, nil
	}
	return nil, nil
}

func (s *messageStream) finishStream() {
	if s.done {
		return
	}
	s.done = true
	// 兜底：某些网关不发 content_block_stop，这里把残留的工具调用补齐。
	for _, idx := range s.order {
		if b, ok := s.tools[idx]; ok {
			args := b.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			call := core.ToolCall{ID: b.id, Name: b.name, Arguments: args}
			s.pending = append(s.pending, provider.Chunk{Kind: provider.ChunkToolCall, ToolCall: &call})
			delete(s.tools, idx)
		}
	}
	finish := s.finish
	if finish == "" {
		finish = "stop"
	}
	usage := s.usage
	s.pending = append(s.pending, provider.Chunk{Kind: provider.ChunkDone, FinishReason: finish, Usage: &usage, RequestID: s.requestID})
}

func mapStopReason(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "end_turn", "stop_sequence", "pause_turn":
		return "stop"
	default:
		return reason
	}
}

func (s *messageStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}
