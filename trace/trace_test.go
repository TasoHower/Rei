package trace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
)

func base(runID string, idx int, at time.Time) events.Base {
	return events.Base{RunID: runID, Index: idx, Time: at}
}

// sampleEvents 构造一条真实形状的事件流：根 run 里一次 LLM、一次工具、
// 一次压缩，然后一个子 run，最后根 run 结束。
func sampleEvents() []events.Event {
	t0 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	b := func(runID string, idx int) events.Base {
		return base(runID, idx, t0.Add(time.Duration(idx)*10*time.Millisecond))
	}

	return []events.Event{
		&events.AgentStart{Base: b("root", 1), Model: "test-model"},
		&events.LLMStart{Base: b("root", 2), Model: "test-model", MessageCount: 1},
		&events.LLMEnd{Base: b("root", 3), Model: "test-model", FinishReason: "tool_calls",
			Usage: core.Usage{Input: 100, CachedInput: 60, Output: 20, Cost: core.Cost{Input: 300, Output: 200}}, DurationMS: 40},
		&events.ToolRunStart{Base: b("root", 4), CallID: "call-1", Name: "read_file", Arguments: `{"path":"机密内容"}`},
		&events.ToolRunEnd{Base: b("root", 5), CallID: "call-1", Name: "read_file",
			Content: "工具正文不应该进投影", IsError: true, DurationMS: 12},
		&events.CompressStart{Base: b("root", 6), MessageCount: 20, Reason: "真实用量"},
		&events.CompressEnd{Base: b("root", 7), MessagesBefore: 20, MessagesAfter: 7,
			Usage: core.Usage{Input: 50, Output: 10}},
		&events.AgentStart{Base: events.Base{RunID: "child", ParentRunID: "root", Depth: 1,
			Index: 8, Time: t0.Add(80 * time.Millisecond)}, Model: "sub-model"},
		&events.LLMStart{Base: events.Base{RunID: "child", ParentRunID: "root", Depth: 1,
			Index: 9, Time: t0.Add(90 * time.Millisecond)}, Model: "sub-model"},
		&events.LLMEnd{Base: events.Base{RunID: "child", ParentRunID: "root", Depth: 1,
			Index: 10, Time: t0.Add(100 * time.Millisecond)}, Model: "sub-model",
			Usage: core.Usage{Input: 10, Output: 5}, DurationMS: 15},
		&events.AgentEnd{Base: events.Base{RunID: "child", ParentRunID: "root", Depth: 1,
			Index: 11, Time: t0.Add(110 * time.Millisecond)}, FinishReason: "stop", DurationMS: 30},
		&events.AgentEnd{Base: b("root", 12), FinishReason: "stop", DurationMS: 120},
	}
}

func TestBuildProducesTree(t *testing.T) {
	res := Build(sampleEvents(), 50)
	if len(res.Traces) != 1 {
		t.Fatalf("trace 数 = %d", len(res.Traces))
	}
	tr := res.Traces[0]
	if tr.ID != "root" {
		t.Fatalf("root = %q", tr.ID)
	}

	counts := map[Kind]int{}
	var child *Span
	for i := range tr.Spans {
		counts[tr.Spans[i].Kind]++
		if tr.Spans[i].Kind == KindRun && tr.Spans[i].ID == "child" {
			child = &tr.Spans[i]
		}
	}
	if counts[KindRun] != 2 || counts[KindLLM] != 2 || counts[KindTool] != 1 || counts[KindCompress] != 1 {
		t.Fatalf("span 统计 = %+v", counts)
	}
	if child == nil || child.ParentID != "root" || child.Depth != 1 {
		t.Fatalf("子 run span 错误：%+v", child)
	}
}

