// Package agents 实现 ReAct 运行引擎：一次 Run 内反复执行
// 「压缩检查 → 调模型 → 处理工具调用」，直到产出最终答复或触及护栏。
//
// 引擎无状态：配置在构造时冻结，Run 期间的所有可变状态都放在 RunResult 里，
// 因此同一个 AgentLoop 可以被多个会话并发复用。
package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tasohower/rei/compress"
	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
	"github.com/tasohower/rei/tools"
)

// 默认护栏。
const (
	DefaultMaxIterations    = 100
	DefaultMaxTokens        = 8192
	DefaultFirstChunkTimout = 90 * time.Second
	DefaultStallTimeout     = 3 * time.Minute
)

// AgentLoop 是运行引擎。
type AgentLoop struct {
	prov     provider.Provider
	model    string
	engine   *tools.Engine
	system   string
	maxToken int

	maxIterations int
	maxCostMicro  int64

	compressor   compress.Compressor
	approver     events.Approver
	asker        events.Asker
	backoff      provider.Backoff
	firstChunk   time.Duration
	stallTimeout time.Duration

	// pricing 可选：配置后每轮调用都会折算 µUSD 成本，预算护栏才真正生效。
	pricing *core.Pricing
}

// Option 配置 AgentLoop。
type Option func(*AgentLoop)

// WithModel 设置模型名。
func WithModel(model string) Option { return func(l *AgentLoop) { l.model = model } }

// WithToolEngine 挂载工具引擎。未挂载时模型无法调用工具。
func WithToolEngine(e *tools.Engine) Option { return func(l *AgentLoop) { l.engine = e } }

// WithMaxTokens 设置单次输出的 token 上限。
func WithMaxTokens(n int) Option {
	return func(l *AgentLoop) {
		if n > 0 {
			l.maxToken = n
		}
	}
}

// WithMaxIterations 设置 ReAct 轮次上限。
func WithMaxIterations(n int) Option {
	return func(l *AgentLoop) {
		if n > 0 {
			l.maxIterations = n
		}
	}
}

// WithMaxCost 设置运行成本上限（µUSD）。超过后不再发起新的模型调用。
func WithMaxCost(microUSD int64) Option {
	return func(l *AgentLoop) {
		if microUSD > 0 {
			l.maxCostMicro = microUSD
		}
	}
}

// WithSystemPromptExtra 在基础系统提示之后追加宿主内容。
func WithSystemPromptExtra(extra string) Option {
	return func(l *AgentLoop) { l.system = buildSystemPrompt(extra) }
}

// WithCompressor 挂载压缩器。
func WithCompressor(c compress.Compressor) Option { return func(l *AgentLoop) { l.compressor = c } }

// WithApprover 挂载审批器。未挂载时所有工具调用直接放行。
func WithApprover(a events.Approver) Option { return func(l *AgentLoop) { l.approver = a } }

// WithAsker 挂载结构化提问器。
func WithAsker(a events.Asker) Option { return func(l *AgentLoop) { l.asker = a } }

// WithBackoff 设置重试退避。
func WithBackoff(b provider.Backoff) Option { return func(l *AgentLoop) { l.backoff = b } }

// WithPricing 设置模型价表（每百万 token 的 µUSD 单价）。
// 不配置时用量只统计 token，WithMaxCost 无法生效。
func WithPricing(p core.Pricing) Option { return func(l *AgentLoop) { l.pricing = &p } }

// WithFirstChunkTimeout 设置首字节超时。
func WithFirstChunkTimeout(d time.Duration) Option {
	return func(l *AgentLoop) {
		if d > 0 {
			l.firstChunk = d
		}
	}
}

// WithStallTimeout 设置流中段停滞超时。
func WithStallTimeout(d time.Duration) Option {
	return func(l *AgentLoop) {
		if d > 0 {
			l.stallTimeout = d
		}
	}
}

// NewAgentLoop 构造引擎。
func NewAgentLoop(p provider.Provider, opts ...Option) (*AgentLoop, error) {
	if p == nil {
		return nil, errors.New("agents: Provider 不能为空")
	}
	l := &AgentLoop{
		prov:          p,
		system:        buildSystemPrompt(""),
		maxToken:      DefaultMaxTokens,
		maxIterations: DefaultMaxIterations,
		backoff:       provider.DefaultBackoff(),
		firstChunk:    DefaultFirstChunkTimout,
		stallTimeout:  DefaultStallTimeout,
	}
	for _, opt := range opts {
		opt(l)
	}
	if strings.TrimSpace(l.model) == "" {
		return nil, errors.New("agents: 必须通过 WithModel 指定模型")
	}
	return l, nil
}

