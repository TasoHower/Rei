package agents

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/provider"
)

// modelOutcome 是一轮模型调用的汇总。
type modelOutcome struct {
	text      string
	reasoning string
	toolCalls []core.ToolCall
	finish    string
	usage     core.Usage
}

// streamOnce 在可重试错误上做指数退避重试。
//
// 只有「完全没拿到内容」的失败才重试：一旦已经向宿主发出了正文增量，
// 重试就会造成重复输出，此时宁可失败。
func (l *AgentLoop) streamOnce(ctx context.Context, msgs []core.Message, handler events.EventHandler, runID string, idx *int) (*modelOutcome, error) {
	attempts := l.backoff.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := l.backoff.Sleep(ctx, attempt-1); err != nil {
				return nil, err
			}
		}
		out, err := l.streamAttempt(ctx, msgs, handler, runID, idx)
		if err == nil {
			return out, nil
		}
		lastErr = err

		pe := provider.Classify(err)
		retryable := pe.Retryable() && attempt < attempts
		events.Emit(ctx, handler, &events.LLMError{
			Base:      l.base(runID, idx),
			Kind:      string(pe.Kind),
			Message:   pe.Message,
			Retryable: retryable,
			Attempt:   attempt,
		})
		if !retryable {
			return nil, err
		}
	}
	return nil, lastErr
}

// streamAttempt 发起一次模型调用，并施加两个看门狗：
// 首字节超时（连不上或上游卡住）与流中段停滞（连上后不再吐字）。
func (l *AgentLoop) streamAttempt(ctx context.Context, msgs []core.Message, handler events.EventHandler, runID string, idx *int) (*modelOutcome, error) {
	req := provider.Request{
		Model:     l.model,
		System:    l.system,
		Messages:  msgs,
		MaxTokens: l.maxToken,
	}
	if l.engine != nil {
		for _, spec := range l.engine.Specs() {
			req.Tools = append(req.Tools, provider.Tool{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  spec.Parameters,
			})
		}
	}

	events.Emit(ctx, handler, &events.LLMStart{Base: l.base(runID, idx), Model: l.model, MessageCount: len(msgs)})
	start := time.Now()

	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	watchdog := time.AfterFunc(l.firstChunk, cancel)
	defer watchdog.Stop()

	stream, err := l.prov.Stream(attemptCtx, req)
	if err != nil {
		if attemptCtx.Err() != nil && ctx.Err() == nil {
			return nil, &provider.Error{Kind: provider.KindUpstream, Message: "建立连接超时"}
		}
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	out := &modelOutcome{}
	var text, reasoning strings.Builder
	first := true

	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if attemptCtx.Err() != nil && ctx.Err() == nil {
				msg := "流停滞超时"
				if first {
					msg = "首字节超时"
				}
				return nil, &provider.Error{Kind: provider.KindUpstream, Message: msg}
			}
			return nil, err
		}
		if first {
			first = false
			watchdog.Stop()
			watchdog = time.AfterFunc(l.stallTimeout, cancel)
		} else {
			watchdog.Stop()
			watchdog.Reset(l.stallTimeout)
		}

		switch chunk.Kind {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			events.Emit(ctx, handler, &events.ContentChunk{Base: l.base(runID, idx), Text: chunk.Text})
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
			events.Emit(ctx, handler, &events.ReasoningChunk{Base: l.base(runID, idx), Text: chunk.Text})
		case provider.ChunkToolCall:
			if chunk.ToolCall != nil {
				out.toolCalls = append(out.toolCalls, *chunk.ToolCall)
			}
		case provider.ChunkDone:
			out.finish = chunk.FinishReason
			if chunk.Usage != nil {
				out.usage = *chunk.Usage
			}
		}
	}

	out.text = text.String()
	out.reasoning = reasoning.String()
	if out.finish == "" {
		out.finish = "stop"
	}
	if l.pricing != nil {
		out.usage = core.PricedUsage(out.usage, *l.pricing)
	}

	events.Emit(ctx, handler, &events.LLMEnd{
		Base:         l.base(runID, idx),
		Model:        l.model,
		FinishReason: out.finish,
		Usage:        out.usage,
		DurationMS:   time.Since(start).Milliseconds(),
	})
	return out, nil
}

// base 生成事件信封。idx 由 Run 持有，保证跨轮连续递增。
func (l *AgentLoop) base(runID string, idx *int) events.Base {
	*idx = *idx + 1
	return events.Base{RunID: runID, Index: *idx, Time: time.Now()}
}
