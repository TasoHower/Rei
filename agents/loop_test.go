package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tasohower/rei/compress"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
	"github.com/tasohower/rei/tools"
)

// ---------------------------------------------------------------- 测试夹具

type scriptStep struct {
	chunks []provider.Chunk
	err    error
	delay  time.Duration
}

type scriptedProvider struct {
	steps []scriptStep
	calls int32
	last  provider.Request
}

func (s *scriptedProvider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	n := int(atomic.AddInt32(&s.calls, 1))
	s.last = req
	idx := n - 1
	if idx >= len(s.steps) {
		idx = len(s.steps) - 1
	}
	step := s.steps[idx]
	if step.err != nil {
		return nil, step.err
	}
	return &scriptStream{ctx: ctx, chunks: step.chunks, delay: step.delay}, nil
}

type scriptStream struct {
	ctx    context.Context
	chunks []provider.Chunk
	delay  time.Duration
	i      int
}

func (s *scriptStream) Recv() (provider.Chunk, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-s.ctx.Done():
			return provider.Chunk{}, s.ctx.Err()
		}
	}
	if s.i >= len(s.chunks) {
		return provider.Chunk{}, io.EOF
	}
	ch := s.chunks[s.i]
	s.i++
	return ch, nil
}

func (s *scriptStream) Close() error { return nil }

func textStep(text string) scriptStep {
	return scriptStep{chunks: []provider.Chunk{
		{Kind: provider.ChunkText, Text: text},
		{Kind: provider.ChunkDone, FinishReason: "stop", Usage: &core.Usage{Input: 10, Output: 5}},
	}}
}

func toolStep(call core.ToolCall) scriptStep {
	return scriptStep{chunks: []provider.Chunk{
		{Kind: provider.ChunkToolCall, ToolCall: &call},
		{Kind: provider.ChunkDone, FinishReason: "tool_calls", Usage: &core.Usage{Input: 12, Output: 3}},
	}}
}

// fakeTool 记录调用次数并返回固定内容。
type fakeTool struct {
	name  string
	calls int32
	out   string
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "测试用工具" }
func (f *fakeTool) CanParallel() bool   { return true }
func (f *fakeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (f *fakeTool) Call(context.Context, core.ToolCall) tools.Result {
	atomic.AddInt32(&f.calls, 1)
	return tools.Result{Content: f.out}
}

type denyApprover struct{}

func (denyApprover) Approve(context.Context, events.ApprovalRequest) (events.Decision, error) {
	return events.Decision{Approved: false, Reason: "测试拒绝"}, nil
}

type recordingHandler struct{ seen []events.Event }

func (r *recordingHandler) handle(_ context.Context, e events.Event) { r.seen = append(r.seen, e) }

func (r *recordingHandler) has(t events.EventType) bool {
	for _, e := range r.seen {
		if e.Type() == t {
			return true
		}
	}
	return false
}

func (r *recordingHandler) count(t events.EventType) int {
	n := 0
	for _, e := range r.seen {
		if e.Type() == t {
			n++
		}
	}
	return n
}

func newLoop(t *testing.T, p provider.Provider, opts ...Option) *AgentLoop {
	t.Helper()
	base := []Option{WithModel("test"), WithBackoff(provider.NoJitter())}
	loop, err := NewAgentLoop(p, append(base, opts...)...)
	if err != nil {
		t.Fatalf("构造引擎失败：%v", err)
	}
	return loop
}

// ---------------------------------------------------------------- 用例

func TestRunExecutesToolThenAnswers(t *testing.T) {
	fileTool := &fakeTool{name: "read_file", out: "文件内容"}
	eng := tools.NewEngine()
	if err := eng.Register(fileTool); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a.txt"}`}),
		textStep("看到内容了"),
	}}
	loop := newLoop(t, prov, WithToolEngine(eng))

	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("读一下 a.txt")},
	}, rec.handle)
	if err != nil {
		t.Fatalf("运行失败：%v", err)
	}

	if res.FinishReason != core.FinishStop || res.FinalText != "看到内容了" {
		t.Fatalf("结果 = %+v", res)
	}
	if atomic.LoadInt32(&fileTool.calls) != 1 {
		t.Fatalf("工具调用次数 = %d", fileTool.calls)
	}
	if err := core.ValidateConversation(res.Messages); err != nil {
		t.Fatalf("产出对话非法：%v", err)
	}
	if len(res.Messages) != 4 {
		t.Fatalf("消息数 = %d，期望 4（user/assistant/tool/assistant）", len(res.Messages))
	}
	if res.Usage.Input != 22 || res.Usage.Output != 8 {
		t.Fatalf("用量未累加：%+v", res.Usage)
	}

	for _, want := range []events.EventType{
		events.AgentStartType, events.LLMStartType, events.LLMEndType,
		events.ToolRunStartType, events.ToolRunEndType, events.AgentEndType,
	} {
		if !rec.has(want) {
			t.Fatalf("缺少事件 %s", want)
		}
	}
	if len(rec.seen) == 0 || rec.seen[len(rec.seen)-1].Type() != events.AgentEndType {
		t.Fatal("agent_end 必须是最后一个事件")
	}
}

