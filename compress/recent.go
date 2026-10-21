package compress

import (
	"context"
	"fmt"
	"strings"

	"github.com/tasohower/rei/core"
)

// RecentWindow 是不依赖模型的压缩器：把旧消息折叠成一份「事实清单」，
// 保留近窗原文。它适合离线运行、测试与低预算场景；摘要质量不如 LLM 版，
// 但行为完全确定。
type RecentWindow struct {
	Trigger
	// KeepRecent 是逐字保留的最近消息条数（KeepTokens 为 0 时生效）。
	KeepRecent int
	// KeepTokens 是逐字保留的近窗 token 预算。
	KeepTokens int
}

// Compress 实现 Compressor。
func (c RecentWindow) Compress(_ context.Context, messages []core.Message) (Result, error) {
	var old, recent []core.Message
	if c.KeepTokens > 0 {
		old, recent = splitRecentTokens(messages, c.KeepTokens)
	} else {
		old, recent = splitRecent(messages, c.recent())
	}
	if len(old) == 0 {
		return Result{Messages: append([]core.Message(nil), messages...)}, nil
	}

	var files []string
	seen := map[string]bool{}
	userTurns, toolCalls := 0, 0
	for _, m := range old {
		switch m.Role {
		case core.User:
			userTurns++
		case core.Assistant:
			for _, tc := range m.ToolCalls {
				toolCalls++
				if isPathLike(tc.Arguments) && !seen[tc.Name] {
					seen[tc.Name] = true
					files = append(files, tc.Name+" "+truncateRunes(tc.Arguments, 200))
				}
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Conversation summary] Compressed %d earlier messages (user turns: %d, tool calls: %d).\n", len(old), userTurns, toolCalls)
	if len(files) > 0 {
		b.WriteString("Tools and arguments involved:\n")
		for _, f := range files {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	if lastUser := lastUserText(old); lastUser != "" {
		b.WriteString("Most recent user instruction before compaction: ")
		b.WriteString(truncateRunes(lastUser, 800))
		b.WriteString("\n")
	}

	out := make([]core.Message, 0, len(recent)+1)
	out = append(out, core.NewSummaryMessage(b.String()))
	out = append(out, recent...)
	return Result{Messages: out}, nil
}

func (c RecentWindow) recent() int {
	if c.KeepRecent <= 0 {
		return 6
	}
	return c.KeepRecent
}

func isPathLike(args string) bool {
	return strings.Contains(args, "/") || strings.Contains(args, ".")
}

func lastUserText(msgs []core.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != core.User {
			continue
		}
		if text := strings.TrimSpace(msgs[i].Text()); text != "" {
			return text
		}
	}
	return ""
}
