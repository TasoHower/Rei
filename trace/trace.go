// Package trace 把事件流投影成调用树（span），并保留按 id 回查原文的入口。
//
// 设计约束来自实测：投影结果只取**元数据**（约 260 B/span），
// 正文一律不复制。详情通过 span ID（tool 的 call_id、llm/compress/plan 的
// 派生 id、run 的 run_id）在 Detail 表里按需回查，因此列表投影可以放心地
// 上限截断，而不会丢正文。
package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/tasohower/rei/events"
)

// Kind 是 span 类型。
type Kind string

const (
	KindRun      Kind = "run"
	KindLLM      Kind = "llm"
	KindTool     Kind = "tool"
	KindCompress Kind = "compress"
	KindPlan     Kind = "plan"
	// KindWait 是人在环的等待段（审批、提问），单独成 span 才能看出时间去哪了。
	KindWait Kind = "wait"
)

// Span 是投影后的一个调用节点，只含元数据。
type Span struct {
	Kind     Kind   `json:"kind"`
	ID       string `json:"id"`
	RunID    string `json:"run_id,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Status   string `json:"status"`
	Depth    int    `json:"depth,omitempty"`
	// StartAt 是相对整条 trace 起点的毫秒偏移；DurMS 是时长。
	StartAt int64 `json:"start_at"`
	DurMS   int64 `json:"dur_ms"`

	Model          string `json:"model,omitempty"`
	InTokens       int    `json:"in_tok,omitempty"`
	OutTokens      int    `json:"out_tok,omitempty"`
	CachedTokens   int    `json:"cache_tok,omitempty"`
	ReasoningToken int    `json:"reason_tok,omitempty"`
	CostMicro      int64  `json:"cost_micro,omitempty"`

	Before int `json:"before,omitempty"`
	After  int `json:"after,omitempty"`

	IsError   bool `json:"is_error,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
	// RequestID 是上游响应 id（llm span），详情优先按它回查。
	RequestID string `json:"request_id,omitempty"`
	// Retries 是该次调用发生过的可重试失败次数。
	Retries int `json:"retries,omitempty"`
}

// Trace 是一条以 root run 为根的调用树。
type Trace struct {
	ID     string `json:"id"`
	Root   string `json:"root"`
	WallMS int64  `json:"wall_ms"`
	Spans  []Span `json:"spans"`
}

// TraceSummary 是列表视图（不含 span），宿主据此渲染列表。
type TraceSummary struct {
	ID     string `json:"id"`
	Root   string `json:"root"`
	WallMS int64  `json:"wall_ms"`
	Spans  int    `json:"spans"`
	Tools  int    `json:"tools"`
	Errors int    `json:"errors"`
	In     int    `json:"in_tok"`
	Out    int    `json:"out_tok"`
	Cost   int64  `json:"cost_micro"`
}

// Detail 是详情回查结果：Payload 是原始事件（可能含正文）。
type Detail struct {
	Kind    Kind
	ID      string
	Payload events.Event
}

// Result 是一次投影的产物。
type Result struct {
	Summaries []TraceSummary
	Traces    []Trace
	HasMore   bool
	// MetaBytes 是 span 列表的 JSON 体积，用于监控投影成本。
	MetaBytes int

	details map[string]Detail
}

// Detail 按 kind + id 回查原文。
func (r *Result) Detail(kind Kind, id string) (Detail, bool) {
	if r == nil {
		return Detail{}, false
	}
	d, ok := r.details[detailKey(kind, id)]
	return d, ok
}

func detailKey(kind Kind, id string) string { return string(kind) + ":" + id }

// runNode 是投影期间的运行节点。
type runNode struct {
	start  *events.AgentStart
	end    *events.AgentEnd
	parent string
	depth  int
}

func (n *runNode) status() string {
	if n.end == nil {
		return "running"
	}
	if n.end.FinishReason == "error" {
		return "error"
	}
	return "ok"
}

func (n *runNode) durMS() int64 {
	if n.end != nil {
		return n.end.DurationMS
	}
	return 0
}