func TestApprovalDeniedSkipsExecution(t *testing.T) {
	fileTool := &fakeTool{name: "bash", out: "不该被调用"}
	eng := tools.NewEngine()
	if err := eng.Register(fileTool); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{steps: []scriptStep{
		toolStep(core.ToolCall{ID: "c1", Name: "bash", Arguments: `{"command":"rm -rf /"}`}),
		textStep("好的，不执行"),
	}}
	loop := newLoop(t, prov, WithToolEngine(eng), WithApprover(denyApprover{}))

	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{
		Messages: []core.Message{core.NewUserTextMessage("删库")},
	}, rec.handle)
	if err != nil {
		t.Fatalf("运行失败：%v", err)
	}
	if atomic.LoadInt32(&fileTool.calls) != 0 {
		t.Fatal("被拒绝的工具不应执行")
	}
	toolMsg := res.Messages[2]
	if !strings.Contains(toolMsg.Text(), "User denied") {
		t.Fatalf("工具回执应说明被拒绝：%q", toolMsg.Text())
	}
	if !rec.has(events.ToolApprovalType) || !rec.has(events.ToolApprovalDoneType) {
		t.Fatal("缺少审批事件")
	}
}

func TestRetryOnRateLimit(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		{err: &provider.Error{Kind: provider.KindRateLimit, Message: "429"}},
		textStep("重试成功"),
	}}
	loop := newLoop(t, prov)

	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{core.NewUserTextMessage("hi")}}, rec.handle)
	if err != nil {
		t.Fatalf("可重试错误应当恢复：%v", err)
	}
	if res.FinalText != "重试成功" || atomic.LoadInt32(&prov.calls) != 2 {
		t.Fatalf("调用次数 = %d，正文 = %q", prov.calls, res.FinalText)
	}
	if rec.count(events.LLMErrorType) != 1 {
		t.Fatalf("llm_error 次数 = %d", rec.count(events.LLMErrorType))
	}
}

func TestNonRetryableErrorStopsRun(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{
		{err: &provider.Error{Kind: provider.KindPermanent, Message: "401 无权限"}},
	}}
	loop := newLoop(t, prov)

	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{core.NewUserTextMessage("hi")}}, rec.handle)
	if err == nil {
		t.Fatal("不可重试错误应当返回 error")
	}
	if res.FinishReason != core.FinishError {
		t.Fatalf("结束原因 = %q", res.FinishReason)
	}
	if atomic.LoadInt32(&prov.calls) != 1 {
		t.Fatalf("不应重试，实际调用 %d 次", prov.calls)
	}
}

func TestMaxIterationsGuard(t *testing.T) {
	call := core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}
	prov := &scriptedProvider{steps: []scriptStep{toolStep(call)}}
	eng := tools.NewEngine()
	_ = eng.Register(&fakeTool{name: "read_file", out: "ok"})

	loop := newLoop(t, prov, WithToolEngine(eng), WithMaxIterations(2))
	res, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{core.NewUserTextMessage("loop")}}, nil)
	if err != nil {
		t.Fatalf("运行失败：%v", err)
	}
	if res.FinishReason != core.FinishMaxIterations {
		t.Fatalf("结束原因 = %q，期望 %q", res.FinishReason, core.FinishMaxIterations)
	}
	if res.Iterations != 2 {
		t.Fatalf("轮次 = %d", res.Iterations)
	}
}

