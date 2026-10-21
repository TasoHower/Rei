package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

type finishStream struct {
	ctx   context.Context
	text  string
	usage core.Usage
	step  int
}

func (s *finishStream) Recv() (provider.Chunk, error) {
	switch s.step {
	case 0:
		s.step++
		return provider.Chunk{Kind: provider.ChunkText, Text: s.text}, nil
	case 1:
		s.step++
		return provider.Chunk{Kind: provider.ChunkDone, FinishReason: "stop", Usage: &s.usage}, nil
	default:
		return provider.Chunk{}, io.EOF
	}
}

func (s *finishStream) Close() error { return nil }

type scriptProvider struct {
	text string
	// block 为 true 时，流会一直等到 ctx 取消（用于测中断）。
	block bool
	calls int32
}

func (p *scriptProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	atomic.AddInt32(&p.calls, 1)
	if p.block {
		return &waitStream{ctx: ctx}, nil
	}
	return &finishStream{ctx: ctx, text: p.text, usage: core.Usage{Input: 5, Output: 2}}, nil
}

// waitStream 在 ctx 取消前一直不返回内容。
type waitStream struct{ ctx context.Context }

func (s *waitStream) Recv() (provider.Chunk, error) {
	<-s.ctx.Done()
	return provider.Chunk{}, s.ctx.Err()
}

func (s *waitStream) Close() error { return nil }

func newLoop(t *testing.T, p provider.Provider) *agents.AgentLoop {
	t.Helper()
	loop, err := agents.NewAgentLoop(p, agents.WithModel("test"), agents.WithBackoff(provider.NoJitter()))
	if err != nil {
		t.Fatal(err)
	}
	return loop
}

type recorder struct{ seen []events.Event }

func (r *recorder) handle(_ context.Context, e events.Event) { r.seen = append(r.seen, e) }

