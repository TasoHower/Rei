package core

import "testing"

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantErr bool
	}{
		{"system 消息", NewSystemMessage("简洁回答"), false},
		{"user 消息", NewUserTextMessage("你好"), false},
		{"assistant 工具调用", NewAssistantMessage(nil, "", []ToolCall{{ID: "c1", Name: "read_file", Arguments: `{}`}}), false},
		{"tool 回执", NewToolMessage("c1", "read_file", "ok"), false},
		{"tool 消息缺 id", Message{Role: Tool, Content: []Content{{Type: ContentTypeText, Content: "x"}}}, true},
		{"user 带调用 id", Message{Role: User, ToolCallID: "c1"}, true},
		{"user 带工具调用", Message{Role: User, ToolCalls: []ToolCall{{ID: "c1"}}}, true},
		{"非法角色", Message{Role: "robot"}, true},
		{"非法内容块", Message{Role: User, Content: []Content{{Type: "video"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.msg.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() 返回 %v，期望错误=%v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateConversationPairing(t *testing.T) {
	ok := []Message{
		NewUserTextMessage("读一下 a.txt"),
		NewAssistantMessage(nil, "", []ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`}}),
		NewToolMessage("c1", "read_file", "hello"),
		NewAssistantMessage([]Content{{Type: ContentTypeText, Content: "完成"}}, "", nil),
	}
	if err := ValidateConversation(ok); err != nil {
		t.Fatalf("合法对话被判为非法：%v", err)
	}

	unanswered := ok[:2]
	if err := ValidateConversation(unanswered); err == nil {
		t.Fatal("缺少工具回执时应当报错")
	}

	orphan := []Message{NewToolMessage("c9", "read_file", "x")}
	if err := ValidateConversation(orphan); err == nil {
		t.Fatal("孤立工具回执应当报错")
	}
}

func TestMessageText(t *testing.T) {
	m := NewUserMessage(
		Content{Type: ContentTypeText, Content: "你好 "},
		Content{Type: ContentTypeImage, Content: "data:image/png;base64,xx"},
		Content{Type: ContentTypeText, Content: "世界"},
	)
	if got := m.Text(); got != "你好 世界" {
		t.Fatalf("Text() = %q", got)
	}
}

func TestCostAndPricing(t *testing.T) {
	u := PricedUsage(Usage{Input: 1_000_000, Output: 500_000}, Pricing{Input: 250_000, Output: 1_000_000})
	if u.Cost.Input != 250_000 {
		t.Fatalf("输入成本 = %d，期望 250000 µUSD", u.Cost.Input)
	}
	if u.Cost.Output != 500_000 {
		t.Fatalf("输出成本 = %d，期望 500000 µUSD", u.Cost.Output)
	}
	if u.Cost.Total() != 750_000 {
		t.Fatalf("总成本 = %d", u.Cost.Total())
	}

	// 缓存命中的输入按更低单价计费，且命中数不会超过输入总数：
	// 1000 token × 0.1 美元/百万 = 100 µUSD。
	cached := PricedUsage(Usage{Input: 1000, CachedInput: 1500}, Pricing{Input: 1_000_000, CachedIn: 100_000})
	if cached.Cost.Input != 100 {
		t.Fatalf("缓存成本 = %d，期望 100", cached.Cost.Input)
	}
}

func TestUsageAdd(t *testing.T) {
	var total Usage
	total.Add(Usage{Input: 10, Output: 5, Cost: Cost{Input: 100}})
	total.Add(Usage{Input: 1, CachedInput: 9, Output: 2, Cost: Cost{Output: 50}})
	if total.Input != 11 || total.CachedInput != 9 || total.Output != 7 {
		t.Fatalf("累加结果不对：%+v", total)
	}
	if total.Cost.Total() != 150 {
		t.Fatalf("成本合计 = %d", total.Cost.Total())
	}
}
