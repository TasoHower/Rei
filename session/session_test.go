package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tasohower/rei/agents"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
	"github.com/tasohower/rei/tools"
)

// ---------------------------------------------------------------- 夹具

type scriptStream struct {
	chunks []provider.Chunk
	i      int
}

func (s *scriptStream) Recv() (provider.Chunk, error) {
	if s.i >= len(s.chunks) {
		return provider.Chunk{}, io.EOF
	}
	ch := s.chunks[s.i]
	s.i++
	return ch, nil
}

func (s *scriptStream) Close() error { return nil }

type scriptedProvider struct {
	steps []scriptStep
	calls int32
}

type scriptStep struct{ chunks []provider.Chunk }

func (p *scriptedProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	n := int(atomic.AddInt32(&p.calls, 1)) - 1
	if n >= len(p.steps) {
		n = len(p.steps) - 1
	}
	return &scriptStream{chunks: p.steps[n].chunks}, nil
}

func textStep(text string) scriptStep {
	return scriptStep{chunks: []provider.Chunk{
		{Kind: provider.ChunkText, Text: text},
		{Kind: provider.ChunkDone, FinishReason: "stop", Usage: &core.Usage{Input: 8, Output: 4}},
	}}
}

type countingTool struct {
	name  string
	calls int32
}

func (c *countingTool) Name() string        { return c.name }
func (c *countingTool) Description() string { return "测试工具" }
func (c *countingTool) CanParallel() bool   { return true }
func (c *countingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (c *countingTool) Call(context.Context, core.ToolCall) tools.Result {
	atomic.AddInt32(&c.calls, 1)
	return tools.Result{Content: "已执行"}
}

func newSession(t *testing.T, p provider.Provider, opts ...Option) *Session {
	t.Helper()
	loop, err := agents.NewAgentLoop(p, agents.WithModel("test"), agents.WithBackoff(provider.NoJitter()))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New("s1", loop, opts...)
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	return s
}

// ---------------------------------------------------------------- 用例

func TestAskQueueBackpressure(t *testing.T) {
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("ok")}}, WithMaxQueue(1))
	ctx := context.Background()

	if err := s.Ask(ctx, core.NewUserTextMessage("第一条")); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if err := s.Ask(ctx, core.NewUserTextMessage("第二条")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("队列满时应返回 ErrQueueFull，得到 %v", err)
	}
}

func TestRunConsumesQueueAndEmitsUsage(t *testing.T) {
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("你好")}})
	ctx := context.Background()

	if err := s.Ask(ctx, core.NewUserTextMessage("在吗")); err != nil {
		t.Fatal(err)
	}
	var consumed int
	handler := func(_ context.Context, e events.Event) {
		if e.Type() == events.UserInputsConsumedType {
			consumed = e.(*events.UserInputsConsumed).Count
		}
	}
	if err := s.Run(ctx, handler); err != nil {
		t.Fatalf("运行失败：%v", err)
	}
	if consumed != 1 {
		t.Fatalf("消费条数 = %d", consumed)
	}
	if hist := s.History(); len(hist) != 2 {
		t.Fatalf("历史条数 = %d", len(hist))
	}
	if u := s.Usage(); u.Input != 8 || u.Output != 4 {
		t.Fatalf("用量 = %+v", u)
	}

	// 队列空了：Run 是 no-op，不报错也不产生新回合。
	if err := s.Run(ctx, nil); err != nil {
		t.Fatalf("空队列 Run 应返回 nil：%v", err)
	}
}

func TestRunRestoresHistoryFromStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	first := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("记住我")}}, WithStore(store))
	if err := first.Ask(ctx, core.NewUserTextMessage("我叫小明")); err != nil {
		t.Fatal(err)
	}
	if err := first.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}

	second, err := New("s1", mustLoop(t, &scriptedProvider{steps: []scriptStep{textStep("继续")}}), WithStore(store))
	if err != nil {
		t.Fatalf("恢复会话失败：%v", err)
	}
	if hist := second.History(); len(hist) != 2 || hist[0].Text() != "我叫小明" {
		t.Fatalf("历史未恢复：%+v", hist)
	}
}

func mustLoop(t *testing.T, p provider.Provider) *agents.AgentLoop {
	t.Helper()
	loop, err := agents.NewAgentLoop(p, agents.WithModel("test"), agents.WithBackoff(provider.NoJitter()))
	if err != nil {
		t.Fatal(err)
	}
	return loop
}

