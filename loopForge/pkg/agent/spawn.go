package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"loopforge/pkg/log"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/exchange"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/variable"
)

// Spawner materializes a child [Agent] loop. Default implementation is [defaultSpawner]
// and matches the contract in [loopforge/internal/engine] where applicable.
type Spawner interface {
	Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error)
}

// DefaultSpawner wraps the parent Agent with spawn configuration.
type DefaultSpawner struct {
	Parent   *Agent
	MaxDepth int
}

// NewDefaultSpawner creates a Spawner that builds child agents from the parent's
// ChildAgentBuilder and runs them in isolated goroutines.
func NewDefaultSpawner(parent *Agent, maxDepth int) Spawner {
	return &DefaultSpawner{Parent: parent, MaxDepth: maxDepth}
}

// Spawn runs the child [RunLoop] in a standalone goroutine; child [event.RuntimeEvent] are not
// forwarded to the parent stream. The parent context is cancelled when the parent is cancelled.
func (s *DefaultSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error) {
	if s == nil || s.Parent == nil {
		return nil, fmt.Errorf("spawner: nil parent agent")
	}

	if spec == nil {
		return nil, fmt.Errorf("spawner: nil spec")
	}

	if s.Parent.ChildAgentBuilder == nil {
		return nil, fmt.Errorf("spawner: ChildAgentBuilder is nil")
	}
	if spec.Lifecycle != "" && spec.Lifecycle != exchange.LifecycleEphemeral {
		if spec.Lifecycle == exchange.LifecycleLinger {
			return &exchange.SpawnResult{
				ChildRunRef: exchange.RunRef{RunID: parent.RunID + "-rejected", ParentRunID: parent.RunID, Depth: parent.Depth + 1},
				Status:      exchange.SpawnRejected,
				FinalText:   "",
				Error:       &exchange.SpawnError{Code: "unsupported_lifecycle", Message: "linger is not implemented in this version"},
			}, nil
		}
		return &exchange.SpawnResult{
			ChildRunRef: exchange.RunRef{RunID: parent.RunID + "-rejected", ParentRunID: parent.RunID, Depth: parent.Depth + 1},
			Status:      exchange.SpawnRejected,
			FinalText:   "",
			Error:       &exchange.SpawnError{Code: "unsupported_lifecycle", Message: string(spec.Lifecycle)},
		}, nil
	}
	if parent.Depth+1 >= s.MaxDepth {
		return &exchange.SpawnResult{
			ChildRunRef: childRunRefPlanned(&parent, "-rejected"),
			Status:      exchange.SpawnRejected,
			FinalText:   "",
			Error:       &exchange.SpawnError{Code: "max_depth", Message: "spawn would exceed max depth"},
		}, nil
	}
	childTmpl := s.Parent.ChildAgentBuilder(spec)
	if childTmpl == nil {
		return nil, fmt.Errorf("spawner: ChildAgentBuilder returned nil")
	}
	childTmpl = childTmpl.Clone()
	childTmpl = applySpecToChildAgent(childTmpl, spec)

	// Force non-streaming on child ChatModel (§6)
	if childTmpl.ChatModel != nil {
		childTmpl.ChatModel = model.WrapNonStream(childTmpl.ChatModel)
	}

	runID := fmt.Sprintf("%s-spawn-%d", parent.RunID, time.Now().UnixNano())
	childRef := exchange.RunRef{
		RunID:       runID,
		ParentRunID: parent.RunID,
		AgentRole:   childTmpl.Name,
		Depth:       parent.Depth + 1,
	}

	sub := &request.RuntimeRequest{
		SessionID:   runID,
		UserMessage: spec.Task,
		Options:     request.RuntimeOptions{SkillIDs: spec.SkillIDs, MaxSteps: spec.LoopOverrides.MaxSteps},
	}
	if spec.ModelOverride != "" {
		sub.Options.Model = spec.ModelOverride
	}

	st := &LoopState{
		VarStore:      variable.New(),
		CurrentRunRef: &childRef,
	}

	ch := make(chan *event.RuntimeEvent, 256)
	var last *outcome.RuntimeOutcome

	// Capture child tool calls in order for inclusion in SpawnResult
	type pendingToolCall struct {
		name      string
		arguments string
	}
	pendingTC := make(map[string]pendingToolCall) // tool_call_id → pending
	var childToolCalls []exchange.ChildToolCall
	var tcMu sync.Mutex

	// Derive a child context so we can cancel independently
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	go func() {
		defer close(ch)
		if childTmpl == nil {
			return
		}
		childTmpl.RunLoop(childCtx, sub, ch, nil, st)
	}()

	// Blind spot #1 fix: ctx-aware event loop instead of bare for-range
eventLoop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break eventLoop
			}

			// 将子 Agent 启动事件转发到父事件流（可观测性增强，不破坏上下文隔离）
			if se := ev.Start(); se != nil && spec.OutputCh != nil {
				taskSummary := spec.Task
				if len(taskSummary) > 200 {
					taskSummary = taskSummary[:200] + "..."
				}
				select {
				case spec.OutputCh <- &event.RuntimeEvent{
					Type:  event.EventSpawnStart,
					RunID: parent.RunID,
					Step:  0,
					Payload: &event.SpawnStartPayload{
						ChildRunID:  childRef.RunID,
						ParentRunID: parent.RunID,
						AgentRole:   childTmpl.Name,
						Depth:       childRef.Depth,
						TaskSummary: taskSummary,
					},
				}:
				default:
				}
			}

			if qe := ev.QueryEnd(); qe != nil {
				if qe.Outcome != nil {
					last = qe.Outcome
				}
				// 将子 Agent 结束事件转发到父事件流
				if spec.OutputCh != nil {
					childOK := last != nil && last.Termination == outcome.TerminationCompleted
					spawnStatus := exchange.SpawnFailed
					if childOK {
						spawnStatus = exchange.SpawnCompleted
					}
					select {
					case spec.OutputCh <- &event.RuntimeEvent{
						Type:  event.EventSpawnEnd,
						RunID: parent.RunID,
						Step:  0,
						Payload: &event.SpawnEndPayload{
							ChildRunID:   childRef.RunID,
							ParentRunID:  parent.RunID,
							AgentRole:    childTmpl.Name,
							Depth:        childRef.Depth,
							OK:           childOK,
							Status:       string(spawnStatus),
							FinalTextLen: len(last.FinalText),
							Metrics:      &last.Metrics,
						},
					}:
					default:
					}
				}
				continue
			}
			if cs := ev.ToolCallStart(); cs != nil {
				tcMu.Lock()
				pendingTC[cs.ToolCallID] = pendingToolCall{name: cs.Name, arguments: cs.Arguments}
				tcMu.Unlock()
			}
			if ce := ev.ToolCallEnd(); ce != nil {
				tcMu.Lock()
				p := pendingTC[ce.ToolCallID]
				delete(pendingTC, ce.ToolCallID)
				tcMu.Unlock()
				childToolCalls = append(childToolCalls, exchange.ChildToolCall{
					Name:      p.name,
					Arguments: p.arguments,
					Output:    ce.Output,
					IsError:   ce.IsError,
				})
			}
		case <-childCtx.Done():
			log.Default().Debug("spawn event loop exiting on ctx cancellation",
				"child_run_id", childRef.RunID,
				"parent_run_id", parent.RunID,
			)
			break eventLoop
		}
	}

	// 父级取消导致子 Agent 未正常结束：发送失败 end 事件
	if last == nil && spec.OutputCh != nil {
		select {
		case spec.OutputCh <- &event.RuntimeEvent{
			Type:  event.EventSpawnEnd,
			RunID: parent.RunID,
			Step:  0,
			Payload: &event.SpawnEndPayload{
				ChildRunID:  childRef.RunID,
				ParentRunID: parent.RunID,
				AgentRole:   childTmpl.Name,
				Depth:       childRef.Depth,
				OK:          false,
				Status:      string(exchange.SpawnFailed),
				ErrorCode:   "parent_cancelled",
			},
		}:
		default:
		}
	}

	if last == nil {
		code := "no_outcome"
		if childCtx.Err() != nil {
			code = "parent_cancelled"
		}
		return &exchange.SpawnResult{
			ChildRunRef: childRef,
			Status:      exchange.SpawnFailed,
			FinalText:   "",
			Error:       &exchange.SpawnError{Code: code, Message: "child run terminated early"},
		}, nil
	}
	stS := exchange.SpawnCompleted
	if last.Termination != outcome.TerminationCompleted {
		stS = exchange.SpawnFailed
	}
	res := &exchange.SpawnResult{
		ChildRunRef:    childRef,
		Status:         stS,
		FinalText:      last.FinalText,
		Metrics:        last.Metrics,
		ChildToolCalls: childToolCalls,
	}
	if stS == exchange.SpawnFailed {
		res.Error = &exchange.SpawnError{Code: string(last.Termination), Message: "child run terminated: " + string(last.Termination)}
	}
	return res, nil
}

