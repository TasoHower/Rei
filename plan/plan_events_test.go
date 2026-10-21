package plan

import (
	"context"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/tools"
)

func TestSecondSubmitEmitsPlanUpdated(t *testing.T) {
	store := NewStore()
	eng := tools.NewEngine()
	if err := Register(eng, store); err != nil {
		t.Fatal(err)
	}
	var seen []events.Event
	ctx := events.WithRunInfo(context.Background(), events.RunInfo{
		RunID: "r1",
		Emit:  func(_ context.Context, e events.Event) { seen = append(seen, e) },
	})

	submit := func(plan string) {
		res := eng.Execute(ctx, []core.ToolCall{{Name: "plan_submit", Arguments: `{"plan":"` + plan + `","steps":[{"title":"a"}]}`}})[0]
		if res.Result.IsError {
			t.Fatalf("提交失败：%s", res.Result.Content)
		}
	}
	submit("第一版")
	submit("第二版")

	if len(seen) != 2 {
		t.Fatalf("事件数 = %d", len(seen))
	}
	if seen[0].Type() != events.PlanGeneratedType {
		t.Fatalf("首次应为 plan_generated：%s", seen[0].Type())
	}
	if seen[1].Type() != events.PlanUpdatedType {
		t.Fatalf("替换计划应为 plan_updated：%s", seen[1].Type())
	}
	updated := seen[1].(*events.PlanUpdated)
	if updated.Plan != "第二版" || updated.Note != "replaced" {
		t.Fatalf("plan_updated 内容 = %+v", updated)
	}
}

func TestReadOnlyOptionIsAvailable(t *testing.T) {
	if ReadOnlyOption() == nil {
		t.Fatal("ReadOnlyOption 不应为 nil")
	}
}