func TestBuildCarriesMetadataOnly(t *testing.T) {
	res := Build(sampleEvents(), 50)
	raw, err := json.Marshal(res.Traces)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "工具正文不应该进投影") || strings.Contains(string(raw), "机密内容") {
		t.Fatalf("投影里出现了正文：%s", raw)
	}

	var llm, tool *Span
	for i := range res.Traces[0].Spans {
		switch res.Traces[0].Spans[i].Kind {
		case KindLLM:
			if llm == nil {
				llm = &res.Traces[0].Spans[i]
			}
		case KindTool:
			tool = &res.Traces[0].Spans[i]
		}
	}
	if llm == nil || llm.InTokens != 100 || llm.OutTokens != 20 || llm.CachedTokens != 60 || llm.CostMicro != 500 {
		t.Fatalf("llm 元数据 = %+v", llm)
	}
	if llm.DurMS != 40 {
		t.Fatalf("llm 时长应取服务端 duration_ms，得到 %d", llm.DurMS)
	}
	if tool == nil || !tool.IsError || tool.Status != "error" || tool.DurMS != 12 {
		t.Fatalf("tool 元数据 = %+v", tool)
	}
}

func TestDetailLookupByCallID(t *testing.T) {
	res := Build(sampleEvents(), 50)
	d, ok := res.Detail(KindTool, "call-1")
	if !ok {
		t.Fatal("按 call_id 回查失败")
	}
	end, ok := d.Payload.(*events.ToolRunEnd)
	if !ok || end.Content != "工具正文不应该进投影" {
		t.Fatalf("详情内容 = %+v", d.Payload)
	}

	if _, ok := res.Detail(KindTool, "不存在"); ok {
		t.Fatal("不存在的 id 不应回查到内容")
	}
}

func TestBuildSummary(t *testing.T) {
	res := Build(sampleEvents(), 50)
	if len(res.Summaries) != 1 {
		t.Fatalf("summary 数 = %d", len(res.Summaries))
	}
	s := res.Summaries[0]
	if s.Tools != 1 || s.Errors != 1 || s.In != 110 || s.Out != 25 {
		t.Fatalf("summary = %+v", s)
	}
	if s.Cost != 500 {
		t.Fatalf("summary 成本 = %d", s.Cost)
	}
}

func TestBuildLimitSetsHasMore(t *testing.T) {
	evs := append(sampleEvents(), &events.AgentStart{
		Base:  events.Base{RunID: "root-2", Index: 1, Time: time.Now()},
		Model: "other",
	}, &events.AgentEnd{
		Base:         events.Base{RunID: "root-2", Index: 2, Time: time.Now()},
		FinishReason: "stop",
	})
	res := Build(evs, 1)
	if len(res.Traces) != 1 || !res.HasMore {
		t.Fatalf("limit 生效失败：traces=%d hasMore=%v", len(res.Traces), res.HasMore)
	}
}

func TestRunningRunStatus(t *testing.T) {
	res := Build([]events.Event{
		&events.AgentStart{Base: events.Base{RunID: "r1", Time: time.Now()}, Model: "m"},
		&events.LLMStart{Base: events.Base{RunID: "r1", Time: time.Now()}, Model: "m"},
	}, 10)
	if len(res.Traces) != 1 || res.Traces[0].Spans[0].Status != "running" {
		t.Fatalf("未结束的运行应为 running：%+v", res.Traces)
	}
	if res.Traces[0].Spans[1].Status != "running" {
		t.Fatalf("未闭合的 llm span 应为 running：%+v", res.Traces[0].Spans[1])
	}
}

func TestRecorderKeepsBoundedHistory(t *testing.T) {
	rec := NewRecorder(3)
	for i := 0; i < 5; i++ {
		rec.Handle(context.Background(), &events.ContentChunk{
			Base: events.Base{RunID: "r", Index: i, Time: time.Now()}, Text: "x",
		})
	}
	if got := len(rec.Events()); got != 3 {
		t.Fatalf("记录器容量 = %d", got)
	}
	if res := rec.Build(10); res == nil {
		t.Fatal("Build 不应返回 nil")
	}
}
