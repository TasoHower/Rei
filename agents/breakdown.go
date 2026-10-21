package agents

import (
	"unicode/utf8"

	"github.com/tasohower/rei/core"
)

// Breakdown 是上下文构成：按角色拆分的估算 token。
//
// 它回答的是「上下文被谁吃掉了」：系统提示、历史、工具结果还是工具参数。
type Breakdown struct {
	System    int `json:"system"`
	User      int `json:"user"`
	Assistant int `json:"assistant"`
	Tool      int `json:"tool"`
	ToolCalls int `json:"tool_calls"`
	Total     int `json:"total"`
}

// ContextBreakdown 估算一段上下文的构成。口径与 compress.EstimateTokens 一致：
// ASCII 约 4 字符/token，CJK 约 2 字符/token。
func ContextBreakdown(msgs []core.Message) Breakdown {
	var b Breakdown
	for _, m := range msgs {
		text := estimate(m.Text())
		switch m.Role {
		case core.System:
			b.System += text
		case core.User:
			b.User += text
		case core.Assistant:
			b.Assistant += text
		case core.Tool:
			b.Tool += text
		}
		for _, tc := range m.ToolCalls {
			b.ToolCalls += estimate(tc.Name) + estimate(tc.Arguments) + 8
		}
	}
	b.Total = b.System + b.User + b.Assistant + b.Tool + b.ToolCalls
	return b
}

func estimate(s string) int {
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