// Model 返回当前模型名。
func (l *AgentLoop) Model() string { return l.model }

// RunRequest 是一次运行的输入。
type RunRequest struct {
	SessionID string
	RunID     string
	// Messages 是完整上下文（含历史）。引擎不会修改调用方切片。
	Messages []core.Message
	// Approver 覆盖引擎级审批器：会话把它自己的审批队列传进来，
	// 这样同一个 AgentLoop 可以被多个会话共享而互不干扰。
	Approver events.Approver
}

// RunResult 是一次运行的产出。
type RunResult struct {
	FinishReason string
	// Messages 是运行结束后的完整上下文，宿主应整体替换旧历史。
	Messages   []core.Message
	FinalText  string
	Usage      core.Usage
	Iterations int
	Duration   time.Duration
}

// RunStream 驱动一次运行，并通过 handler 发布事件。
//
// 返回的 error 只表示「运行失败」；预算耗尽、轮次耗尽、用户中断都属于正常结束，
// 通过 FinishReason 表达。
func (l *AgentLoop) RunStream(ctx context.Context, req RunRequest, handler events.EventHandler) (*RunResult, error) {
	start := time.Now()
	msgs := append([]core.Message(nil), req.Messages...)
	runID := req.RunID
	if runID == "" {
		runID = fmt.Sprintf("run-%d", start.UnixNano())
	}

	idx := 0
	nextBase := func() events.Base {
		idx++
		return events.Base{RunID: runID, Index: idx, Time: time.Now()}
	}

	approver := l.approver
	if req.Approver != nil {
		approver = req.Approver
	}

	res := &RunResult{}
	events.Emit(ctx, handler, &events.AgentStart{
		Base:         nextBase(),
		Model:        l.model,
		MessageCount: len(msgs),
	})

	finish := core.FinishStop
	var runErr error

loop:
	for iter := 1; iter <= l.maxIterations; iter++ {
		res.Iterations = iter
		if err := ctx.Err(); err != nil {
			finish = core.FinishAbort
			runErr = err
			break
		}
		if l.maxCostMicro > 0 && res.Usage.Cost.Total() >= l.maxCostMicro {
			finish = core.FinishMaxCost
			events.Emit(ctx, handler, &events.SessionBudgetExceeded{
				Base:           nextBase(),
				BudgetMicroUSD: l.maxCostMicro,
				UsedMicroUSD:   res.Usage.Cost.Total(),
			})
			break
		}

		// 压缩：只在调用前做，避免把刚写入的工具结果立刻摘要掉。
		msgs = l.maybeCompress(ctx, msgs, res, handler, nextBase)

		out, err := l.streamOnce(ctx, msgs, handler, runID, &idx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				finish = core.FinishAbort
			} else {
				finish = core.FinishError
			}
			runErr = err
			break
		}
		res.Usage.Add(out.usage)

		assistant := core.NewAssistantMessage(textBlocks(out.text), out.reasoning, out.toolCalls)
		assistant.Model = l.model
		msgs = append(msgs, assistant)
		if out.text != "" {
			res.FinalText = out.text
		}

		if len(out.toolCalls) == 0 {
			finish = core.FinishStop
			break
		}

		results := l.executeTools(ctx, out.toolCalls, handler, nextBase, approver)
		for _, r := range results {
			msgs = append(msgs, core.NewToolMessage(r.Call.ID, r.Call.Name, r.text()))
		}

		if iter == l.maxIterations {
			finish = core.FinishMaxIterations
			break loop
		}
	}

	res.FinishReason = finish
	res.Messages = msgs
	res.Duration = time.Since(start)

	events.Emit(ctx, handler, &events.AgentEnd{
		Base:         nextBase(),
		FinishReason: finish,
		FinalText:    res.FinalText,
		Usage:        res.Usage,
		DurationMS:   res.Duration.Milliseconds(),
	})
	return res, runErr
}