// applySpecToChildAgent mutates a cloned [Agent] from [SpawnSpec] (Slice1: no parent transcript; optional digest and addendum).
func applySpecToChildAgent(a *Agent, spec *exchange.SpawnSpec) *Agent {
	extra := formatMemoryDigest(spec.MemoryDigest)
	if strings.TrimSpace(spec.SystemAddendum) != "" {
		if extra != "" {
			extra = extra + "\n\n" + spec.SystemAddendum
		} else {
			extra = spec.SystemAddendum
		}
	}
	if extra != "" {
		if a.SystemInstructions != "" {
			a.SystemInstructions = strings.TrimSpace(a.SystemInstructions) + "\n\n" + extra
		} else {
			a.SystemInstructions = extra
		}
	}
	return a
}

func formatMemoryDigest(d *exchange.MemoryDigest) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("<short_term_memory>\n")
	if s := strings.TrimSpace(d.Summary); s != "" {
		b.WriteString("summary: ")
		b.WriteString(s)
		b.WriteRune('\n')
	}
	if len(d.KVs) > 0 {
		for k, v := range d.KVs {
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteRune('\n')
		}
	}
	b.WriteString("</short_term_memory>\n")
	return b.String()
}

func childRunRefPlanned(p *exchange.RunRef, suffix string) exchange.RunRef {
	if p == nil {
		return exchange.RunRef{RunID: "child" + suffix, Depth: 0}
	}
	return exchange.RunRef{RunID: p.RunID + suffix, ParentRunID: p.RunID, Depth: p.Depth + 1}
}
