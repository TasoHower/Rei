package todo

import (
	"context"
	"strings"
	"testing"
	"unicode"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/tools"
)

func TestAddUpdateList(t *testing.T) {
	store := NewStore()
	a, err := store.Add(" 写测试 ")
	if err != nil || a.ID != 1 || a.Text != "写测试" || a.Status != StatusPending {
		t.Fatalf("新增 = %+v, err=%v", a, err)
	}
	if _, err := store.Add("  "); err == nil {
		t.Fatal("空内容应报错")
	}
	if _, err := store.Update(1, StatusDone); err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 1 || got[0].Status != StatusDone {
		t.Fatalf("列表 = %+v", got)
	}
	if _, err := store.Update(9, StatusDone); err == nil {
		t.Fatal("不存在的 id 应报错")
	}
	if _, err := store.Update(1, "unknown"); err == nil {
		t.Fatal("非法状态应报错")
	}
}

func TestRestoreKeepsNextID(t *testing.T) {
	store := NewStore()
	store.Restore([]Item{{ID: 3, Text: "旧任务", Status: StatusDone}})
	item, err := store.Add("新任务")
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != 4 {
		t.Fatalf("restore 后新 id = %d，期望 4", item.ID)
	}
}

func TestPromptFragmentIsEnglish(t *testing.T) {
	store := NewStore()
	if store.PromptFragment() != "" {
		t.Fatal("空列表不应注入片段")
	}
	_, _ = store.Add("write tests")
	frag := store.PromptFragment()
	if !strings.Contains(frag, "[pending] write tests") {
		t.Fatalf("片段 = %q", frag)
	}
	for _, r := range frag {
		if r > unicode.MaxASCII {
			t.Fatalf("片段必须英文：%q", string(r))
		}
	}
}

func TestTools(t *testing.T) {
	store := NewStore()
	eng := tools.NewEngine()
	if err := Register(eng, store); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	eng.Execute(ctx, []core.ToolCall{{Name: "todo_add", Arguments: `{"text":"a"}`}})
	eng.Execute(ctx, []core.ToolCall{{Name: "todo_add", Arguments: `{"text":"b"}`}})
	res := eng.Execute(ctx, []core.ToolCall{{Name: "todo_update", Arguments: `{"id":2,"status":"done"}`}})[0]
	if res.Result.IsError || !strings.Contains(res.Result.Content, `"done"`) {
		t.Fatalf("更新结果 = %+v", res.Result)
	}
	list := eng.Execute(ctx, []core.ToolCall{{Name: "todo_list", Arguments: `{}`}})[0]
	if !strings.Contains(list.Result.Content, `"total"`) && !strings.Contains(list.Result.Content, `"a"`) {
		t.Fatalf("列表结果 = %q", list.Result.Content)
	}
}