// maybeCompress 在需要时压缩上下文，并把压缩消耗计入用量。
// 压缩失败时保留原上下文（绝不截断式降级），只把错误写进事件。
func (l *AgentLoop) maybeCompress(ctx context.Context, msgs []core.Message, res *RunResult, handler events.EventHandler, nextBase func() events.Base) []core.Message {
	if l.compressor == nil {
		return msgs
	}
	need, reason := l.compressor.Decide(msgs, res.Usage)
	if !need {
		return msgs
	}
	events.Emit(ctx, handler, &events.CompressStart{Base: nextBase(), MessageCount: len(msgs), Reason: reason})

	compressed, err := l.compressor.Compress(ctx, msgs)
	if err != nil {
		events.Emit(ctx, handler, &events.CompressEnd{
			Base:           nextBase(),
			MessagesBefore: len(msgs),
			MessagesAfter:  len(msgs),
			Err:            err.Error(),
		})
		return msgs
	}
	res.Usage.Add(compressed.Usage)
	events.Emit(ctx, handler, &events.CompressEnd{
		Base:           nextBase(),
		MessagesBefore: len(msgs),
		MessagesAfter:  len(compressed.Messages),
		Usage:          compressed.Usage,
	})
	if err := core.ValidateConversation(compressed.Messages); err != nil {
		// 压缩产物破坏了「调用-回执」配对时，宁可保留原上下文。
		return msgs
	}
	return compressed.Messages
}

// toolOutcome 是单个工具调用的结果，附带被拒绝时的文案。
type toolOutcome struct {
	Call   core.ToolCall
	Result tools.Result
}

func (o toolOutcome) text() string {
	if o.Result.IsError {
		return "Error: " + o.Result.Content
	}
	return o.Result.Content
}

// executeTools 先逐个审批，再交给引擎分批执行（只读并行、写串行）。
func (l *AgentLoop) executeTools(ctx context.Context, calls []core.ToolCall, handler events.EventHandler, nextBase func() events.Base, approver events.Approver) []toolOutcome {
	out := make([]toolOutcome, 0, len(calls))
	approved := make([]core.ToolCall, 0, len(calls))

	for _, call := range calls {
		events.Emit(ctx, handler, &events.ToolRunStart{Base: nextBase(), CallID: call.ID, Name: call.Name, Arguments: call.Arguments})
		if approver == nil {
			approved = append(approved, call)
			continue
		}
		events.Emit(ctx, handler, &events.ToolApprovalRequested{
			Base: nextBase(), CallID: call.ID, Name: call.Name, Arguments: call.Arguments,
		})
		decision, err := approver.Approve(ctx, events.ApprovalRequest{
			CallID: call.ID, ToolName: call.Name, Arguments: call.Arguments,
		})
		approvedFlag := err == nil && decision.Approved
		reason := decision.Reason
		if err != nil {
			reason = err.Error()
		}
		events.Emit(ctx, handler, &events.ToolApprovalResolved{
			Base: nextBase(), CallID: call.ID, Approved: approvedFlag, Reason: reason,
		})
		if !approvedFlag {
			msg := "User denied this tool call"
			if reason != "" {
				msg += ": " + reason
			}
			out = append(out, toolOutcome{Call: call, Result: tools.ErrorResult("%s", msg)})
			continue
		}
		if decision.Arguments != nil {
			call.Arguments = *decision.Arguments
		}
		approved = append(approved, call)
	}

	if len(approved) > 0 {
		if l.engine == nil {
			for _, call := range approved {
				out = append(out, toolOutcome{Call: call, Result: tools.ErrorResult("No tool engine is attached to the runtime")})
			}
		} else {
			for _, r := range l.engine.Execute(ctx, approved) {
				out = append(out, toolOutcome{Call: r.Call, Result: r.Result})
			}
		}
	}

	for i := range out {
		events.Emit(ctx, handler, &events.ToolRunEnd{
			Base:       nextBase(),
			CallID:     out[i].Call.ID,
			Name:       out[i].Call.Name,
			Content:    out[i].Result.Content,
			IsError:    out[i].Result.IsError,
			Truncated:  out[i].Result.Truncated,
			DurationMS: 0,
		})
	}
	return out
}

func textBlocks(text string) []core.Content {
	if text == "" {
		return nil
	}
	return []core.Content{{Type: core.ContentTypeText, Content: text}}
}
