package compress

import (
	"context"
	"errors"
	"strings"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/provider"
)

// summarySystemPrompt 要求模型输出结构化摘要。段落固定，便于下游稳定解析；
// 明确禁止「续写任务」，避免摘要里混入新的指令。
//
// 内置 prompt 使用英文：跨厂商分词与指令微调对英文最稳定。
const summarySystemPrompt = `You are a context compressor. Compress the given conversation history into a factual summary that another model can use to continue the task.

Requirements:
1. Output only the summary body. Do not continue the task, do not answer questions found in the transcript, and do not add pleasantries.
2. Organize the summary into these sections; write "None" for an empty section:
   - Task and goal
   - Completed changes (file paths + key changes)
   - Key facts and decisions
   - Remaining work and next steps
   - Constraints and user preferences
3. Preserve concrete file paths, function names, commands, error messages, and numbers. Do not write "several" or "some".
4. Maximum 800 words.`

// LLM 是调用模型生成摘要的压缩器。近窗原文仍然逐字保留。
type LLM struct {
	Provider provider.Provider
	Model    string
	Trigger
	// KeepRecent 是逐字保留的最近消息条数。
	KeepRecent int
	// MaxTokens 是摘要本身的输出上限，默认 1024。
	MaxTokens int
	// MaxTranscriptRunes 是送入摘要器的历史长度上限，默认 60000。
	MaxTranscriptRunes int
}

// Compress 实现 Compressor。任何失败都原样返回错误，由调用方保留原上下文。
func (c LLM) Compress(ctx context.Context, messages []core.Message) (Result, error) {
	if c.Provider == nil {
		return Result{}, errors.New("compress: 未配置 Provider")
	}
	old, recent := splitRecent(messages, c.keepRecent())
	if len(old) == 0 {
		return Result{Messages: append([]core.Message(nil), messages...)}, nil
	}

	limit := c.MaxTranscriptRunes
	if limit <= 0 {
		limit = 60_000
	}
	maxTokens := c.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	res, err := provider.Complete(ctx, c.Provider, provider.Request{
		Model:     c.Model,
		System:    summarySystemPrompt,
		Messages:  []core.Message{core.NewUserTextMessage("Conversation history to compress:\n\n" + transcript(old, limit))},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return Result{}, err
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return Result{}, errors.New("compress: 摘要为空")
	}

	out := make([]core.Message, 0, len(recent)+1)
	out = append(out, core.NewSummaryMessage("[Conversation summary]\n"+text))
	out = append(out, recent...)
	return Result{Messages: out, Usage: res.Usage}, nil
}

func (c LLM) keepRecent() int {
	if c.KeepRecent <= 0 {
		return 6
	}
	return c.KeepRecent
}
