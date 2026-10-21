// Package core 存放各层共享的零依赖类型：消息、工具调用、用量与成本。
// 它不 import 本项目其它包，因此 provider、tools、agents 与宿主都能安全引用。
package core

import (
	"errors"
	"fmt"
)

// MessageRole 是消息在会话中的角色。
type MessageRole string

const (
	User      MessageRole = "user"
	Tool      MessageRole = "tool"
	Assistant MessageRole = "assistant"
	System    MessageRole = "system"
)

// 内容块类型（Content.Type）。
const (
	ContentTypeText    = "text"
	ContentTypeImage   = "image"
	ContentTypeSummary = "summary" // 压缩产物，发给模型时按文本处理
)

// Message 是全链路共享的唯一会话消息模型。
//
// 角色与字段的不变量由构造器和 Validate 保证：tool 消息必须携带它所应答的
// 调用 id，只有 assistant 消息可以携带工具调用，内容块类型必须在白名单内。
type Message struct {
	Role    MessageRole `json:"role"`
	Content []Content   `json:"content,omitempty"`

	// Reasoning 是 assistant 的思考内容。推理模型在同一轮里既思考又发起工具调用时，
	// 这段内容必须原样回传，否则上游接口会拒绝请求。
	Reasoning string `json:"reasoning,omitempty"`
	// Model 记录产出该消息的模型，用于判断思考签名是否可跨模型回传。
	Model string `json:"model,omitempty"`

	// ToolCalls 出现在发起工具调用的 assistant 消息上。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 出现在 tool 消息上，与某个 ToolCall 配对。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolName 是产出该 tool 消息的工具名。
	ToolName string `json:"tool_name,omitempty"`
}

// Content 是消息里的一个内容块，文本与图片可以混排。
type Content struct {
	Type     string `json:"type"`
	Content  string `json:"content"`
	MimeType string `json:"mime_type,omitempty"`
}

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // 原始 JSON 对象
}

// NewSystemMessage 构造纯文本 system 消息。
func NewSystemMessage(text string) Message {
	return Message{Role: System, Content: []Content{{Type: ContentTypeText, Content: text}}}
}

// NewUserMessage 用内容块构造 user 消息。
func NewUserMessage(content ...Content) Message {
	return Message{Role: User, Content: content}
}

// NewUserTextMessage 是最常见的形态：单个文本块。
func NewUserTextMessage(text string) Message {
	return NewUserMessage(Content{Type: ContentTypeText, Content: text})
}

// NewAssistantMessage 构造 assistant 消息。纯工具调用轮的 content 可以为空；
// reasoning 与 toolCalls 必须原样保留。
func NewAssistantMessage(content []Content, reasoning string, toolCalls []ToolCall) Message {
	return Message{Role: Assistant, Content: content, Reasoning: reasoning, ToolCalls: toolCalls}
}

// NewToolMessage 构造一次工具调用的回执。
func NewToolMessage(toolCallID, toolName, text string) Message {
	return Message{
		Role:       Tool,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    []Content{{Type: ContentTypeText, Content: text}},
	}
}

// NewSummaryMessage 构造压缩摘要消息（system 角色）。
func NewSummaryMessage(text string) Message {
	return Message{Role: System, Content: []Content{{Type: ContentTypeSummary, Content: text}}}
}

// Text 拼接消息里的全部文本块。
func (m *Message) Text() string {
	out := ""
	for _, c := range m.Content {
		if c.Type == ContentTypeText || c.Type == ContentTypeSummary {
			out += c.Content
		}
	}
	return out
}

// HasToolCalls 判断该轮是否发起了工具调用。
func (m *Message) HasToolCalls() bool { return len(m.ToolCalls) > 0 }

// Validate 校验单条消息的角色与字段不变量。
func (m *Message) Validate() error {
	switch m.Role {
	case System, User, Assistant, Tool:
	default:
		return fmt.Errorf("非法角色 %q", m.Role)
	}
	if m.Role == Tool && m.ToolCallID == "" {
		return errors.New("tool 消息必须携带 ToolCallID")
	}
	if m.Role != Tool && m.ToolCallID != "" {
		return fmt.Errorf("角色 %s 不能携带 ToolCallID", m.Role)
	}
	if m.Role != Assistant && len(m.ToolCalls) > 0 {
		return fmt.Errorf("角色 %s 不能携带 ToolCalls", m.Role)
	}
	for i, c := range m.Content {
		switch c.Type {
		case "", ContentTypeText, ContentTypeImage, ContentTypeSummary:
		default:
			return fmt.Errorf("content[%d] 的类型 %q 非法", i, c.Type)
		}
	}
	return nil
}

// ValidateConversation 校验整段对话，并检查「工具调用必须有且仅有一次回执」。
func ValidateConversation(msgs []Message) error {
	pending := map[string]bool{}
	for i := range msgs {
		if err := msgs[i].Validate(); err != nil {
			return fmt.Errorf("message[%d]: %w", i, err)
		}
		for _, tc := range msgs[i].ToolCalls {
			if tc.ID == "" {
				return fmt.Errorf("message[%d]: 工具调用缺少 id", i)
			}
			pending[tc.ID] = true
		}
		if msgs[i].Role == Tool {
			if !pending[msgs[i].ToolCallID] {
				return fmt.Errorf("message[%d]: 孤立的工具回执 %q", i, msgs[i].ToolCallID)
			}
			delete(pending, msgs[i].ToolCallID)
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("有 %d 个工具调用没有回执", len(pending))
	}
	return nil
}