func TestApprovalFlow(t *testing.T) {
	tool := &countingTool{name: "bash"}
	eng := tools.NewEngine()
	if err := eng.Register(tool); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{steps: []scriptStep{
		{chunks: []provider.Chunk{
			{Kind: provider.ChunkToolCall, ToolCall: &core.ToolCall{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`}},
			{Kind: provider.ChunkDone, FinishReason: "tool_calls"},
		}},
		textStep("执行完了"),
	}}
	loop, err := agents.NewAgentLoop(prov, agents.WithModel("test"), agents.WithBackoff(provider.NoJitter()), agents.WithToolEngine(eng))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New("s1", loop)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ask(context.Background(), core.NewUserTextMessage("列一下目录")); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background(), nil) }()

	callID := waitForApproval(t, s)
	if callID != "c1" {
		t.Fatalf("待审批 call id = %q", callID)
	}
	if err := s.ResolveApproval(callID, true, "允许"); err != nil {
		t.Fatalf("审批失败：%v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("运行失败：%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("运行没有在超时内结束")
	}
	if atomic.LoadInt32(&tool.calls) != 1 {
		t.Fatalf("工具执行次数 = %d", tool.calls)
	}
}

func waitForApproval(t *testing.T, s *Session) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ids := s.PendingApprovals(); len(ids) > 0 {
			return ids[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待审批超时")
	return ""
}

func TestApprovalOnlyHeadCanBeResolved(t *testing.T) {
	q := newApprovalQueue(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_, _ = q.Approve(ctx, events.ApprovalRequest{CallID: "c1"})
	}()
	waitPending(t, q, 1)

	go func() {
		_, _ = q.Approve(ctx, events.ApprovalRequest{CallID: "c2"})
	}()
	waitPending(t, q, 2)

	if ids := q.Pending(); len(ids) != 2 || ids[0] != "c1" || ids[1] != "c2" {
		t.Fatalf("待审批项 = %v", ids)
	}

	if err := q.Resolve("c2", events.Decision{Approved: true}); !errors.Is(err, ErrApprovalNotHead) {
		t.Fatalf("非队首审批应被拒绝，得到 %v", err)
	}
	if err := q.Resolve("c9", events.Decision{Approved: true}); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("未知 call id 应报 NotFound，得到 %v", err)
	}
	if err := q.Resolve("c1", events.Decision{Approved: true}); err != nil {
		t.Fatalf("队首审批失败：%v", err)
	}
	if err := q.Resolve("c2", events.Decision{Approved: false}); err != nil {
		t.Fatalf("第二个审批失败：%v", err)
	}
}

func waitPending(t *testing.T, q *approvalQueue, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(q.Pending()) >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待 %d 个待审批项超时，当前 %v", n, q.Pending())
}

func TestApprovalTimeoutIsDenial(t *testing.T) {
	q := newApprovalQueue(20 * time.Millisecond)
	decision, err := q.Approve(context.Background(), events.ApprovalRequest{CallID: "c1"})
	if !errors.Is(err, ErrApprovalTimeout) {
		t.Fatalf("期望超时错误，得到 %v", err)
	}
	if decision.Approved {
		t.Fatal("超时应视为拒绝")
	}
	if len(q.Pending()) != 0 {
		t.Fatal("超时后应从队列移除")
	}
}

func TestShutdownRejectsNewWork(t *testing.T) {
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("ok")}})
	ctx := context.Background()
	if err := s.Shutdown(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Ask(ctx, core.NewUserTextMessage("还在吗")); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后 Ask 应返回 ErrClosed，得到 %v", err)
	}
	if err := s.Run(ctx, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后 Run 应返回 ErrClosed，得到 %v", err)
	}
}

type failingStore struct{}

func (failingStore) Save(context.Context, Snapshot) error { return errors.New("磁盘满了") }
func (failingStore) Load(context.Context, string) (Snapshot, error) {
	return Snapshot{}, nil
}

func TestPersistErrorIsNotSilent(t *testing.T) {
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("ok")}}, WithStore(failingStore{}))
	if err := s.Ask(context.Background(), core.NewUserTextMessage("hi")); err != nil {
		t.Fatal(err)
	}
	var persistErr bool
	handler := func(_ context.Context, e events.Event) {
		if e.Type() == events.SessionPersistErrorType {
			persistErr = true
		}
	}
	if err := s.Run(context.Background(), handler); err != nil {
		t.Fatalf("运行失败：%v", err)
	}
	if !persistErr {
		t.Fatal("持久化失败必须发事件，不能静默")
	}
}

type corruptStore struct{}

func (corruptStore) Save(context.Context, Snapshot) error { return nil }
func (corruptStore) Load(context.Context, string) (Snapshot, error) {
	return Snapshot{SessionID: "s1", Messages: []core.Message{{Role: "robot"}}}, nil
}

func TestRestoreRejectsCorruptHistory(t *testing.T) {
	_, err := New("s1", mustLoop(t, &scriptedProvider{steps: []scriptStep{textStep("x")}}), WithStore(corruptStore{}))
	if err == nil {
		t.Fatal("非法历史应当让会话启动失败")
	}
}