func (r *recorder) find(t events.EventType) []events.Event {
	var out []events.Event
	for _, e := range r.seen {
		if e.Type() == t {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------- 同步子 agent

func TestExploreEmitsChildRunWithParent(t *testing.T) {
	explore, err := NewExploreTool(newLoop(t, &scriptProvider{text: "调研结论"}))
	if err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	ctx := events.WithRunInfo(context.Background(), events.RunInfo{RunID: "parent-run", Depth: 0, Emit: rec.handle})
	res := explore.Call(ctx, core.ToolCall{ID: "c1", Name: "explore", Arguments: `{"task":"看看仓库结构"}`})
	if res.IsError {
		t.Fatalf("子 agent 失败：%s", res.Content)
	}
	if res.Content != "调研结论" {
		t.Fatalf("结果 = %q", res.Content)
	}

	starts := rec.find(events.AgentStartType)
	if len(starts) != 1 {
		t.Fatalf("子运行 agent_start 数 = %d", len(starts))
	}
	start := starts[0].(*events.AgentStart)
	if start.ParentRunID != "parent-run" || start.Depth != 1 {
		t.Fatalf("子运行事件树字段错误：parent=%q depth=%d", start.ParentRunID, start.Depth)
	}
	if start.RunID == "" || start.RunID == "parent-run" {
		t.Fatalf("子运行必须有独立 run_id，得到 %q", start.RunID)
	}
}

func TestExploreRejectsTooDeep(t *testing.T) {
	explore, err := NewExploreTool(newLoop(t, &scriptProvider{text: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.WithRunInfo(context.Background(), events.RunInfo{RunID: "r", Depth: DefaultMaxDepth})
	res := explore.Call(ctx, core.ToolCall{Arguments: `{"task":"再来一层"}`})
	if !res.IsError || !strings.Contains(res.Content, "depth limit") {
		t.Fatalf("超过深度应被拒绝：%+v", res)
	}
}

func TestExploreRequiresTask(t *testing.T) {
	explore, _ := NewExploreTool(newLoop(t, &scriptProvider{text: "x"}))
	res := explore.Call(context.Background(), core.ToolCall{Arguments: `{}`})
	if !res.IsError || !strings.Contains(res.Content, "task is required") {
		t.Fatalf("缺少 task 应报错：%+v", res)
	}
}

func TestReadOnlyEngineKeepsWhitelist(t *testing.T) {
	src := tools.NewEngine()
	for _, name := range []string{"read_file", "grep", "glob", "write_file", "bash"} {
		if err := src.Register(fakeTool{name: name}); err != nil {
			t.Fatal(err)
		}
	}
	ro := ReadOnlyEngine(src)
	got := strings.Join(ro.SortedNames(), ",")
	if got != "glob,grep,read_file" {
		t.Fatalf("只读子集 = %s", got)
	}
	if len(ReadOnlyEngine(src, "bash").SortedNames()) != 4 {
		t.Fatal("显式追加的白名单工具应当保留")
	}
}

type fakeTool struct{ name string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "test tool" }
func (f fakeTool) CanParallel() bool   { return true }
func (f fakeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (f fakeTool) Call(context.Context, core.ToolCall) tools.Result {
	return tools.Result{Content: "ok"}
}

// ---------------------------------------------------------------- 后台任务

func TestRegistrySpawnsJournalsAndDelivers(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(WithJournalDir(dir))
	defer reg.Close()

	rec := &recorder{}
	var delivered atomic.Int32
	reg.OnDone(func(events.TaskEnd) { delivered.Add(1) })

	snap, err := reg.Spawn(context.Background(), SpawnRequest{
		Name:        "调研",
		Prompt:      "数一数 Go 文件",
		Loop:        newLoop(t, &scriptProvider{text: "17 个 Go 文件"}),
		Handler:     rec.handle,
		ParentRunID: "parent-run",
	})
	if err != nil {
		t.Fatalf("spawn 失败：%v", err)
	}
	if snap.Status != events.TaskRunning {
		t.Fatalf("初始状态 = %q", snap.Status)
	}
	reg.Wait(snap.ID)

	got, err := reg.Snapshot(snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != events.TaskCompleted || got.Result != "17 个 Go 文件" {
		t.Fatalf("终态快照 = %+v", got)
	}
	if got.Usage.Input == 0 {
		t.Fatal("任务用量未累计")
	}

	starts := rec.find(events.TaskStartedType)
	if len(starts) != 1 || starts[0].(*events.TaskStarted).ParentRunID != "parent-run" {
		t.Fatalf("task_started 事件树字段错误：%+v", starts)
	}
	ends := rec.find(events.TaskEndType)
	if len(ends) != 1 || ends[0].(*events.TaskEnd).Status != events.TaskCompleted {
		t.Fatalf("task_end = %+v", ends)
	}
	if delivered.Load() != 1 {
		t.Fatalf("OnDone 触发 %d 次", delivered.Load())
	}

	// journal 必须落盘，并且 start/end 都在。
	path := filepath.Join(dir, snap.ID+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("journal 未落盘：%v", err)
	}
	records, err := reg.Records(snap.ID, "all")
	if err != nil || len(records) < 2 {
		t.Fatalf("journal 记录 = %d, err = %v", len(records), err)
	}
	if records[0].Type != RecordStart || records[len(records)-1].Type != RecordEnd {
		t.Fatalf("journal 首尾记录 = %q / %q", records[0].Type, records[len(records)-1].Type)
	}
}

func TestRegistryDepthLimit(t *testing.T) {
	reg := NewRegistry(WithMaxDepth(1))
	defer reg.Close()

	_, err := reg.Spawn(context.Background(), SpawnRequest{
		Prompt: "x", Loop: newLoop(t, &scriptProvider{text: "x"}), Depth: 1,
	})
	if !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("超深 spawn 应报错，得到 %v", err)
	}
}

func TestRegistryInterrupt(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()

	snap, err := reg.Spawn(context.Background(), SpawnRequest{
		Prompt: "长任务", Loop: newLoop(t, &scriptProvider{block: true}),
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := reg.Interrupt(snap.ID); err != nil {
		t.Fatalf("中断失败：%v", err)
	}
	reg.Wait(snap.ID)

	got, _ := reg.Snapshot(snap.ID)
	if got.Status != events.TaskInterrupted {
		t.Fatalf("中断后状态 = %q", got.Status)
	}
	if err := reg.Send(snap.ID, "还在吗"); !errors.Is(err, ErrTaskFinished) {
		t.Fatalf("已结束任务 Send 应报错，得到 %v", err)
	}
}

func TestRegistryResumeKeepsSameTask(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(WithJournalDir(dir))
	defer reg.Close()

	prov := &scriptProvider{text: "继续工作的结果"}
	snap, err := reg.Spawn(context.Background(), SpawnRequest{
		Prompt: "第一步", Loop: newLoop(t, prov),
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.Wait(snap.ID)
	if atomic.LoadInt32(&prov.calls) != 1 {
		t.Fatalf("首次运行调用数 = %d", prov.calls)
	}

	resumed, err := reg.Resume(context.Background(), snap.ID, "接着做")
	if err != nil {
		t.Fatalf("resume 失败：%v", err)
	}
	if resumed.ID != snap.ID || resumed.Status != events.TaskRunning {
		t.Fatalf("resume 快照 = %+v", resumed)
	}
	reg.Wait(snap.ID)

	got, _ := reg.Snapshot(snap.ID)
	if got.Status != events.TaskCompleted {
		t.Fatalf("resume 后状态 = %q", got.Status)
	}
	if atomic.LoadInt32(&prov.calls) != 2 {
		t.Fatalf("resume 应复用同一任务再跑一轮，调用数 = %d", prov.calls)
	}

	records, err := reg.Records(snap.ID, "all")
	if err != nil || len(records) < 3 {
		t.Fatalf("journal 记录 = %d, err = %v", len(records), err)
	}
	hasMessage := false
	for _, rec := range records {
		if rec.Type == RecordMessage {
			hasMessage = true
		}
	}
	if !hasMessage {
		t.Fatal("resume 指令应写入 journal")
	}
}

func TestRegistryResumeRejectsRunningTask(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()

	snap, err := reg.Spawn(context.Background(), SpawnRequest{
		Prompt: "长任务", Loop: newLoop(t, &scriptProvider{block: true}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Resume(context.Background(), snap.ID, "再来"); !errors.Is(err, ErrTaskRunning) {
		t.Fatalf("运行中的任务 resume 应报错，得到 %v", err)
	}
	_ = reg.Interrupt(snap.ID)
	reg.Wait(snap.ID)
}

func TestTaskToolsRegistered(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	eng := tools.NewEngine()
	if err := Register(eng, reg); err != nil {
		t.Fatal(err)
	}
	want := "agent_interrupt,agent_resume,agent_send,agent_spawn,task_output"
	if got := strings.Join(eng.SortedNames(), ","); got != want {
		t.Fatalf("工具集 = %s，期望 %s", got, want)
	}

	// task_output 不带 task_id 时返回任务列表。
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "task_output", Arguments: `{}`}})[0]
	if res.Result.IsError || !strings.Contains(res.Result.Content, "[]") {
		t.Fatalf("任务列表 = %+v", res.Result)
	}
}
