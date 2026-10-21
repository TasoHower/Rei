package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/tools"
)

// askUserTool 让模型发起结构化提问（1~8 个问题，可带选项）。
//
// 它依赖运行注入的 events.Asker：没有配置提问器时直接返回错误，
// 而不是假装问过（那会让模型继续基于假设工作）。
type askUserTool struct{}

func (t *askUserTool) Name() string { return "ask_user" }

func (t *askUserTool) Description() string {
	return "Ask the human 1-8 structured questions and wait for the answers. Use it when a decision cannot be inferred from the repository."
}

func (t *askUserTool) CanParallel() bool { return false }

func (t *askUserTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","properties":{"header":{"type":"string","description":"Short label (max 12 chars)."},"question":{"type":"string"},"options":{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},"description":{"type":"string"}},"required":["label"]}},"multi_select":{"type":"boolean"}},"required":["question"]}}},"required":["questions"]}`)
}

func (t *askUserTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Questions []events.Question `json:"questions"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	if len(args.Questions) == 0 {
		return tools.ErrorResult("at least one question is required")
	}
	if len(args.Questions) > 8 {
		return tools.ErrorResult("at most 8 questions are allowed")
	}

	info, _ := events.RunInfoFrom(ctx)
	if info.Asker == nil {
		return tools.ErrorResult("no asker is configured for this run; decide from the repository or ask in the final answer instead")
	}

	events.Emit(ctx, info.Emit, &events.AskUserQuestion{
		Base:      events.Base{RunID: info.RunID, ParentRunID: info.ParentRunID, Depth: info.Depth, Time: time.Now()},
		CallID:    call.ID,
		Questions: args.Questions,
	})

	answers, err := info.Asker.Ask(ctx, args.Questions)
	if err != nil {
		return tools.ErrorResult("ask failed: %v", err)
	}
	if len(answers) == 0 {
		return tools.ErrorResult("the human dismissed the questions; do not assume an answer")
	}

	var b strings.Builder
	for i, ans := range answers {
		q := ""
		if i < len(args.Questions) {
			q = args.Questions[i].Question
		}
		fmt.Fprintf(&b, "%d. %s\n   -> %s\n", i+1, q, ans)
	}
	return tools.Result{Content: strings.TrimRight(b.String(), "\n")}
}
