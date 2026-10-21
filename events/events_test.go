package events

import (
	"context"
	"testing"
	"time"

	"github.com/tasohower/rei/core"
)

func TestEventTypesAreStable(t *testing.T) {
	// 事件名是公开契约：改线上的字符串等于破坏宿主。
	want := map[EventType]string{
		AgentStartType:         "agent_start",
		AgentEndType:           "agent_end",
		LLMStartType:           "llm_start",
		LLMEndType:             "llm_end",
		ToolApprovalType:       "tool_approval_requested",
		CompressEndType:        "compress_end",
		SessionClosedType:      "session_closed",
		UserInputsConsumedType: "user_inputs_consumed",
	}
	for got, expect := range want {
		if string(got) != expect {
			t.Fatalf("事件类型 %q 不等于 %q", got, expect)
		}
	}
}

func TestEmitDeliversTypedEvents(t *testing.T) {
	var got []EventType
	h := func(_ context.Context, e Event) { got = append(got, e.Type()) }

	base := Base{RunID: "r1", RequestID: "req1", Index: 0, Time: time.Now()}
	Emit(context.Background(), h, &AgentStart{Base: base, Model: "m"})
	Emit(context.Background(), h, &ContentChunk{Base: base, Text: "你好"})
	Emit(context.Background(), h, &AgentEnd{Base: base, FinishReason: core.FinishStop})

	if len(got) != 3 || got[0] != AgentStartType || got[1] != ContentChunkType || got[2] != AgentEndType {
		t.Fatalf("事件顺序不对：%v", got)
	}
}

func TestEmitNilHandlerIsSafe(t *testing.T) {
	Emit(context.Background(), nil, &ContentChunk{Text: "x"})
}

func TestEveryEventExposesTypeAndEnvelope(t *testing.T) {
	base := Base{RunID: "r1", Index: 3}
	cases := []struct {
		ev   Event
		want EventType
	}{
		{&AgentStart{Base: base}, AgentStartType},
		{&AgentEnd{Base: base}, AgentEndType},
		{&LLMStart{Base: base}, LLMStartType},
		{&LLMEnd{Base: base}, LLMEndType},
		{&LLMError{Base: base}, LLMErrorType},
		{&ContentChunk{Base: base}, ContentChunkType},
		{&ReasoningChunk{Base: base}, ReasoningChunkType},
		{&ToolRunStart{Base: base}, ToolRunStartType},
		{&ToolRunEnd{Base: base}, ToolRunEndType},
		{&ToolApprovalRequested{Base: base}, ToolApprovalType},
		{&ToolApprovalResolved{Base: base}, ToolApprovalDoneType},
		{&GoalAlignmentReminder{Base: base}, GoalAlignmentRemindType},
		{&CompressStart{Base: base}, CompressStartType},
		{&CompressEnd{Base: base}, CompressEndType},
		{&AskUserQuestion{Base: base}, AskUserQuestionType},
		{&SessionClosed{Base: base}, SessionClosedType},
		{&SessionBudgetExceeded{Base: base}, SessionBudgetExceededType},
		{&SessionRunError{Base: base}, SessionRunErrorType},
		{&SessionPersistError{Base: base}, SessionPersistErrorType},
		{&UserInputsConsumed{Base: base}, UserInputsConsumedType},
		{&TaskStarted{Base: base}, TaskStartedType},
		{&TaskMessage{Base: base}, TaskMessageType},
		{&TaskEnd{Base: base}, TaskEndType},
		{&TaskResultDelivered{Base: base}, TaskResultDeliveredType},
		{&PlanGenerated{Base: base}, PlanGeneratedType},
		{&PlanUpdated{Base: base}, PlanUpdatedType},
		{&PlanStepStarted{Base: base}, PlanStepStartedType},
		{&PlanStepCompleted{Base: base}, PlanStepCompletedType},
	}

	// 事件集合是封闭契约：新增类型必须同步更新本表与 README 的数量说明。
	if len(cases) != 28 {
		t.Fatalf("事件类型数 = %d，期望 28", len(cases))
	}
	for _, c := range cases {
		if got := c.ev.Type(); got != c.want {
			t.Fatalf("Type() = %q，期望 %q", got, c.want)
		}
		if env := c.ev.Envelope(); env == nil || env.RunID != "r1" || env.Index != 3 {
			t.Fatalf("%s 的信封丢失：%+v", c.want, env)
		}
	}
}

func TestBaseAccessor(t *testing.T) {
	base := Base{RunID: "r2", Index: 7}
	e := &ToolRunEnd{Base: base, Name: "bash"}
	if e.Envelope().RunID != "r2" || e.Envelope().Index != 7 {
		t.Fatalf("信封信息丢失：%+v", e.Envelope())
	}
}
