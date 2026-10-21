package compress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// KeepRecent 是逐字保留的最近消息条数（KeepTokens 为 0 时生效）。
	KeepRecent int
	// KeepTokens 是逐字保留的近窗 token 预算；0 表示按 KeepRecent 条数。
	// 推荐值：ContextWindow/10 且不低于 8000。
	KeepTokens int
	// MaxTokens 是摘要本身的输出上限，默认 1024。
	MaxTokens int
	// MaxTranscriptRunes 是送入摘要器的历史长度上限，默认 60000。
	MaxTranscriptRunes int
}

// 摘要质量要求：至少覆盖三段。缺段的摘要会把任务目标或文件改动吞掉。
var requiredSections = []string{
	"Task and goal",
	"Completed changes",
	"Key facts and decisions",
	"Remaining work and next steps",
	"Constraints and user preferences",
}

// Compress 实现 Compressor。任何失败都原样返回错误，由调用方保留原上下文。
func (c LLM) Compress(ctx context.Context, messages []core.Message) (Result, error) {
	if c.Provider == nil {
		return Result{}, errors.New("compress: 未配置 Provider")
	}
	old, recent := c.split(messages)
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

	input := "Conversation history to compress:\n\n" + transcript(old, limit)
	if files := fileDigest(old); files != "" {
		input += "\nFiles touched in this history:\n" + files
	}
	if anchor := lastUserText(old); anchor != "" {
		input += "\nMost recent user instruction (keep it verbatim in the summary): " + truncateRunes(anchor, 800)
	}

	var totalUsage core.Usage
	for attempt := 1; attempt <= 2; attempt++ {
		res, err := provider.Complete(ctx, c.Provider, provider.Request{
			Model:     c.Model,
			System:    summarySystemPrompt,
			Messages:  []core.Message{core.NewUserTextMessage(input)},
			MaxTokens: maxTokens,
		})
		if err != nil {
			return Result{}, err
		}
		totalUsage.Add(res.Usage)

		text := strings.TrimSpace(res.Text)
		if text == "" {
			return Result{}, errors.New("compress: 摘要为空")
		}
		if missing := missingSections(text); len(missing) > 0 {
			if attempt == 1 {
				// 质量重试：明确告诉模型缺了哪些段落。
				input += "\n\nYour previous summary was rejected. It was missing these sections: " +
					strings.Join(missing, ", ") + ". Rewrite the summary and include every section."
				continue
			}
			return Result{}, fmt.Errorf("compress: 摘要缺少段落 %s", strings.Join(missing, ", "))
		}

		out := make([]core.Message, 0, len(recent)+1)
		out = append(out, core.NewSummaryMessage("[Conversation summary]\n"+text))
		out = append(out, recent...)
		return Result{Messages: out, Usage: totalUsage}, nil
	}
	return Result{}, errors.New("compress: 摘要重试后仍不合格")
}

func (c LLM) split(messages []core.Message) (old, recent []core.Message) {
	if c.KeepTokens > 0 {
		return splitRecentTokens(messages, c.KeepTokens)
	}
	return splitRecent(messages, c.keepRecent())
}

func (c LLM) keepRecent() int {
	if c.KeepRecent <= 0 {
		return 6
	}
	return c.KeepRecent
}

func missingSections(text string) []string {
	lower := strings.ToLower(text)
	var missing []string
	for _, section := range requiredSections {
		if !strings.Contains(lower, strings.ToLower(section)) {
			missing = append(missing, section)
		}
	}
	return missing
}

// fileDigest 汇总历史里被读写的文件，让摘要保留「改过哪些文件」。
func fileDigest(msgs []core.Message) string {
	seen := map[string]bool{}
	var out []string
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			switch tc.Name {
			case "write_file", "edit_file", "read_file":
			default:
				continue
			}
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil || args.Path == "" {
				continue
			}
			line := tc.Name + " " + args.Path
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, "- "+line)
		}
	}
	return strings.Join(out, "\n")
}