func TestBudgetStopsBeforeNextCall(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{{
		chunks: []provider.Chunk{
			{Kind: provider.ChunkToolCall, ToolCall: &core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}},
			{Kind: provider.ChunkDone, FinishReason: "tool_calls", Usage: &core.Usage{Input: 1_000_000, Output: 1_000_000}},
		},
	}}}
	eng := tools.NewEngine()
	_ = eng.Register(&fakeTool{name: "read_file", out: "ok"})

	loop := newLoop(t, prov, WithToolEngine(eng), WithPricing(core.Pricing{Input: 1_000_000, Output: 1_000_000}), WithMaxCost(1))
	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{core.NewUserTextMessage("hi")}}, rec.handle)
	if err != nil {
		t.Fatalf("预算耗尽不应返回 error：%v", err)
	}
	if res.FinishReason != core.FinishMaxCost {
		t.Fatalf("结束原因 = %q", res.FinishReason)
	}
	if atomic.LoadInt32(&prov.calls) != 1 {
		t.Fatalf("预算耗尽后不应再调用模型，实际 %d 次", prov.calls)
	}
	if !rec.has(events.SessionBudgetExceededType) {
		t.Fatal("缺少预算事件")
	}
}

func TestFirstChunkTimeoutIsRetryable(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{{delay: 200 * time.Millisecond, chunks: []provider.Chunk{{Kind: provider.ChunkDone}}}}}
	loop := newLoop(t, prov, WithFirstChunkTimeout(10*time.Millisecond), WithBackoff(provider.Backoff{MaxRetries: 0}))

	rec := &recordingHandler{}
	_, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{core.NewUserTextMessage("hi")}}, rec.handle)
	if err == nil {
		t.Fatal("首字节超时应当报错")
	}
	var pe *provider.Error
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "首字节超时") {
		t.Fatalf("错误 = %v", err)
	}
}

func TestCompressionRunsBeforeModelCall(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{textStep("完成")}}
	loop := newLoop(t, prov, WithCompressor(alwaysCompress{}))

	rec := &recordingHandler{}
	res, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{
		core.NewUserTextMessage("一"), core.NewAssistantMessage([]core.Content{{Type: core.ContentTypeText, Content: "二"}}, "", nil),
	}}, rec.handle)
	if err != nil {
		t.Fatalf("运行失败：%v", err)
	}
	if !rec.has(events.CompressStartType) || !rec.has(events.CompressEndType) {
		t.Fatal("缺少压缩事件")
	}
	if res.Messages[0].Content[0].Type != core.ContentTypeSummary {
		t.Fatalf("首条应为摘要：%+v", res.Messages[0])
	}
}

func TestSystemPromptAndToolsArePassedToProvider(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{textStep("ok")}}
	eng := tools.NewEngine()
	_ = eng.Register(&fakeTool{name: "read_file", out: "x"})

	loop := newLoop(t, prov, WithToolEngine(eng), WithSystemPromptExtra("产品层补充"))
	if _, err := loop.RunStream(context.Background(), RunRequest{Messages: []core.Message{core.NewUserTextMessage("hi")}}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prov.last.System, "driven by the Rei runtime") || !strings.Contains(prov.last.System, "产品层补充") {
		t.Fatalf("系统提示未分层拼接：%q", prov.last.System)
	}
	if len(prov.last.Tools) != 1 || prov.last.Tools[0].Name != "read_file" {
		t.Fatalf("工具声明未传给 provider：%+v", prov.last.Tools)
	}
}

func TestCanceledContextEndsRun(t *testing.T) {
	prov := &scriptedProvider{steps: []scriptStep{textStep("不该到这一步")}}
	loop := newLoop(t, prov)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := loop.RunStream(ctx, RunRequest{Messages: []core.Message{core.NewUserTextMessage("hi")}}, nil)
	if err == nil {
		t.Fatal("已取消的 ctx 应当返回错误")
	}
	if res.FinishReason != core.FinishAbort {
		t.Fatalf("结束原因 = %q", res.FinishReason)
	}
}

func TestNewAgentLoopValidation(t *testing.T) {
	if _, err := NewAgentLoop(nil); err == nil {
		t.Fatal("缺少 provider 应当报错")
	}
	if _, err := NewAgentLoop(&scriptedProvider{}); err == nil {
		t.Fatal("缺少模型名应当报错")
	}
}

// alwaysCompress 总是触发压缩，并把上下文折叠成一条摘要。
type alwaysCompress struct{}

func (alwaysCompress) Decide([]core.Message, core.Usage) (bool, string) { return true, "测试" }

func (alwaysCompress) Compress(_ context.Context, msgs []core.Message) (compress.Result, error) {
	return compress.Result{
		Messages: []core.Message{core.NewSummaryMessage("【历史摘要】测试")},
		Usage:    core.Usage{Input: 7, Output: 2},
	}, nil
}
