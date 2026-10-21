package agents

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
	"github.com/tasohower/rei/tools"
)

// 上下文超限应当「压缩后重试一次」，而不是直接失败。
func TestContextExceededCompactsAndRetries(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		{err: &provider.Error{Kind: provider.KindContextExceeded, Message: "too long"}},
		textStep("压缩后成功"),
	}}
	loop := newLoop(t, prov, WithCompressor(alwaysCompress{}))

	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("长会话")},
	}, rec.handle)
	if err != nil {
		t.Fatalf("上下文超限后应恢复：%v", err)
	}
	if res.FinalText != "压缩后成功" || atomic.LoadInt32(&prov.calls) != 2 {
		t.Fatalf("calls=%d text=%q", prov.calls, res.FinalText)
	}
	forced := 0
	for _, e := range rec.find(events.CompressStartType) {
		if e.(*events.CompressStart).Reason == "context_exceeded" {
			forced++
		}
	}
	if forced != 1 {
		t.Fatalf("context_exceeded 压缩次数 = %d", forced)
	}
}

// 输出被 max_tokens 截断时续写，而不是把半截答案当最终结果。
func TestOutputTruncationContinues(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		{chunks: []provider.Chunk{
			{Kind: provider.ChunkText, Text: "前半段"},
			{Kind: provider.ChunkDone, FinishReason: "length"},
		}},
		textStep("后半段"),
	}}
	loop := newLoop(t, prov)

	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("写长文")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&prov.calls) != 2 {
		t.Fatalf("应续写一次，实际调用 %d 次", prov.calls)
	}
	if res.FinalText != "后半段" {
		t.Fatalf("最终正文 = %q", res.FinalText)
	}
	if !strings.Contains(prov.last.Messages[len(prov.last.Messages)-1].Text(), "cut off") {
		t.Fatal("续写请求应带上明确的续写指令")
	}
}

// 运行中插话：轮次边界把新输入并进上下文。
func TestSteeringMessagesAreInjected(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}),
		textStep("按新指令执行"),
	}}
	eng := tools.NewEngine()
	_ = eng.Register(&fakeTool{name: "read_file", out: "ok"})

	var served int32
	loop := newLoop(t, prov, WithToolEngine(eng))
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("开始")},
		Steering: func() []core.Message {
			if atomic.AddInt32(&served, 1) == 1 {
				return []core.Message{core.NewUserTextMessage("顺便改一下配置")}
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range res.Messages {
		if strings.Contains(m.Text(), "顺便改一下配置") {
			found = true
		}
	}
	if !found {
		t.Fatal("运行中插话没有被注入上下文")
	}
}

// 只读模式由内核强制：写工具直接拒绝，且不会被调用。
func TestReadOnlyModeBlocksMutatingTools(t *testing.T) {
	write := &serialTool{fakeTool{name: "write_file", out: "should not run"}}
	eng := tools.NewEngine()
	_ = eng.Register(write)

	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "w1", Name: "write_file", Arguments: `{"path":"a"}`}),
		textStep("被拒绝了"),
	}}
	loop := newLoop(t, prov, WithToolEngine(eng), WithReadOnlyMode())
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("写文件")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&write.calls) != 0 {
		t.Fatal("只读模式下写工具不应被调用")
	}
	if !strings.Contains(res.Messages[2].Text(), "read-only mode") {
		t.Fatalf("工具回执 = %q", res.Messages[2].Text())
	}
}

// 审批策略：只读工具放行，写工具才问人。
func TestApprovalPolicyOnlyAsksForWrites(t *testing.T) {
	read := &fakeTool{name: "read_file", out: "content"}
	write := &serialTool{fakeTool{name: "write_file", out: "written"}}
	eng := tools.NewEngine()
	_ = eng.Register(read)
	_ = eng.Register(write)

	var asked int32
	approver := PolicyDenyApprover{onAsk: func() { atomic.AddInt32(&asked, 1) }}
	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "r1", Name: "read_file", Arguments: `{}`}),
		toolStep(core.ToolCall{ID: "w1", Name: "write_file", Arguments: `{}`}),
		textStep("完成"),
	}}
	loop := newLoop(t, prov, WithToolEngine(eng), WithApprover(approver), WithApprovalPolicy(events.ReadOnlyPolicy()))
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("读写")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&asked) != 1 {
		t.Fatalf("只应为写工具询问一次，实际 %d 次", asked)
	}
	if atomic.LoadInt32(&read.calls) != 1 {
		t.Fatal("只读工具应直接执行")
	}
	if atomic.LoadInt32(&write.calls) != 0 {
		t.Fatal("被拒绝的写工具不应执行")
	}
	if !strings.Contains(res.Messages[4].Text(), "denied") {
		t.Fatalf("被拒回执 = %q", res.Messages[4].Text())
	}
}

