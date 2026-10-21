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
	prov         provider.Provider
	model        string
	engine       *tools.Engine
	system       string
	extra        string
	instructions string
	maxToken     int

	maxIterations int
	maxCostMicro  int64

	compressor   compress.Compressor
	approver     events.Approver
	asker        events.Asker
	backoff      provider.Backoff
	firstChunk   time.Duration
	stallTimeout time.Duration

	// policy 决定「哪些工具需要审批」；nil 表示挂了 approver 就全问。
	policy events.ApprovalPolicy
	// readOnly 为 true 时，内核拒绝执行任何非只读工具。
	readOnly bool
	// maxContinuations 是输出被 max_tokens 截断后的续写次数。
	maxContinuations int
	// goalReminderEvery > 0 时每 N 轮发一次目标对齐提醒。
	goalReminderEvery int

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
	return func(l *AgentLoop) { l.extra = extra }
}

// WithCompressor 挂载压缩器。
func WithCompressor(c compress.Compressor) Option { return func(l *AgentLoop) { l.compressor = c } }

// WithApprover 挂载审批器。未挂载时所有工具调用直接放行。
func WithApprover(a events.Approver) Option { return func(l *AgentLoop) { l.approver = a } }

// WithAsker 挂载结构化提问器。
func WithAsker(a events.Asker) Option { return func(l *AgentLoop) { l.asker = a } }

// WithApprovalPolicy 设置「哪些工具需要审批」。没有策略时等价于全部要问。
func WithApprovalPolicy(p events.ApprovalPolicy) Option {
	return func(l *AgentLoop) { l.policy = p }
}

// WithReadOnlyMode 让内核强制只读：非只读工具（CanParallel=false）一律拒绝执行。
//
// 这是内核语义，不再依赖宿主装配正确的引擎；子 agent / Plan 模式可以直接打开。
func WithReadOnlyMode() Option { return func(l *AgentLoop) { l.readOnly = true } }

// WithMaxContinuations 设置输出被截断后的续写次数（默认 2）。
func WithMaxContinuations(n int) Option {
	return func(l *AgentLoop) {
		if n >= 0 {
			l.maxContinuations = n
		}
	}
}

// WithGoalReminderEvery 每 N 轮发一次目标对齐提醒（0 表示关闭）。
func WithGoalReminderEvery(n int) Option {
	return func(l *AgentLoop) {
		if n > 0 {
			l.goalReminderEvery = n
		}
	}
}

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
		prov:              p,
		system:            buildSystemPrompt(""),
		maxToken:          DefaultMaxTokens,
		maxIterations:     DefaultMaxIterations,
		maxContinuations:  2,
		goalReminderEvery: 25,
		backoff:           provider.DefaultBackoff(),
		firstChunk:        DefaultFirstChunkTimout,
		stallTimeout:      DefaultStallTimeout,
	}
	for _, opt := range opts {
		opt(l)
	}
	// 系统提示统一在最后拼装：选项顺序不应影响结果。
	parts := make([]string, 0, 2)
	if strings.TrimSpace(l.instructions) != "" {
		parts = append(parts, l.instructions)
	}
	if strings.TrimSpace(l.extra) != "" {
		parts = append(parts, l.extra)
	}
	l.system = buildSystemPrompt(strings.Join(parts, "\n\n"))
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
	// ParentRunID / Depth 由子 agent 工具填写，用于还原事件树。
	// 顶层运行两者都为零值。
	ParentRunID string
	Depth       int
	// Asker 覆盖引擎级提问器（子 agent 透传父运行的能力）。
	Asker events.Asker
	// ApprovalPolicy 覆盖引擎级审批策略。
	ApprovalPolicy events.ApprovalPolicy
	// Deliver 指定后台任务结果的投递目标；nil 时用 Registry 的全局回调。
	Deliver func(events.TaskEnd)
	// Checkpoint 在每个 ReAct 轮次结束时被调用：宿主据此做逐轮检查点，
	// 崩溃时最多丢一轮，而不是整次运行。
	Checkpoint func(ctx context.Context, messages []core.Message, usage core.Usage)
	// Steering 在轮次边界被调用，返回运行期间新到的用户消息（边跑边改）。
	Steering func() []core.Message
}

