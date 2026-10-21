// Package tools 定义工具协议与执行引擎。
//
// 多数朴素的 agent 循环都在这里翻车：工具一个个串行跑、一个慢工具拖死整轮、
// 一个 panic 打掉整个运行。Rei 的引擎把调用分桶为并行（只读）与串行（写）两相，
// 给每次调用加超时，把 panic 收敛成错误结果，并对超长输出做截断。
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tasohower/rei/core"
)

// 引擎默认值。
const (
	DefaultMaxParallel = 4
	DefaultToolTimeout = 5 * time.Minute
	DefaultMaxOutput   = 20_000
)

// Result 是工具交还给循环的结果。
type Result struct {
	Content   string
	Images    []core.Content
	IsError   bool
	Truncated bool
	Metadata  map[string]any
}

// ErrorResult 构造一个失败结果。
func ErrorResult(format string, args ...any) Result {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}
}

// Tool 是一个可调用能力。
type Tool interface {
	Name() string
	Description() string
	// Parameters 是描述参数结构的 JSON Schema 对象。
	Parameters() json.RawMessage
	// CanParallel 表示该工具只读，可以和同一轮里其它并行安全工具并发执行。
	CanParallel() bool
	Call(ctx context.Context, call core.ToolCall) Result
}

// Spec 是给模型看的工具描述。
type Spec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ExecResult 把调用、结果与耗时绑在一起。
type ExecResult struct {
	Call     core.ToolCall
	Result   Result
	Duration time.Duration
}

// Engine 执行已注册的工具。
type Engine struct {
	mu          sync.RWMutex
	tools       map[string]Tool
	order       []string
	maxParallel int
	timeout     time.Duration
	maxOutput   int
}

// Option 配置引擎。
type Option func(*Engine)

// WithMaxParallel 设置并发上限。
func WithMaxParallel(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxParallel = n
		}
	}
}

// WithToolTimeout 设置单次调用的超时。
func WithToolTimeout(d time.Duration) Option {
	return func(e *Engine) {
		if d > 0 {
			e.timeout = d
		}
	}
}

// WithMaxOutputBytes 设置回给模型的输出上限。
func WithMaxOutputBytes(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxOutput = n
		}
	}
}

// NewEngine 构造带默认值的引擎。
func NewEngine(opts ...Option) *Engine {
	e := &Engine{
		tools:       map[string]Tool{},
		maxParallel: DefaultMaxParallel,
		timeout:     DefaultToolTimeout,
		maxOutput:   DefaultMaxOutput,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Register 注册一个工具。工具名唯一；重名返回错误而不是静默覆盖。
func (e *Engine) Register(t Tool) error {
	if t == nil {
		return errors.New("tools: 工具为 nil")
	}
	name := strings.TrimSpace(t.Name())
	if name == "" {
		return errors.New("tools: 工具缺少名称")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.tools[name]; ok {
		return fmt.Errorf("tools: 工具 %q 重复注册", name)
	}
	if err := validateSchema(t.Parameters()); err != nil {
		return fmt.Errorf("tools: %s: %w", name, err)
	}
	e.tools[name] = t
	e.order = append(e.order, name)
	return nil
}

func validateSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("参数 schema 为空")
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("参数 schema 非法：%w", err)
	}
	if probe["type"] != "object" {
		return errors.New(`参数 schema 必须有 "type": "object"`)
	}
	return nil
}

// Lookup 查找已注册工具。
func (e *Engine) Lookup(name string) (Tool, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.tools[name]
	return t, ok
}

// Specs 按注册顺序返回给模型看的工具清单。
func (e *Engine) Specs() []Spec {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Spec, 0, len(e.order))
	for _, name := range e.order {
		t := e.tools[name]
		out = append(out, Spec{Name: t.Name(), Description: t.Description(), Parameters: t.Parameters()})
	}
	return out
}

// Names 按注册顺序返回工具名。
func (e *Engine) Names() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]string(nil), e.order...)
}

// Execute 执行一批调用，并保持模型给出的顺序返回结果。
// 连续的可并行调用组成一个批次并发执行；遇到串行工具时先冲刷待执行批次，再单独执行。
func (e *Engine) Execute(ctx context.Context, calls []core.ToolCall) []ExecResult {
	results := make([]ExecResult, len(calls))
	i := 0
	for i < len(calls) {
		if !e.isParallel(calls[i].Name) {
			results[i] = e.runOne(ctx, calls[i])
			i++
			continue
		}
		j := i
		for j < len(calls) && e.isParallel(calls[j].Name) {
			j++
		}
		e.runBatch(ctx, calls[i:j], results[i:j])
		i = j
	}
	return results
}

func (e *Engine) isParallel(name string) bool {
	t, ok := e.Lookup(name)
	return ok && t.CanParallel()
}

func (e *Engine) runBatch(ctx context.Context, calls []core.ToolCall, out []ExecResult) {
	sem := make(chan struct{}, e.maxParallel)
	var wg sync.WaitGroup
	for idx := range calls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[idx] = e.runOne(ctx, calls[idx])
		}(idx)
	}
	wg.Wait()
}

func (e *Engine) runOne(ctx context.Context, call core.ToolCall) ExecResult {
	start := time.Now()
	res := e.callSafely(ctx, call)
	res.Result = e.truncate(res.Result)
	res.Duration = time.Since(start)
	return res
}

// callSafely 施加超时并把 panic 转成错误结果：
// 坏工具退化为模型可见的失败，而不是杀死整个运行。
func (e *Engine) callSafely(ctx context.Context, call core.ToolCall) (out ExecResult) {
	t, ok := e.Lookup(call.Name)
	if !ok {
		return ExecResult{Call: call, Result: ErrorResult("unknown tool %q", call.Name)}
	}

	timeout := e.timeout
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan Result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- ErrorResult("tool %q panicked: %v", call.Name, r)
			}
		}()
		done <- t.Call(callCtx, call)
	}()

	select {
	case res := <-done:
		return ExecResult{Call: call, Result: res}
	case <-callCtx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return ExecResult{Call: call, Result: ErrorResult("tool %q canceled", call.Name)}
		}
		return ExecResult{Call: call, Result: ErrorResult("tool %q timed out after %s", call.Name, timeout)}
	}
}

// truncate 保留超长输出的头部与尾部：头部通常是命令输出，尾部通常是错误，
// 中间的信息密度最低。
func (e *Engine) truncate(r Result) Result {
	if len(r.Content) <= e.maxOutput {
		return r
	}
	head := e.maxOutput * 2 / 3
	tail := e.maxOutput - head
	omitted := len(r.Content) - head - tail
	r.Content = r.Content[:head] +
		fmt.Sprintf("\n... [truncated %d bytes] ...\n", omitted) +
		r.Content[len(r.Content)-tail:]
	r.Truncated = true
	return r
}

// SortedNames 返回排序后的工具名，便于诊断输出稳定。
func (e *Engine) SortedNames() []string {
	names := e.Names()
	sort.Strings(names)
	return names
}
