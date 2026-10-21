package session

import (
	"context"
	"strings"
	"testing"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/subagent"
	"github.com/tasohower/rei/todo"
)

func taskEndForTest(id string) events.TaskEnd {
	return events.TaskEnd{
		Base:   events.Base{RunID: id},
		TaskID: id,
		Status: events.TaskCompleted,
		Result: "done",
	}
}

func TestBackgroundTaskResultIsDeliveredIntoInbox(t *testing.T) {
	ctx := context.Background()
	reg := subagent.NewRegistry()
	defer reg.Close()

	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("收到结果")}}, WithTasks(reg))

	// 后台任务跑完 → 结果被回填成 task_result 消息。
	snap, err := reg.Spawn(ctx, subagent.SpawnRequest{
		Name:   "统计",
		Prompt: "数一数文件",
		Loop:   mustLoop(t, &scriptedProvider{steps: []scriptStep{textStep("42 个文件")}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.Wait(snap.ID)

	if err := s.Ask(ctx, core.NewUserTextMessage("继续")); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, nil); err != nil {
		t.Fatalf("运行失败：%v", err)
	}

	var taskMsg *core.Message
	for i := range s.History() {
		msg := s.History()[i]
		if len(msg.Content) > 0 && msg.Content[0].Type == core.ContentTypeTaskResult {
			taskMsg = &msg
			break
		}
	}
	if taskMsg == nil {
		t.Fatal("任务结果没有回填到会话上下文")
	}
	if !strings.Contains(taskMsg.Text(), "background task") || !strings.Contains(taskMsg.Text(), "42 个文件") {
		t.Fatalf("回填文案异常：%q", taskMsg.Text())
	}
	if taskMsg.Role != core.User {
		t.Fatalf("回填角色 = %q", taskMsg.Role)
	}
}

func TestDeliverTaskResultIgnoresClosedSession(t *testing.T) {
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("x")}})
	_ = s.Shutdown(context.Background(), nil)

	// 关闭后回填应当被丢弃，而不是写入已关闭的队列。
	s.deliverTaskResult(taskEndForTest("t1"))
	if err := s.Ask(context.Background(), core.NewUserTextMessage("还在吗")); err == nil {
		t.Fatal("关闭后的会话不应接受新输入")
	}
}

func TestTodoSurvivesSnapshot(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	todos := todo.NewStore()
	s := newSession(t, &scriptedProvider{steps: []scriptStep{textStep("好")}}, WithStore(store), WithTodo(todos))

	if _, err := todos.Add("把 README 补全"); err != nil {
		t.Fatal(err)
	}
	if err := s.Ask(ctx, core.NewUserTextMessage("开始")); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}

	restored := todo.NewStore()
	_, err := New("s1", mustLoop(t, &scriptedProvider{steps: []scriptStep{textStep("继续")}}), WithStore(store), WithTodo(restored))
	if err != nil {
		t.Fatalf("恢复会话失败：%v", err)
	}
	items := restored.List()
	if len(items) != 1 || items[0].Text != "把 README 补全" {
		t.Fatalf("待办未恢复：%+v", items)
	}
}
