// Package provider 定义模型接入层：一次请求、一条流、一套错误语义与退避策略。
//
// Rei 只要求实现 Stream 一个方法。仓库自带 OpenAI 兼容实现（/chat/completions），
// 因为绝大多数国内外网关都提供这个协议；需要 Anthropic Messages 或 Responses
// 协议时，实现同一个接口即可，agents 层不需要改动。
package provider

import (
	"context"
	"encoding/json"

	"github.com/tasohower/rei/core"
)

// Tool 是发给模型的工具声明。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request 是一次模型调用。
type Request struct {
	Model    string
	System   string
	Messages []core.Message
	Tools    []Tool

	MaxTokens   int
	Temperature float64
}

// Chunk 是流式增量。Kind 取值见下方常量。
type Chunk struct {
	Kind string
	Text string
	// ToolCall 只在 Kind == ChunkToolCall 时有值，且总是完整的一次调用
	// （id / name / arguments 已拼装完成）。
	ToolCall *core.ToolCall
	// FinishReason 只在 Kind == ChunkDone 时有值。
	FinishReason string
	// Usage 只在 Kind == ChunkDone 时可能非 nil（取决于上游是否回传用量）。
	Usage *core.Usage
}

// 增量类型。
const (
	ChunkText      = "text"
	ChunkReasoning = "reasoning"
	ChunkToolCall  = "tool_call"
	ChunkDone      = "done"
)

// Stream 是一条模型输出流。Recv 返回 io.EOF 表示正常结束；
// 实现必须保证 Close 幂等。
type Stream interface {
	Recv() (Chunk, error)
	Close() error
}

// Provider 创建一个流。
type Provider interface {
	Stream(ctx context.Context, req Request) (Stream, error)
}

// Result 是一次非流式调用的汇总。
type Result struct {
	Text         string
	Reasoning    string
	ToolCalls    []core.ToolCall
	FinishReason string
	Usage        core.Usage
}
