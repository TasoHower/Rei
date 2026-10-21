package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tasohower/rei/core"
)

// fakeTool 用闭包模拟任意行为，避免为每个用例写一个类型。
type fakeTool struct {
	name     string
	parallel bool
	call     func(ctx context.Context, call core.ToolCall) Result
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "测试工具" }
func (f *fakeTool) CanParallel() bool   { return f.parallel }
func (f *fakeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (f *fakeTool) Call(ctx context.Context, call core.ToolCall) Result {
	return f.call(ctx, call)
}

func mustRegister(t *testing.T, e *Engine, ts ...Tool) {
	t.Helper()
	for _, tool := range ts {
		if err := e.Register(tool); err != nil {
			t.Fatalf("注册 %s 失败：%v", tool.Name(), err)
		}
	}
}

func TestRegisterRejectsDuplicatesAndBadSchema(t *testing.T) {
	e := NewEngine()
	mustRegister(t, e, &fakeTool{name: "a", call: func(context.Context, core.ToolCall) Result { return Result{} }})
	if err := e.Register(&fakeTool{name: "a", call: func(context.Context, core.ToolCall) Result { return Result{} }}); err == nil {
		t.Fatal("重名注册应当报错")
	}
	if err := e.Register(&fakeTool{name: "", call: func(context.Context, core.ToolCall) Result { return Result{} }}); err == nil {
		t.Fatal("空名注册应当报错")
	}
}

func TestExecuteRunsParallelToolsConcurrently(t *testing.T) {
	e := NewEngine(WithMaxParallel(4))
	var wg sync.WaitGroup
	wg.Add(2)
	slow := func(context.Context, core.ToolCall) Result {
		time.Sleep(60 * time.Millisecond)
		wg.Done()
		return Result{Content: "ok"}
	}
	mustRegister(t, e,
		&fakeTool{name: "p1", parallel: true, call: slow},
		&fakeTool{name: "p2", parallel: true, call: slow},
	)

	start := time.Now()
	results := e.Execute(context.Background(), []core.ToolCall{{Name: "p1"}, {Name: "p2"}})
	elapsed := time.Since(start)

	if len(results) != 2 || results[0].Result.Content != "ok" || results[1].Result.Content != "ok" {
		t.Fatalf("结果异常：%+v", results)
	}
	if elapsed > 110*time.Millisecond {
		t.Fatalf("两个 60ms 工具串行执行了 %s，说明没有并发", elapsed)
	}
}

func TestSerialToolFlushesParallelBatch(t *testing.T) {
	e := NewEngine()
	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context, core.ToolCall) Result {
		return func(context.Context, core.ToolCall) Result {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return Result{Content: name}
		}
	}
	mustRegister(t, e,
		&fakeTool{name: "read", parallel: true, call: record("read")},
		&fakeTool{name: "write", parallel: false, call: record("write")},
	)

	results := e.Execute(context.Background(), []core.ToolCall{{Name: "read"}, {Name: "write"}})
	if results[0].Call.Name != "read" || results[1].Call.Name != "write" {
		t.Fatalf("结果顺序必须与调用顺序一致：%+v", results)
	}
	if len(order) != 2 {
		t.Fatalf("执行次数 = %d", len(order))
	}
}

func TestPanicBecomesErrorResult(t *testing.T) {
	e := NewEngine()
	mustRegister(t, e, &fakeTool{name: "boom", parallel: true, call: func(context.Context, core.ToolCall) Result {
		panic("炸了")
	}})

	res := e.Execute(context.Background(), []core.ToolCall{{Name: "boom"}})[0]
	if !res.Result.IsError || !strings.Contains(res.Result.Content, "panic") {
		t.Fatalf("panic 应当转成错误结果：%+v", res.Result)
	}
}

func TestUnknownTool(t *testing.T) {
	res := NewEngine().Execute(context.Background(), []core.ToolCall{{Name: "nope"}})[0]
	if !res.Result.IsError || !strings.Contains(res.Result.Content, "unknown tool") {
		t.Fatalf("未知工具应当报错：%+v", res.Result)
	}
}

func TestTimeout(t *testing.T) {
	e := NewEngine(WithToolTimeout(20 * time.Millisecond))
	mustRegister(t, e, &fakeTool{name: "sleepy", parallel: true, call: func(ctx context.Context, _ core.ToolCall) Result {
		<-ctx.Done()
		return Result{Content: "太晚了"}
	}})

	res := e.Execute(context.Background(), []core.ToolCall{{Name: "sleepy"}})[0]
	if !res.Result.IsError || !strings.Contains(res.Result.Content, "timed out") {
		t.Fatalf("超时应当返回错误结果：%+v", res.Result)
	}
}

func TestTruncationKeepsHeadAndTail(t *testing.T) {
	e := NewEngine(WithMaxOutputBytes(30))
	mustRegister(t, e, &fakeTool{name: "big", parallel: true, call: func(context.Context, core.ToolCall) Result {
		return Result{Content: strings.Repeat("头", 10) + strings.Repeat("中", 100) + strings.Repeat("尾", 10)}
	}})

	res := e.Execute(context.Background(), []core.ToolCall{{Name: "big"}})[0]
	if !res.Result.Truncated {
		t.Fatal("超长输出应标记为已截断")
	}
	if !strings.HasPrefix(res.Result.Content, "头") || !strings.HasSuffix(res.Result.Content, "尾") {
		t.Fatalf("截断应保留头尾：%q", res.Result.Content)
	}
	if !strings.Contains(res.Result.Content, "truncated") {
		t.Fatalf("缺少截断标记：%q", res.Result.Content)
	}
}

func TestSpecsAndNames(t *testing.T) {
	e := NewEngine()
	mustRegister(t, e,
		&fakeTool{name: "z", call: func(context.Context, core.ToolCall) Result { return Result{} }},
		&fakeTool{name: "a", call: func(context.Context, core.ToolCall) Result { return Result{} }},
	)
	if specs := e.Specs(); len(specs) != 2 || specs[0].Name != "z" {
		t.Fatalf("Specs 应保持注册顺序：%+v", specs)
	}
	if names := e.SortedNames(); names[0] != "a" || names[1] != "z" {
		t.Fatalf("SortedNames = %v", names)
	}
}
