package plan

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/tools"
)

// Tools 返回 Plan 模式的两个工具：plan_submit 与 plan_update。
func Tools(store *Store) []tools.Tool {
	return []tools.Tool{&submitTool{store: store}, &updateTool{store: store}}
}

// Register 注册 Plan 工具。
func Register(eng *tools.Engine, store *Store) error {
	for _, t := range Tools(store) {
		if err := eng.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- submit

type submitTool struct{ store *Store }

func (t *submitTool) Name() string { return "plan_submit" }

func (t *submitTool) Description() string {
	return "Submit or replace the execution plan. Use it before starting a multi-step task, and keep steps small and verifiable."
}

func (t *submitTool) CanParallel() bool { return false }

func (t *submitTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"plan":{"type":"string","description":"One-paragraph description of the overall approach."},"steps":{"type":"array","description":"Ordered steps.","items":{"type":"object","properties":{"title":{"type":"string"},"detail":{"type":"string"}},"required":["title"]}}},"required":["plan","steps"]}`)
}

func (t *submitTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Plan  string `json:"plan"`
		Steps []Step `json:"steps"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}
	submitted, err := t.store.Submit(ctx, args.Plan, args.Steps)
	if err != nil {
		return tools.ErrorResult("%v", err)
	}

	base := baseFrom(ctx)
	if submitted.Revision > 1 {
		// 替换既有计划：发 plan_updated 而不是再来一次 plan_generated。
		events.Emit(ctx, emitFrom(ctx), &events.PlanUpdated{
			Base:  base,
			Plan:  submitted.Text,
			Steps: toEventSteps(submitted.Steps),
			Note:  "replaced",
		})
	} else {
		events.Emit(ctx, emitFrom(ctx), &events.PlanGenerated{
			Base:  base,
			Plan:  submitted.Text,
			Steps: toEventSteps(submitted.Steps),
		})
	}
	raw, _ := json.Marshal(submitted)
	return tools.Result{Content: string(raw), Metadata: map[string]any{"steps": len(submitted.Steps)}}
}

// ---------------------------------------------------------------- update

type updateTool struct{ store *Store }

func (t *updateTool) Name() string { return "plan_update" }

func (t *updateTool) Description() string {
	return "Update one plan step: mark it in progress, completed, failed or skipped."
}

func (t *updateTool) CanParallel() bool { return false }

func (t *updateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"step":{"type":"integer","description":"1-based step index from plan_submit."},"status":{"type":"string","enum":["in_progress","completed","failed","skipped"]},"note":{"type":"string","description":"Short evidence or reason."}},"required":["step","status"]}`)
}

func (t *updateTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Step   int    `json:"step"`
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decode(call, &args); err != nil {
		return tools.ErrorResult("%v", err)
	}

	var (
		updated Plan
		err     error
	)
	if args.Status == StatusInProgress {
		updated, err = t.store.Start(ctx, args.Step)
	} else {
		updated, err = t.store.Complete(ctx, args.Step, args.Status, args.Note)
	}
	if err != nil {
		return tools.ErrorResult("%v", err)
	}

	base := baseFrom(ctx)
	title := stepTitle(updated, args.Step)
	if args.Status == StatusInProgress {
		events.Emit(ctx, emitFrom(ctx), &events.PlanStepStarted{Base: base, Index: args.Step, Title: title})
	} else {
		events.Emit(ctx, emitFrom(ctx), &events.PlanStepCompleted{
			Base: base, Index: args.Step, Title: title, Status: args.Status, Note: args.Note,
		})
	}
	raw, _ := json.Marshal(updated)
	return tools.Result{Content: string(raw)}
}

// ---------------------------------------------------------------- 辅助

func decode(call core.ToolCall, v any) error {
	raw := strings.TrimSpace(call.Arguments)
	if raw == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return err
	}
	return nil
}

func baseFrom(ctx context.Context) events.Base {
	info, _ := events.RunInfoFrom(ctx)
	return events.Base{RunID: info.RunID, ParentRunID: info.ParentRunID, Depth: info.Depth, Time: time.Now()}
}

func emitFrom(ctx context.Context) events.EventHandler {
	info, _ := events.RunInfoFrom(ctx)
	return info.Emit
}

func toEventSteps(steps []Step) []events.PlanStep {
	out := make([]events.PlanStep, 0, len(steps))
	for _, s := range steps {
		out = append(out, events.PlanStep{Index: s.Index, Title: s.Title, Detail: s.Detail})
	}
	return out
}

func stepTitle(p Plan, index int) string {
	for _, s := range p.Steps {
		if s.Index == index {
			return s.Title
		}
	}
	return ""
}