// runEnv 是信封里与「运行树」相关的字段，随调用链透传。
type runEnv struct {
	RunID       string
	ParentRunID string
	Depth       int
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

	approver := l.approver
	if req.Approver != nil {
		approver = req.Approver
	}
	asker := l.asker
	if req.Asker != nil {
		asker = req.Asker
	}
	policy := l.policy
	if req.ApprovalPolicy != nil {
		policy = req.ApprovalPolicy
	}

	env := runEnv{RunID: runID, ParentRunID: req.ParentRunID, Depth: req.Depth}
	ctx = events.WithRunInfo(ctx, events.RunInfo{
		RunID:       runID,
		ParentRunID: req.ParentRunID,
		Depth:       req.Depth,
		Emit:        handler,
		Asker:       asker,
		Approver:    approver,
		Deliver:     req.Deliver,
	})

	idx := 0
	nextBase := func() events.Base {
		idx++
		return events.Base{
			RunID:       runID,
			ParentRunID: env.ParentRunID,
			Depth:       env.Depth,
			Index:       idx,
			Time:        time.Now(),
		}
	}

	res := &RunResult{}
	events.Emit(ctx, handler, &events.AgentStart{
		Base:         nextBase(),
		Model:        l.model,
		MessageCount: len(msgs),
	})

	checkpoint := func() {
		if req.Checkpoint != nil {
			req.Checkpoint(ctx, append([]core.Message(nil), msgs...), res.Usage)
		}
	}

	finish := core.FinishStop
	var runErr error
	ctxRetried := false
	continuations := 0
	lastSig := ""
	repeatStreak := 0
	repeatNudged := false

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

		out, err := l.streamOnce(ctx, msgs, handler, env, &idx)
		if err != nil {
			// 上下文超限不是死路：强制压缩一次再重试（只做一次，避免死循环）。
			if pe := provider.Classify(err); pe.Kind == provider.KindContextExceeded && l.compressor != nil && !ctxRetried {
				ctxRetried = true
				if shrunk, ok := l.compressNow(ctx, msgs, res, handler, nextBase, "context_exceeded"); ok {
					msgs = shrunk
					continue
				}
			}
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
		assistant.ReasoningSignature = out.signature
		msgs = append(msgs, assistant)
		if out.text != "" {
			res.FinalText = out.text
		}

		if len(out.toolCalls) == 0 {
			// 输出被 max_tokens 截断：续写而不是直接结束。
			if out.finish == "length" && continuations < l.maxContinuations {
				continuations++
				msgs = append(msgs, core.NewUserTextMessage(
					"Your previous answer was cut off by the token limit. Continue exactly where you stopped, without repeating what you already wrote."))
				checkpoint()
				continue
			}
			finish = core.FinishStop
			checkpoint()
			break
		}

		results := l.executeTools(ctx, out.toolCalls, handler, nextBase, approver, policy)
		for _, r := range results {
			msgs = append(msgs, core.NewToolMessage(r.Call.ID, r.Call.Name, r.text()))
			// 工具内部消耗（子 agent、内部摘要）也计入本次运行。
			res.Usage.Add(r.Result.Usage)
		}

		// 运行中插话：轮次边界把新输入并进上下文。
		if req.Steering != nil {
			for _, m := range req.Steering() {
				if m.Role == core.User && len(m.Content) > 0 {
					msgs = append(msgs, m)
				}
			}
		}

		// 重复调用护栏 + 目标对齐提醒。
		sig := callsSignature(out.toolCalls)
		if sig == lastSig {
			repeatStreak++
		} else {
			repeatStreak = 1
			lastSig = sig
		}
		if repeatStreak >= 2 && !repeatNudged {
			repeatNudged = true
			events.Emit(ctx, handler, &events.GoalAlignmentReminder{Base: nextBase(), Iteration: iter})
			msgs = append(msgs, core.NewUserTextMessage(
				"You are repeating the same tool calls with identical arguments. Re-read the last result: if it will not change, change your approach instead of retrying."))
		} else if l.goalReminderEvery > 0 && iter%l.goalReminderEvery == 0 {
			events.Emit(ctx, handler, &events.GoalAlignmentReminder{Base: nextBase(), Iteration: iter})
		}

		checkpoint()
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

// callsSignature 用于识别「完全相同的连续工具调用」。
func callsSignature(calls []core.ToolCall) string {
	var b strings.Builder
	for _, c := range calls {
		b.WriteString(c.Name)
		b.WriteString("|")
		b.WriteString(c.Arguments)
		b.WriteString(";")
	}
	return b.String()
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
	out, _ := l.compressNow(ctx, msgs, res, handler, nextBase, reason)
	return out
}

// compressNow 强制执行一次压缩（reason 会写进 compress_start 事件）。
// 返回压缩后的上下文与是否成功；失败时原样返回输入。
func (l *AgentLoop) compressNow(ctx context.Context, msgs []core.Message, res *RunResult, handler events.EventHandler, nextBase func() events.Base, reason string) ([]core.Message, bool) {
	if l.compressor == nil {
		return msgs, false
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
		return msgs, false
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
		return msgs, false
	}
	return compressed.Messages, true
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

// executeTools 依次做：只读门禁 → 审批策略 → 引擎分批执行（只读并行、写串行）。
//
// 结果按调用顺序回填，保证事件顺序与模型给出的顺序一致。
func (l *AgentLoop) executeTools(ctx context.Context, calls []core.ToolCall, handler events.EventHandler, nextBase func() events.Base, approver events.Approver, policy events.ApprovalPolicy) []toolOutcome {
	out := make([]toolOutcome, len(calls))
	approved := make([]core.ToolCall, 0, len(calls))
	approvedIdx := make([]int, 0, len(calls))

	for i, call := range calls {
		events.Emit(ctx, handler, &events.ToolRunStart{Base: nextBase(), CallID: call.ID, Name: call.Name, Arguments: call.Arguments})

		readOnly := l.toolReadOnly(call.Name)
		if l.readOnly && !readOnly {
			out[i] = toolOutcome{Call: call, Result: tools.ErrorResult(
				"read-only mode: tool %q is not allowed in this run", call.Name)}
			continue
		}

		needApproval := approver != nil && (policy == nil || policy.RequiresApproval(call.Name, readOnly))
		if !needApproval {
			approved = append(approved, call)
			approvedIdx = append(approvedIdx, i)
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
			out[i] = toolOutcome{Call: call, Result: tools.ErrorResult("%s", msg)}
			continue
		}
		if decision.Arguments != nil {
			call.Arguments = *decision.Arguments
		}
		approved = append(approved, call)
		approvedIdx = append(approvedIdx, i)
	}

	if len(approved) > 0 {
		if l.engine == nil {
			for k, call := range approved {
				out[approvedIdx[k]] = toolOutcome{Call: call, Result: tools.ErrorResult("No tool engine is attached to the runtime")}
			}
		} else {
			for k, r := range l.engine.Execute(ctx, approved) {
				out[approvedIdx[k]] = toolOutcome{Call: r.Call, Result: r.Result}
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
			Usage:      out[i].Result.Usage,
			DurationMS: 0,
		})
	}
	return out
}

// toolReadOnly 把「只读」定义为 CanParallel（不产生副作用、可并发）。
func (l *AgentLoop) toolReadOnly(name string) bool {
	if l.engine == nil {
		return false
	}
	t, ok := l.engine.Lookup(name)
	return ok && t.CanParallel()
}

func textBlocks(text string) []core.Content {
	if text == "" {
		return nil
	}
	return []core.Content{{Type: core.ContentTypeText, Content: text}}
}
