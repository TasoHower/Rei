package provider

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
)

// OpenAIConfig 是 OpenAI 兼容端点的配置。
type OpenAIConfig struct {
	// BaseURL 例如 https://api.deepseek.com/v1（不带结尾斜杠也可）。
	BaseURL string
	// APIKey 为空时不发 Authorization 头（适合本地网关）。
	APIKey string
	// Path 默认 /chat/completions。
	Path string
	// HTTPClient 可选；默认 120s 整体超时。
	HTTPClient *http.Client
	// Headers 追加到每个请求（例如网关的自定义鉴权头）。
	Headers map[string]string
}

// OpenAI 是 OpenAI 兼容的 /chat/completions 流式客户端。
type OpenAI struct {
	baseURL string
	apiKey  string
	path    string
	client  *http.Client
	headers map[string]string
}

// NewOpenAI 构造客户端。
func NewOpenAI(cfg OpenAIConfig) (*OpenAI, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("provider: BaseURL 不能为空")
	}
	path := cfg.Path
	if path == "" {
		path = "/chat/completions"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	return &OpenAI{baseURL: base, apiKey: cfg.APIKey, path: path, client: client, headers: cfg.Headers}, nil
}

// ---------------------------------------------------------------- 请求结构

type oaiMessage struct {
	Role             string        `json:"role"`
	Content          any           `json:"content,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	Name             string        `json:"name,omitempty"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaiRequest struct {
	Model         string       `json:"model"`
	Messages      []oaiMessage `json:"messages"`
	Stream        bool         `json:"stream"`
	MaxTokens     int          `json:"max_tokens,omitempty"`
	Temperature   float64      `json:"temperature,omitempty"`
	Tools         []oaiTool    `json:"tools,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

// ---------------------------------------------------------------- 响应结构

type oaiChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// Stream 发起一次流式请求。
func (o *OpenAI) Stream(ctx context.Context, req Request) (Stream, error) {
	body, err := json.Marshal(o.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("provider: 编码请求失败：%w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+o.path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider: 构造请求失败：%w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	for k, v := range o.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, &Error{Kind: KindCanceled, Message: ctxErr.Error()}
		}
		return nil, &Error{Kind: KindGeneric, Message: err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		pe := ClassifyHTTPError(resp.StatusCode, string(raw))
		pe.RetryAfter = ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, pe
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	return &openaiStream{body: resp.Body, scanner: sc, calls: map[int]*toolAcc{}}, nil
}

func (o *OpenAI) buildRequest(req Request) oaiRequest {
	out := oaiRequest{
		Model:       req.Model,
		Messages:    toOAIMessages(req.System, req.Messages),
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	out.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	for _, t := range req.Tools {
		var tool oaiTool
		tool.Type = "function"
		tool.Function.Name = t.Name
		tool.Function.Description = t.Description
		tool.Function.Parameters = t.Parameters
		out.Tools = append(out.Tools, tool)
	}
	return out
}

func toOAIMessages(system string, msgs []core.Message) []oaiMessage {
	var out []oaiMessage
	if system != "" {
		out = append(out, oaiMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		om := oaiMessage{
			Role:             string(m.Role),
			ReasoningContent: m.Reasoning,
			ToolCallID:       m.ToolCallID,
			Name:             m.ToolName,
		}
		om.Content = contentValue(m.Content)
		for _, tc := range m.ToolCalls {
			var otc oaiToolCall
			otc.ID = tc.ID
			otc.Type = "function"
			otc.Function.Name = tc.Name
			otc.Function.Arguments = tc.Arguments
			om.ToolCalls = append(om.ToolCalls, otc)
		}
		out = append(out, om)
	}
	return out
}

// contentValue 把内容块翻译成 OpenAI 形态：纯文本用字符串（缓存前缀更稳定），
// 含图片时用 parts 数组。
func contentValue(blocks []core.Content) any {
	if len(blocks) == 0 {
		return nil
	}
	hasImage := false
	for _, b := range blocks {
		if b.Type == core.ContentTypeImage {
			hasImage = true
			break
		}
	}
	if !hasImage {
		var b strings.Builder
		for _, c := range blocks {
			b.WriteString(c.Content)
		}
		return b.String()
	}
	var parts []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case core.ContentTypeImage:
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": b.Content},
			})
		default:
			parts = append(parts, map[string]any{"type": "text", "text": b.Content})
		}
	}
	return parts
}

// ---------------------------------------------------------------- 流实现

type toolAcc struct {
	id   string
	name string
	args strings.Builder
}

type openaiStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	closed  bool

	pending []Chunk
	calls   map[int]*toolAcc
	order   []int
	usage   *core.Usage
	finish  string
	sawDone bool
}

// Recv 逐个返回增量。实现要点：文本/思考增量即时返回；工具调用按 index 拼装，
// 在流结束（或 [DONE]）时一次性发出完整调用，随后发出 done。
func (s *openaiStream) Recv() (Chunk, error) {
	for {
		if len(s.pending) > 0 {
			ch := s.pending[0]
			s.pending = s.pending[1:]
			return ch, nil
		}
		if s.sawDone {
			return Chunk{}, io.EOF
		}
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				return Chunk{}, &Error{Kind: KindUpstream, Message: "读取流失败：" + err.Error()}
			}
			s.finalize()
			continue
		}
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			s.finalize()
			continue
		}
		var chunk oaiChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 上游偶尔夹杂心跳/非 JSON 行，跳过而不是中断整条流
		}
		if chunk.Usage != nil {
			s.usage = &core.Usage{
				Input:       chunk.Usage.PromptTokens,
				CachedInput: chunk.Usage.PromptTokensDetails.CachedTokens,
				Output:      chunk.Usage.CompletionTokens,
				Reasoning:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			}
		}
		for _, choice := range chunk.Choices {
			if t := choice.Delta.Content; t != "" {
				return Chunk{Kind: ChunkText, Text: t}, nil
			}
			if t := choice.Delta.ReasoningContent; t != "" {
				return Chunk{Kind: ChunkReasoning, Text: t}, nil
			}
			for _, tc := range choice.Delta.ToolCalls {
				acc, ok := s.calls[tc.Index]
				if !ok {
					acc = &toolAcc{}
					s.calls[tc.Index] = acc
					s.order = append(s.order, tc.Index)
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args.WriteString(tc.Function.Arguments)
			}
			if choice.FinishReason != "" {
				s.finish = choice.FinishReason
			}
		}
	}
}

// finalize 把已拼装的工具调用与 done 排进待发队列（幂等）。
func (s *openaiStream) finalize() {
	if s.sawDone {
		return
	}
	s.sawDone = true
	for _, idx := range s.order {
		acc := s.calls[idx]
		if acc.name == "" {
			continue
		}
		call := core.ToolCall{ID: acc.id, Name: acc.name, Arguments: acc.args.String()}
		if call.Arguments == "" {
			call.Arguments = "{}"
		}
		s.pending = append(s.pending, Chunk{Kind: ChunkToolCall, ToolCall: &call})
	}
	done := Chunk{Kind: ChunkDone, FinishReason: s.finish}
	if done.FinishReason == "" {
		done.FinishReason = "stop"
	}
	done.Usage = s.usage
	s.pending = append(s.pending, done)
}

func (s *openaiStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

// Complete 把一条流折叠成一次结果，供压缩器/一次性调用使用。
func Complete(ctx context.Context, p Provider, req Request) (*Result, error) {
	stream, err := p.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	res := &Result{}
	var text, reasoning strings.Builder
	for {
		ch, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch ch.Kind {
		case ChunkText:
			text.WriteString(ch.Text)
		case ChunkReasoning:
			reasoning.WriteString(ch.Text)
		case ChunkToolCall:
			if ch.ToolCall != nil {
				res.ToolCalls = append(res.ToolCalls, *ch.ToolCall)
			}
		case ChunkDone:
			res.FinishReason = ch.FinishReason
			if ch.Usage != nil {
				res.Usage = *ch.Usage
			}
		}
	}
	res.Text = text.String()
	res.Reasoning = reasoning.String()
	if res.FinishReason == "" {
		res.FinishReason = "stop"
	}
	return res, nil
}
