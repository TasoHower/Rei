// Package compress 实现上下文压缩：决定什么时候压、压成什么、失败时怎么办。
//
// 长会话真正的问题不是 token 超限，而是「压完之后模型失忆」。因此这里的两条硬规则是：
//  1. 近窗消息永远逐字保留，摘要只替换更早的部分；
//  2. 压缩失败时保留原上下文（并让调用方决定退避），绝不做「截断式降级」。
package compress

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/tasohower/rei/core"
)

// Compressor 是 agents 循环使用的压缩契约。
type Compressor interface {
	// Decide 判断当前上下文是否需要压缩，返回原因（便于写入事件）。
	Decide(messages []core.Message, lastUsage core.Usage) (bool, string)
	// Compress 返回压缩后的消息列表与本次压缩消耗的用量。
	Compress(ctx context.Context, messages []core.Message) (Result, error)
}

// Result 是一次压缩的产物。
type Result struct {
	Messages []core.Message
	Usage    core.Usage
}

// Trigger 用「真实用量优先、字符估算兜底」判断是否该压缩。
type Trigger struct {
	// ContextWindow 是模型窗口；为 0 时退化为按绝对 token 阈值判断。
	ContextWindow int
	// Threshold 是触发比例，默认 0.8。
	Threshold float64
	// MinMessages 是低于该条数时永不压缩，避免短会话被无谓改写。
	MinMessages int
	// FallbackTokens 是没有窗口信息时使用的绝对阈值。
	FallbackTokens int
}

// Decide 实现 Compressor。
func (t Trigger) Decide(messages []core.Message, lastUsage core.Usage) (bool, string) {
	if len(messages) < max(t.MinMessages, 4) {
		return false, ""
	}
	tokens := lastUsage.Input
	reason := "真实用量"
	if tokens <= 0 {
		tokens = EstimateTokens(messages)
		reason = "字符估算"
	}
	if t.ContextWindow > 0 {
		ratio := t.Threshold
		if ratio <= 0 || ratio >= 1 {
			ratio = 0.8
		}
		limit := int(float64(t.ContextWindow) * ratio)
		if tokens >= limit {
			return true, reason
		}
		return false, ""
	}
	limit := t.FallbackTokens
	if limit <= 0 {
		limit = 4000
	}
	if tokens >= limit {
		return true, reason
	}
	return false, ""
}

// EstimateTokens 是粗略估算：ASCII 约 4 字符/token，CJK 约 2 字符/token。
// 它只在拿不到真实用量时使用。
func EstimateTokens(messages []core.Message) int {
	total := 0
	for _, m := range messages {
		total += 4 // 角色与结构开销
		total += estimateString(m.Reasoning)
		for _, c := range m.Content {
			total += estimateString(c.Content)
		}
		for _, tc := range m.ToolCalls {
			total += estimateString(tc.Arguments) + 8
		}
	}
	return total
}

func estimateString(s string) int {
	if s == "" {
		return 0
	}
	ascii, other := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return ascii/4 + other/2
}

// splitRecent 把消息切成「待摘要的旧消息」与「逐字保留的近窗」。
//
// 近窗以条数为单位，并保证最后一条 user 文本一定留在原文里：
// 那是当前任务的锚点，被摘要掉就等于丢指令。
func splitRecent(messages []core.Message, keep int) (old, recent []core.Message) {
	if keep < 1 {
		keep = 1
	}
	if keep >= len(messages) {
		return nil, append([]core.Message(nil), messages...)
	}
	cut := len(messages) - keep
	// 不要从 assistant 的「工具调用 -> 回执」中间切开：把整组一起挪进近窗。
	for cut > 0 && cut < len(messages) && messages[cut].Role == core.Tool {
		cut--
	}
	if cut <= 0 {
		return nil, append([]core.Message(nil), messages...)
	}
	return messages[:cut], append([]core.Message(nil), messages[cut:]...)
}

// transcript 把消息渲染成给压缩器看的纯文本。
// 标签使用英文，与内置 prompt 保持一致。
func transcript(messages []core.Message, maxRunes int) string {
	var b strings.Builder
	for _, m := range messages {
		if b.Len() > maxRunes {
			b.WriteString("\n... (earlier content omitted)")
			break
		}
		switch m.Role {
		case core.User:
			b.WriteString("User: ")
			b.WriteString(truncateRunes(m.Text(), 1200))
		case core.Assistant:
			if text := m.Text(); text != "" {
				b.WriteString("Assistant: ")
				b.WriteString(truncateRunes(text, 1200))
				b.WriteString("\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString("Tool call: ")
				b.WriteString(tc.Name)
				b.WriteString(" ")
				b.WriteString(truncateRunes(tc.Arguments, 300))
			}
		case core.Tool:
			b.WriteString("Tool result: ")
			b.WriteString(truncateRunes(m.Text(), 600))
		case core.System:
			b.WriteString("System: ")
			b.WriteString(truncateRunes(m.Text(), 600))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
