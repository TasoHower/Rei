package trace

import (
	"testing"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
)

func TestWaitSpanRetriesAndRequestID(t *testing.T) {
	t0 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	b := func(idx int) events.Base {
		return events.Base{RunID: "r1", Index: idx, Time: t0.Add(time.Duration(idx) * 10 * time.Millisecond)}
	}
	evs := []events.Event{
		&events.AgentStart{Base: b(1), Model: "m"},
		&events.LLMStart{Base: b(2), Model: "m"},
		&events.LLMError{Base: b(3), Kind: "rate_limit", Message: "429", Retryable: true, Attempt: 1},
		&events.LLMEnd{Base: b(4), Model: "m", FinishReason: "tool_calls",
			Usage: core.Usage{Input: 10, Output: 2}, DurationMS: 20, RequestID: "req-1"},
		&events.ToolRunStart{Base: b(5), CallID: "call-1", Name: "bash", Arguments: `{}`},
		&events.ToolApprovalRequested{Base: b(6), CallID: "call-1", Name: "bash"},
		&events.ToolApprovalResolved{Base: b(8), CallID: "call-1", Approved: true},
		&events.ToolRunEnd{Base: b(9), CallID: "call-1", Name: "bash", Content: "ok", DurationMS: 5},
		&events.AgentEnd{Base: b(10), FinishReason: "stop", DurationMS: 90},
	}

	res := Build(evs, 10)
	var wait, llm *Span
	for i := range res.Traces[0].Spans {
		switch res.Traces[0].Spans[i].Kind {
		case KindWait:
			wait = &res.Traces[0].Spans[i]
		case KindLLM:
			llm = &res.Traces[0].Spans[i]
		}
	}

	if wait == nil {
		t.Fatal("缺少审批等待 span")
	}
	// 审批从 idx=6 到 idx=8，各 10ms，所以等待应为 20ms。
	if wait.DurMS != 20 || wait.ParentID != "r1" {
		t.Fatalf("等待 span = %+v", wait)
	}
	if llm == nil || llm.Retries != 1 || llm.RequestID != "req-1" {
		t.Fatalf("llm span = %+v", llm)
	}
	if llm.Status != "ok" {
		t.Fatalf("重试成功后应回到 ok：%+v", llm)
	}

	if _, ok := res.Detail(KindLLM, "req-1"); !ok {
		t.Fatal("应能按 request_id 回查 LLM 详情")
	}
	if _, ok := res.Detail(KindWait, "call-1"); !ok {
		t.Fatal("应能按 call_id 回查等待详情")
	}
	if res.MetaBytes <= 0 {
		t.Fatalf("MetaBytes = %d", res.MetaBytes)
	}
	if res.Summaries[0].Errors != 0 {
		t.Fatalf("等待与服务调用都成功，不应计错误：%+v", res.Summaries[0])
	}
}

func TestRejectedApprovalMarksWaitAsError(t *testing.T) {
	t0 := time.Now()
	evs := []events.Event{
		&events.AgentStart{Base: events.Base{RunID: "r1", Index: 1, Time: t0}, Model: "m"},
		&events.ToolApprovalRequested{Base: events.Base{RunID: "r1", Index: 2, Time: t0}, CallID: "c1"},
		&events.ToolApprovalResolved{Base: events.Base{RunID: "r1", Index: 3, Time: t0.Add(5 * time.Millisecond)}, CallID: "c1", Approved: false},
		&events.AgentEnd{Base: events.Base{RunID: "r1", Index: 4, Time: t0}, FinishReason: "stop"},
	}
	res := Build(evs, 10)
	if res.Summaries[0].Errors != 1 {
		t.Fatalf("被拒审批应计入错误：%+v", res.Summaries[0])
	}
}