// 重复调用护栏：连续同样的工具调用会触发一次目标对齐提醒与一句纠偏。
func TestRepeatedToolCallsAreNudged(t *testing.T) {
	call := core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a"}`}
	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(call), toolStep(call), toolStep(call), textStep("换个思路"),
	}}
	eng := tools.NewEngine()
	_ = eng.Register(&fakeTool{name: "read_file", out: "same"})

	rec := &recordingHandler{}
	loop := newLoop(t, prov, WithToolEngine(eng), WithMaxIterations(6))
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("读")},
	}, rec.handle)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.find(events.GoalAlignmentRemindType)) == 0 {
		t.Fatal("重复调用应触发目标对齐提醒")
	}
	found := false
	for _, m := range res.Messages {
		if strings.Contains(m.Text(), "repeating the same tool calls") {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少纠偏消息")
	}
}

// 逐轮检查点：每个 ReAct 轮次结束都会回调。
func TestCheckpointRunsEveryIteration(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}),
		textStep("完成"),
	}}
	eng := tools.NewEngine()
	_ = eng.Register(&fakeTool{name: "read_file", out: "ok"})

	var rounds int32
	loop := newLoop(t, prov, WithToolEngine(eng))
	_, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("读")},
		Checkpoint: func(_ context.Context, msgs []core.Message, _ core.Usage) {
			atomic.AddInt32(&rounds, 1)
			if len(msgs) == 0 {
				t.Error("检查点不应拿到空历史")
			}
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&rounds); got != 2 {
		t.Fatalf("检查点次数 = %d，期望 2", got)
	}
}

// 工具内部用量并入运行账本。
func TestToolUsageIsAggregated(t *testing.T) {
	usageTool := &usageTool{name: "summarize", usage: core.Usage{Input: 100, Output: 20, Cost: core.Cost{Input: 500}}}
	eng := tools.NewEngine()
	_ = eng.Register(usageTool)
	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "c1", Name: "summarize", Arguments: `{}`}),
		textStep("好了"),
	}}
	loop := newLoop(t, prov, WithToolEngine(eng))
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("总结")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 模型两次调用：12+3 与 10+5；工具 100+20。
	if res.Usage.Input != 122 || res.Usage.Output != 28 {
		t.Fatalf("聚合用量 = %+v", res.Usage)
	}
	if res.Usage.Cost.Total() != 500 {
		t.Fatalf("工具成本未计入：%d", res.Usage.Cost.Total())
	}
}

// ---------------------------------------------------------------- 测试夹具

func (r *recordingHandler) find(t events.EventType) []events.Event {
	var out []events.Event
	for _, e := range r.seen {
		if e.Type() == t {
			out = append(out, e)
		}
	}
	return out
}

// serialTool 是「写工具」：CanParallel=false，只读门禁会拦住它。
type serialTool struct{ fakeTool }

func (s *serialTool) CanParallel() bool { return false }

// PolicyDenyApprover 记录被询问次数并一律拒绝。
type PolicyDenyApprover struct{ onAsk func() }

func (p PolicyDenyApprover) Approve(context.Context, events.ApprovalRequest) (events.Decision, error) {
	if p.onAsk != nil {
		p.onAsk()
	}
	return events.Decision{Approved: false, Reason: "测试拒绝"}, nil
}

// usageTool 模拟「内部会烧 token」的工具（子 agent / 内部摘要）。
type usageTool struct {
	name  string
	usage core.Usage
}

func (u *usageTool) Name() string        { return u.name }
func (u *usageTool) Description() string { return "会消耗模型用量的测试工具" }
func (u *usageTool) CanParallel() bool   { return true }
func (u *usageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (u *usageTool) Call(context.Context, core.ToolCall) tools.Result {
	return tools.Result{Content: "summary", Usage: u.usage}
}

func TestZeroContinuationsDisablesRetry(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		{chunks: []provider.Chunk{
			{Kind: provider.ChunkText, Text: "半截"},
			{Kind: provider.ChunkDone, FinishReason: "length"},
		}},
	}}
	loop := newLoop(t, prov, WithMaxContinuations(0))
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("x")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&prov.calls) != 1 || res.FinalText != "半截" {
		t.Fatalf("calls=%d text=%q", prov.calls, res.FinalText)
	}
}
