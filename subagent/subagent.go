// Package subagent 提供两级子 agent 能力：
//
//   - 同步只读调研（ExploreTool）：在当前运行里就地嵌套一次子运行，事件带
//     parent_run_id 与 depth，结果直接作为工具结果返回；
//   - 后台任务（Registry + 四个工具）：跨 Run 存活，写 journal 落盘，
//     结果可回填主会话。
//
// 两者的共同点是**事件树可还原**：子运行复用父运行的 EventHandler，
// 只在信封里写上 ParentRunID / Depth，投影端不需要额外的父子表。
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tasohower/rei/agents"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/tools"
)

// 默认深度上限：顶层(0) → 子 agent(1) → 孙 agent(2)。
const DefaultMaxDepth = 2

// ExploreTool 是同步只读子 agent 工具。
//
// 它本身不限制工具集：调用方应当传入一个只挂了只读工具的 AgentLoop
// （见 ReadOnlyEngine）：限制发生在装配期，而不是运行期黑名单。
type ExploreTool struct {
	loop     *agents.AgentLoop
	name     string
	maxDepth int
	maxRunes int
}

// ExploreOption 配置 ExploreTool。
type ExploreOption func(*ExploreTool)

// WithExploreName 设置工具名（默认 explore）。
func WithExploreName(name string) ExploreOption {
	return func(t *ExploreTool) {
		if name != "" {
			t.name = name
		}
	}
}

// WithExploreMaxDepth 设置深度上限。
func WithExploreMaxDepth(n int) ExploreOption {
	return func(t *ExploreTool) {
		if n > 0 {
			t.maxDepth = n
		}
	}
}

// WithExploreMaxRunes 设置返回文本上限（默认 8000 runes）。
func WithExploreMaxRunes(n int) ExploreOption {
	return func(t *ExploreTool) {
		if n > 0 {
			t.maxRunes = n
		}
	}
}

// NewExploreTool 构造同步子 agent 工具。
func NewExploreTool(loop *agents.AgentLoop, opts ...ExploreOption) (tools.Tool, error) {
	if loop == nil {
		return nil, errors.New("subagent: explore 需要 AgentLoop")
	}
	t := &ExploreTool{loop: loop, name: "explore", maxDepth: DefaultMaxDepth, maxRunes: taskTextCap}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

func (t *ExploreTool) Name() string { return t.name }

func (t *ExploreTool) Description() string {
	return "Run a read-only research sub-agent on a self-contained task and return its final answer."
}

func (t *ExploreTool) CanParallel() bool { return false }

func (t *ExploreTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"Self-contained instruction for the sub-agent; include all context it needs."}},"required":["task"]}`)
}

func (t *ExploreTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	if strings.TrimSpace(args.Task) == "" {
		return tools.ErrorResult("task is required")
	}

	info, _ := events.RunInfoFrom(ctx)
	childDepth := info.Depth + 1
	if childDepth > t.maxDepth {
		return tools.ErrorResult("sub-agent depth limit reached (max %d)", t.maxDepth)
	}

	childRunID := newTaskID("run")
	childCtx := events.WithRunInfo(ctx, events.RunInfo{
		RunID:       childRunID,
		ParentRunID: info.RunID,
		Depth:       childDepth,
		Emit:        info.Emit,
	})

	res, err := t.loop.RunStream(childCtx, agents.RunRequest{
		RunID:       childRunID,
		ParentRunID: info.RunID,
		Depth:       childDepth,
		Messages:    []core.Message{core.NewUserTextMessage(args.Task)},
	}, info.Emit)
	if err != nil {
		return tools.ErrorResult("sub-agent failed: %v", err)
	}
	text := strings.TrimSpace(res.FinalText)
	if text == "" {
		return tools.ErrorResult("sub-agent produced no final answer")
	}
	return tools.Result{
		Content:  truncateRunes(text, t.maxRunes),
		Usage:    res.Usage,
		Metadata: map[string]any{"run_id": childRunID},
	}
}

// ReadOnlyEngine 复制出一个只包含只读工具的新引擎。
//
// 白名单是显式的：调用方必须自己决定哪些工具算「只读」。默认包含
// read_file / grep / glob，其余需要显式追加（例如只读的 MCP 工具）。
func ReadOnlyEngine(src *tools.Engine, extra ...string) *tools.Engine {
	allowed := map[string]bool{"read_file": true, "grep": true, "glob": true}
	for _, name := range extra {
		allowed[name] = true
	}
	dst := tools.NewEngine()
	if src == nil {
		return dst
	}
	for _, name := range src.Names() {
		if !allowed[name] {
			continue
		}
		if tool, ok := src.Lookup(name); ok {
			_ = dst.Register(tool)
		}
	}
	return dst
}

func defaultJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func newTaskID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, nextSeq())
}
