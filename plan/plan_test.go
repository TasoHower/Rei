package plan

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/tools"
)

func TestSubmitNormalizesSteps(t *testing.T) {
	store := NewStore()
	got, err := store.Submit(context.Background(), "先读代码再改", []Step{
		{Title: " 读 agents/loop.go ", Detail: "看重试"},
		{Title: "加重试测试"},
	})
	if err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	if got.Revision != 1 || len(got.Steps) != 2 {
		t.Fatalf("计划 = %+v", got)
	}
	if got.Steps[0].Index != 1 || got.Steps[0].Title != "读 agents/loop.go" {
		t.Fatalf("步骤未归一化：%+v", got.Steps[0])
	}
	if got.Steps[1].Status != StatusPending {
		t.Fatalf("新计划步骤应为 pending：%+v", got.Steps[1])
	}
}

func TestSubmitValidation(t *testing.T) {
	store := NewStore()
	if _, err := store.Submit(context.Background(), "  ", []Step{{Title: "a"}}); !errors.Is(err, ErrEmptyPlan) {
		t.Fatalf("空计划应报错，得到 %v", err)
	}
	if _, err := store.Submit(context.Background(), "x", nil); !errors.Is(err, ErrNoSteps) {
		t.Fatalf("无步骤应报错，得到 %v", err)
	}
	tooMany := make([]Step, MaxSteps+1)
	for i := range tooMany {
		tooMany[i].Title = "step"
	}
	if _, err := store.Submit(context.Background(), "x", tooMany); !errors.Is(err, ErrTooMany) {
		t.Fatalf("步骤过多应报错，得到 %v", err)
	}
	if _, err := store.Submit(context.Background(), "x", []Step{{Detail: "无标题"}}); err == nil {
		t.Fatal("缺少标题应报错")
	}
}

func TestTransitions(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	if _, err := store.Start(ctx, 1); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("未提交时更新应报错，得到 %v", err)
	}
	if _, err := store.Submit(ctx, "计划", []Step{{Title: "a"}, {Title: "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	got, err := store.Complete(ctx, 1, StatusCompleted, "已验证")
	if err != nil {
		t.Fatal(err)
	}
	if got.Steps[0].Status != StatusCompleted || got.Steps[0].Note != "已验证" || got.Revision != 3 {
		t.Fatalf("状态 = %+v revision=%d", got.Steps[0], got.Revision)
	}
	if _, err := store.Complete(ctx, 9, StatusCompleted, ""); !errors.Is(err, ErrBadIndex) {
		t.Fatalf("越界步骤应报错，得到 %v", err)
	}
	if _, err := store.Complete(ctx, 2, StatusPending, ""); !errors.Is(err, ErrBadStatus) {
		t.Fatalf("非法终态应报错，得到 %v", err)
	}
}

func TestPromptFragmentIsEnglishAndHasStatuses(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	if frag := store.PromptFragment(); frag != "" {
		t.Fatalf("空计划不应注入片段：%q", frag)
	}
	_, _ = store.Submit(ctx, "计划", []Step{{Title: "read code"}, {Title: "add tests"}})
	_, _ = store.Complete(ctx, 1, StatusCompleted, "verified")

	frag := store.PromptFragment()
	if !strings.Contains(frag, "Current plan:") || !strings.Contains(frag, "[completed] read code") {
		t.Fatalf("片段内容异常：%q", frag)
	}
	for _, r := range frag {
		if r > unicode.MaxASCII {
			t.Fatalf("计划片段必须是英文：%q", string(r))
		}
	}
}

func TestToolsEmitEventsWithRunID(t *testing.T) {
	store := NewStore()
	eng := tools.NewEngine()
	if err := Register(eng, store); err != nil {
		t.Fatal(err)
	}

	var seen []events.Event
	ctx := events.WithRunInfo(context.Background(), events.RunInfo{
		RunID: "run-1", Depth: 0,
		Emit: func(_ context.Context, e events.Event) { seen = append(seen, e) },
	})

	res := eng.Execute(ctx, []core.ToolCall{{ID: "c1", Name: "plan_submit", Arguments: `{"plan":"两步走","steps":[{"title":"read"},{"title":"write"}]}`}})[0]
	if res.Result.IsError {
		t.Fatalf("提交工具失败：%s", res.Result.Content)
	}
	if len(seen) != 1 || seen[0].Type() != events.PlanGeneratedType {
		t.Fatalf("缺少 plan_generated 事件：%+v", seen)
	}
	if seen[0].Envelope().RunID != "run-1" {
		t.Fatalf("事件未带 run_id：%+v", seen[0].Envelope())
	}

	res = eng.Execute(ctx, []core.ToolCall{{ID: "c2", Name: "plan_update", Arguments: `{"step":1,"status":"in_progress"}`}})[0]
	if res.Result.IsError {
		t.Fatalf("更新工具失败：%s", res.Result.Content)
	}
	res = eng.Execute(ctx, []core.ToolCall{{ID: "c3", Name: "plan_update", Arguments: `{"step":1,"status":"completed","note":"done"}`}})[0]
	if res.Result.IsError {
		t.Fatalf("更新工具失败：%s", res.Result.Content)
	}
	if len(seen) != 3 || seen[1].Type() != events.PlanStepStartedType || seen[2].Type() != events.PlanStepCompletedType {
		t.Fatalf("步骤事件异常：%+v", seen)
	}

	var snap Plan
	if err := json.Unmarshal([]byte(res.Result.Content), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Steps[0].Status != StatusCompleted {
		t.Fatalf("工具返回的快照未更新：%+v", snap)
	}
}

func TestUpdateToolRejectsBadArguments(t *testing.T) {
	store := NewStore()
	eng := tools.NewEngine()
	_ = Register(eng, store)
	res := eng.Execute(context.Background(), []core.ToolCall{{Name: "plan_update", Arguments: `{"step":1,"status":"in_progress"}`}})[0]
	if !res.Result.IsError {
		t.Fatal("未提交计划时更新应返回错误")
	}
}
