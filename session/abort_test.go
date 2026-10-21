package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/tasohower/rei/agents"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
	"github.com/tasohower/rei/tools"
)

// blockingProvider 的流一直等到 ctx 被取消。
type blockingProvider struct{ calls int }

func (p *blockingProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	p.calls++
	return &blockingStream{ctx: ctx}, nil
}

type blockingStream struct{ ctx context.Context }

func (s *blockingStream) Recv() (provider.Chunk, error) {
	<-s.ctx.Done()
	return provider.Chunk{}, s.ctx.Err()
}

func (s *blockingStream) Close() error { return nil }

func waitRunning(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if running {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("等待运行开始超时")
}

func TestAbortStopsRunningRun(t *testing.T) {
	s := newSession(t, &blockingProvider{})
	if err := s.Ask(context.Background(), core.NewUserTextMessage("长任务")); err != nil {
		t.Fatal(err)
	}
	var finish string
	handler := func(_ context.Context, e events.Event) {
		if ev, ok := e.(*events.AgentEnd); ok {
			finish = ev.FinishReason
		}
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background(), handler) }()

	waitRunning(t, s)
	if err := s.Abort(); err != nil {
		t.Fatalf("Abort 失败：%v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrAborted) {
			t.Fatalf("Run 返回 %v，期望 ErrAborted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Abort 后运行没有结束")
	}
	if finish != core.FinishAbort {
		t.Fatalf("结束原因 = %q", finish)
	}
	if err := s.Abort(); !errors.Is(err, ErrNoRunning) {
		t.Fatalf("没有运行时的 Abort 应返回 ErrNoRunning，得到 %v", err)
	}
}

func TestShutdownCancelsInflightRun(t *testing.T) {
	s := newSession(t, &blockingProvider{})
	_ = s.Ask(context.Background(), core.NewUserTextMessage("长任务"))

	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background(), nil) }()
	waitRunning(t, s)

	if err := s.Shutdown(context.Background(), nil); err != nil {
		t.Fatalf("Shutdown 失败：%v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrAborted) {
			t.Fatalf("运行返回 %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown 没有等待运行收尾")
	}
}

// recordingStore 记录每次提交，用于验证逐轮检查点。
type recordingStore struct {
	mu    sync.Mutex
	saves []Snapshot
}

func (r *recordingStore) Save(_ context.Context, snap Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves = append(r.saves, snap)
	return nil
}

func (r *recordingStore) Load(context.Context, string) (Snapshot, error) {
	return Snapshot{}, nil
}

func TestPerRoundCheckpointPersistsIntermediateState(t *testing.T) {
	store := &recordingStore{}
	eng := tools.NewEngine()
	if err := eng.Register(&captureTool{name: "read_file"}); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{steps: []scriptStep{
		{chunks: []provider.Chunk{
			{Kind: provider.ChunkToolCall, ToolCall: &core.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}},
			{Kind: provider.ChunkDone, FinishReason: "tool_calls"},
		}},
		textStep("完成"),
	}}
	loop, err := agents.NewAgentLoop(prov, agents.WithModel("test"), agents.WithToolEngine(eng), agents.WithBackoff(provider.NoJitter()))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New("s1", loop, WithStore(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ask(context.Background(), core.NewUserTextMessage("读文件")); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background(), nil); err != nil {
		t.Fatalf("运行失败：%v", err)
	}

	store.mu.Lock()
	saves := append([]Snapshot(nil), store.saves...)
	store.mu.Unlock()
	if len(saves) < 2 {
		t.Fatalf("提交次数 = %d，期望至少 2（逐轮 + 收尾）", len(saves))
	}
	if len(saves[0].Messages) >= len(saves[len(saves)-1].Messages) {
		t.Fatalf("中间检查点应立即少于最终快照：%d vs %d", len(saves[0].Messages), len(saves[len(saves)-1].Messages))
	}
}

type captureTool struct {
	name string
	mu   sync.Mutex
	args []string
}

func (c *captureTool) Name() string        { return c.name }
func (c *captureTool) Description() string { return "记录参数的测试工具" }
func (c *captureTool) CanParallel() bool   { return false }
func (c *captureTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (c *captureTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	c.mu.Lock()
	c.args = append(c.args, call.Arguments)
	c.mu.Unlock()
	return tools.Result{Content: "ok"}
}

func TestResolveApprovalWithArgs(t *testing.T) {
	eng := tools.NewEngine()
	tool := &captureTool{name: "bash"}
	if err := eng.Register(tool); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{steps: []scriptStep{
		{chunks: []provider.Chunk{
			{Kind: provider.ChunkToolCall, ToolCall: &core.ToolCall{ID: "c1", Name: "bash", Arguments: `{"command":"rm -rf /"}`}},
			{Kind: provider.ChunkDone, FinishReason: "tool_calls"},
		}},
		textStep("执行完毕"),
	}}
	loop, err := agents.NewAgentLoop(prov, agents.WithModel("test"), agents.WithToolEngine(eng), agents.WithBackoff(provider.NoJitter()))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New("s1", loop, WithHumanApproval())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Ask(context.Background(), core.NewUserTextMessage("清理"))

	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background(), nil) }()
	callID := waitForApproval(t, s)

	fixed := `{"command":"echo safe"}`
	if err := s.ResolveApprovalWithArgs(callID, true, "改成安全命令", &fixed); err != nil {
		t.Fatalf("带参数审批失败：%v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("运行失败：%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("审批后运行没有结束")
	}

	tool.mu.Lock()
	defer tool.mu.Unlock()
	if len(tool.args) != 1 || tool.args[0] != fixed {
		t.Fatalf("工具收到的参数 = %v，期望被审批改写", tool.args)
	}
}

func TestAbortWithoutRunIsSafe(t *testing.T) {
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("x")}})
	if err := s.Abort(); !errors.Is(err, ErrNoRunning) {
		t.Fatalf("空闲时 Abort 应返回 ErrNoRunning，得到 %v", err)
	}
}

var _ = io.EOF