// Build 是纯函数：输入事件切片，输出投影结果。limit <= 0 时默认 50 条。
func Build(evs []events.Event, limit int) *Result {
	if limit <= 0 {
		limit = 50
	}
	res := &Result{details: map[string]Detail{}}

	runs := map[string]*runNode{}
	runOrder := []string{}
	traceStart := time.Time{}

	for _, ev := range evs {
		if b := ev.Envelope(); b != nil && !b.Time.IsZero() {
			if traceStart.IsZero() || b.Time.Before(traceStart) {
				traceStart = b.Time
			}
		}
		switch e := ev.(type) {
		case *events.AgentStart:
			if _, ok := runs[e.RunID]; !ok {
				runOrder = append(runOrder, e.RunID)
				runs[e.RunID] = &runNode{}
			}
			runs[e.RunID].start = e
			runs[e.RunID].parent = e.ParentRunID
			runs[e.RunID].depth = e.Depth
			res.details[detailKey(KindRun, e.RunID)] = Detail{Kind: KindRun, ID: e.RunID, Payload: e}
		case *events.AgentEnd:
			if _, ok := runs[e.RunID]; !ok {
				runOrder = append(runOrder, e.RunID)
				runs[e.RunID] = &runNode{}
			}
			runs[e.RunID].end = e
			res.details[detailKey(KindRun, e.RunID)] = Detail{Kind: KindRun, ID: e.RunID, Payload: e}
		}
	}

	rel := func(t time.Time) int64 {
		if traceStart.IsZero() || t.IsZero() {
			return 0
		}
		return t.Sub(traceStart).Milliseconds()
	}

	spansByRun := map[string][]Span{}
	open := map[string]int64{}       // span id -> start offset
	pending := map[string][]string{} // run id -> 待闭合 span id 栈
	approvalStart := map[string]int64{}
	seq := map[string]int{} // run id -> 派生 id 序号

	push := func(runID string, span Span) {
		spansByRun[runID] = append(spansByRun[runID], span)
	}
	nextID := func(runID, kind string) string {
		seq[runID+"#"+kind]++
		return fmt.Sprintf("%s#%s%d", runID, kind, seq[runID+"#"+kind])
	}
	latest := func(runID string) string {
		ids := pending[runID]
		if len(ids) == 0 {
			return ""
		}
		return ids[len(ids)-1]
	}
	pop := func(runID string) string {
		id := latest(runID)
		if id != "" {
			pending[runID] = pending[runID][:len(pending[runID])-1]
		}
		return id
	}
	update := func(runID, id string, fn func(*Span)) {
		spans := spansByRun[runID]
		for i := range spans {
			if spans[i].ID == id {
				fn(&spans[i])
				return
			}
		}
	}

	for _, ev := range evs {
		switch e := ev.(type) {
		case *events.LLMStart:
			id := nextID(e.RunID, "llm")
			open[id] = rel(e.Time)
			pending[e.RunID] = append(pending[e.RunID], id)
			push(e.RunID, Span{Kind: KindLLM, ID: id, RunID: e.RunID, ParentID: e.RunID,
				Name: e.Model, Model: e.Model, Status: "running", StartAt: rel(e.Time)})
		case *events.LLMEnd:
			id := pop(e.RunID)
			start := open[id]
			update(e.RunID, id, func(s *Span) {
				s.Status = "ok"
				// 重试成功不算错误：Retries 保留痕迹，IsError 归位。
				s.IsError = false
				s.DurMS = e.DurationMS
				if s.DurMS == 0 {
					s.DurMS = rel(e.Time) - start
				}
				s.InTokens = e.Usage.Input
				s.OutTokens = e.Usage.Output
				s.CachedTokens = e.Usage.CachedInput
				s.ReasoningToken = e.Usage.Reasoning
				s.CostMicro = e.Usage.Cost.Total()
			})
			res.details[detailKey(KindLLM, id)] = Detail{Kind: KindLLM, ID: id, Payload: e}
			update(e.RunID, id, func(s *Span) { s.RequestID = e.RequestID })
			if e.RequestID != "" {
				res.details[detailKey(KindLLM, e.RequestID)] = Detail{Kind: KindLLM, ID: e.RequestID, Payload: e}
			}
		case *events.LLMError:
			if id := latest(e.RunID); id != "" {
				update(e.RunID, id, func(s *Span) {
					s.Status = "error"
					s.IsError = true
					s.Retries++
				})
			}
		case *events.ToolRunStart:
			open[e.CallID] = rel(e.Time)
			push(e.RunID, Span{Kind: KindTool, ID: e.CallID, RunID: e.RunID, ParentID: e.RunID,
				Name: e.Name, Status: "running", StartAt: rel(e.Time)})
		case *events.ToolRunEnd:
			start := open[e.CallID]
			update(e.RunID, e.CallID, func(s *Span) {
				s.Status = "ok"
				if e.IsError {
					s.Status = "error"
				}
				s.IsError = e.IsError
				s.Truncated = e.Truncated
				s.DurMS = e.DurationMS
				if s.DurMS == 0 {
					s.DurMS = rel(e.Time) - start
				}
			})
			res.details[detailKey(KindTool, e.CallID)] = Detail{Kind: KindTool, ID: e.CallID, Payload: e}
		case *events.ToolApprovalRequested:
			approvalStart[e.CallID] = rel(e.Time)
		case *events.ToolApprovalResolved:
			start, ok := approvalStart[e.CallID]
			if !ok {
				start = rel(e.Time)
			}
			waitID := e.CallID + "#wait"
			push(e.RunID, Span{
				Kind: KindWait, ID: waitID, RunID: e.RunID, ParentID: e.RunID,
				Name: "approval", Status: "ok", StartAt: start, DurMS: rel(e.Time) - start,
				IsError: !e.Approved,
			})
			res.details[detailKey(KindWait, waitID)] = Detail{Kind: KindWait, ID: waitID, Payload: e}
			res.details[detailKey(KindWait, e.CallID)] = Detail{Kind: KindWait, ID: e.CallID, Payload: e}
		case *events.CompressStart:
			id := nextID(e.RunID, "compress")
			open[id] = rel(e.Time)
			pending[e.RunID] = append(pending[e.RunID], id)
			push(e.RunID, Span{Kind: KindCompress, ID: id, RunID: e.RunID, ParentID: e.RunID,
				Name: e.Reason, Status: "running", StartAt: rel(e.Time), Before: e.MessageCount})
		case *events.CompressEnd:
			id := pop(e.RunID)
			start := open[id]
			update(e.RunID, id, func(s *Span) {
				s.Status = "ok"
				if e.Err != "" {
					s.Status = "error"
					s.IsError = true
				}
				s.After = e.MessagesAfter
				s.DurMS = rel(e.Time) - start
				s.InTokens = e.Usage.Input
				s.OutTokens = e.Usage.Output
				s.CostMicro = e.Usage.Cost.Total()
			})
			res.details[detailKey(KindCompress, id)] = Detail{Kind: KindCompress, ID: id, Payload: e}
		case *events.PlanGenerated:
			id := nextID(e.RunID, "plan")
			push(e.RunID, Span{Kind: KindPlan, ID: id, RunID: e.RunID, ParentID: e.RunID,
				Name: "plan", Status: "ok", StartAt: rel(e.Time)})
			res.details[detailKey(KindPlan, id)] = Detail{Kind: KindPlan, ID: id, Payload: e}
		}
	}

	for _, rootID := range runOrder {
		node := runs[rootID]
		if node == nil || node.parent != "" {
			continue
		}
		tr := Trace{ID: rootID, Root: rootID}
		if node.start != nil {
			tr.Spans = append(tr.Spans, Span{
				Kind: KindRun, ID: rootID, Name: node.start.Model, Status: node.status(),
				Depth: node.depth, StartAt: rel(node.start.Time), DurMS: node.durMS(),
			})
		}
		tr.Spans = append(tr.Spans, spansByRun[rootID]...)

		for _, childID := range runOrder {
			child := runs[childID]
			if child == nil || child.parent != rootID {
				continue
			}
			start := int64(0)
			name := ""
			if child.start != nil {
				start = rel(child.start.Time)
				name = child.start.Model
			}
			tr.Spans = append(tr.Spans, Span{
				Kind: KindRun, ID: childID, ParentID: rootID, Name: name,
				Status: child.status(), Depth: child.depth, StartAt: start, DurMS: child.durMS(),
			})
			tr.Spans = append(tr.Spans, spansByRun[childID]...)
		}

		tr.WallMS = wallMS(tr.Spans)
		res.Traces = append(res.Traces, tr)
	}

	if len(res.Traces) > limit {
		res.Traces = res.Traces[:limit]
		res.HasMore = true
	}
	for i := range res.Traces {
		res.Summaries = append(res.Summaries, summarize(res.Traces[i]))
	}
	if raw, err := json.Marshal(res.Traces); err == nil {
		res.MetaBytes = len(raw)
	}
	return res
}

