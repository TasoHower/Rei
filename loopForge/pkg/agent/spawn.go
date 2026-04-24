package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

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

type defaultSpawner struct {
	parent  *Agent
	maxDepth int
}

func newDefaultSpawner(parent *Agent, maxDepth int) *defaultSpawner {
	return &defaultSpawner{parent: parent, maxDepth: maxDepth}
}

// Spawn runs the child [RunLoop] in a standalone goroutine; child [event.RuntimeEvent] are not
// forwarded to the parent stream. The parent context is cancelled when the parent is cancelled.
func (s *defaultSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error) {
	if s == nil || s.parent == nil {
		return nil, fmt.Errorf("spawner: nil parent agent")
	}
	if spec == nil {
		return nil, fmt.Errorf("spawner: nil spec")
	}
	if s.parent.ChildAgentBuilder == nil {
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
	if parent.Depth+1 >= s.maxDepth {
		return &exchange.SpawnResult{
			ChildRunRef: childRunRefPlanned(&parent, "-rejected"),
			Status:      exchange.SpawnRejected,
			FinalText:   "",
			Error:       &exchange.SpawnError{Code: "max_depth", Message: "spawn would exceed max depth"},
		}, nil
	}
	childTmpl := s.parent.ChildAgentBuilder(spec)
	if childTmpl == nil {
		return nil, fmt.Errorf("spawner: ChildAgentBuilder returned nil")
	}
	childTmpl = childTmpl.Clone()
	childTmpl = applySpecToChildAgent(childTmpl, spec)

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

	go func() {
		defer close(ch)
		if childTmpl == nil {
			return
		}
		childTmpl.RunLoop(ctx, sub, ch, nil, st)
	}()

	for ev := range ch {
		if ev == nil {
			continue
		}
		if qe := ev.QueryEnd(); qe != nil {
			if qe.Outcome != nil {
				last = qe.Outcome
			}
		}
	}

	if last == nil {
		return &exchange.SpawnResult{
			ChildRunRef: childRef,
			Status:      exchange.SpawnFailed,
			FinalText:   "",
			Error:       &exchange.SpawnError{Code: "no_outcome", Message: "child run produced no terminal outcome"},
		}, nil
	}
	stS := exchange.SpawnCompleted
	if last.Termination != outcome.TerminationCompleted {
		stS = exchange.SpawnFailed
	}
	res := &exchange.SpawnResult{
		ChildRunRef: childRef,
		Status:      stS,
		FinalText:   last.FinalText,
		Metrics:     last.Metrics,
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