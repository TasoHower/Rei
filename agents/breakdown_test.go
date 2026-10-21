package agents

import (
	"testing"

	"github.com/tasohower/rei/core"
)

func TestContextBreakdownByRole(t *testing.T) {
	msgs := []core.Message{
		core.NewSystemMessage("system prompt"),
		core.NewUserTextMessage("用户问题"),
		core.NewAssistantMessage(nil, "", []core.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`}}),
		core.NewToolMessage("c1", "read_file", "工具结果内容"),
	}
	b := ContextBreakdown(msgs)
	if b.System == 0 || b.User == 0 || b.Tool == 0 || b.ToolCalls == 0 {
		t.Fatalf("构成 = %+v", b)
	}
	if b.Assistant != 0 {
		t.Fatalf("纯工具调用轮不应有正文：%+v", b)
	}
	if b.Total != b.System+b.User+b.Assistant+b.Tool+b.ToolCalls {
		t.Fatalf("Total 不等于分项之和：%+v", b)
	}
}