func wallMS(spans []Span) int64 {
	var out int64
	for _, s := range spans {
		if end := s.StartAt + s.DurMS; end > out {
			out = end
		}
	}
	return out
}

func summarize(tr Trace) TraceSummary {
	sum := TraceSummary{ID: tr.ID, Root: tr.Root, WallMS: tr.WallMS, Spans: len(tr.Spans)}
	for _, s := range tr.Spans {
		switch s.Kind {
		case KindTool:
			sum.Tools++
		case KindLLM:
			sum.In += s.InTokens
			sum.Out += s.OutTokens
		}
		sum.Cost += s.CostMicro
		if s.IsError {
			sum.Errors++
		}
	}
	return sum
}

// ---------------------------------------------------------------- 记录器

// Recorder 是一个把事件留在内存里的 EventHandler。
// 宿主可以把它当历史 buffer 用：满了丢最旧的，投影时再统一 Build。
type Recorder struct {
	mu    sync.Mutex
	evs   []events.Event
	limit int
}

// NewRecorder 构造记录器，limit <= 0 时默认 2000 条。
func NewRecorder(limit int) *Recorder {
	if limit <= 0 {
		limit = 2000
	}
	return &Recorder{limit: limit}
}

// Handle 实现 events.EventHandler。
func (r *Recorder) Handle(_ context.Context, e events.Event) {
	if e == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, e)
	if len(r.evs) > r.limit {
		r.evs = r.evs[len(r.evs)-r.limit:]
	}
}

// Events 返回事件副本。
func (r *Recorder) Events() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Event(nil), r.evs...)
}

// Build 用记录器里的事件做投影。
func (r *Recorder) Build(limit int) *Result { return Build(r.Events(), limit) }
