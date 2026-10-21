package subagent

import (
	"context"
	"encoding/json"

	"github.com/tasohower/rei/core"
	"github.com/tasohower/rei/events"
	"github.com/tasohower/rei/tools"
)

// Register 把后台任务相关的四个工具注册到引擎上：
// agent_spawn / agent_send / agent_interrupt / task_output。
func Register(eng *tools.Engine, reg *Registry) error {
	for _, t := range []tools.Tool{
		&spawnTool{reg: reg},
		&sendTool{reg: reg},
		&interruptTool{reg: reg},
		&resumeTool{reg: reg},
		&outputTool{reg: reg},
	} {
		if err := eng.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// ------------------------------------------------------------------ spawn

type spawnTool struct{ reg *Registry }

func (t *spawnTool) Name() string { return "agent_spawn" }

func (t *spawnTool) Description() string {
	return "Start a background sub-agent task that keeps running across turns, then return its task_id immediately."
}

func (t *spawnTool) CanParallel() bool { return false }

func (t *spawnTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"Self-contained instruction for the background task."},"name":{"type":"string","description":"Short human-readable task name."}},"required":["task"]}`)
}

func (t *spawnTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		Task string `json:"task"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	info, _ := events.RunInfoFrom(ctx)
	snap, err := t.reg.Spawn(ctx, SpawnRequest{
		Name:        args.Name,
		Prompt:      args.Task,
		Handler:     info.Emit,
		ParentRunID: info.RunID,
		Depth:       info.Depth,
		Deliver:     info.Deliver,
	})
	if err != nil {
		return tools.ErrorResult("spawn failed: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{
		"task_id": snap.ID,
		"status":  snap.Status,
		"name":    snap.Name,
		"output":  snap.OutputFile,
		"hint":    "The task runs in the background. Poll task_output or wait for its result to be delivered.",
	})
	return tools.Result{Content: string(raw)}
}

// ------------------------------------------------------------------- send

type sendTool struct{ reg *Registry }

func (t *sendTool) Name() string { return "agent_send" }

func (t *sendTool) Description() string {
	return "Send an additional instruction to a running background task."
}

func (t *sendTool) CanParallel() bool { return false }

func (t *sendTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"message":{"type":"string"}},"required":["task_id","message"]}`)
}

func (t *sendTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	if err := t.reg.Send(args.TaskID, args.Message); err != nil {
		return tools.ErrorResult("send failed: %v", err)
	}
	return tools.Result{Content: "message queued for " + args.TaskID}
}

// -------------------------------------------------------------- interrupt

type interruptTool struct{ reg *Registry }

func (t *interruptTool) Name() string { return "agent_interrupt" }

func (t *interruptTool) Description() string {
	return "Interrupt a running background task. Its final status becomes interrupted."
}

func (t *interruptTool) CanParallel() bool { return false }

func (t *interruptTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`)
}

func (t *interruptTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	if err := t.reg.Interrupt(args.TaskID); err != nil {
		return tools.ErrorResult("interrupt failed: %v", err)
	}
	return tools.Result{Content: "interrupt requested for " + args.TaskID}
}

// ----------------------------------------------------------------- resume

type resumeTool struct{ reg *Registry }

func (t *resumeTool) Name() string { return "agent_resume" }

func (t *resumeTool) Description() string {
	return "Resume a finished background task with its original context and an optional follow-up instruction."
}

func (t *resumeTool) CanParallel() bool { return false }

func (t *resumeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"message":{"type":"string","description":"Optional follow-up instruction."}},"required":["task_id"]}`)
}

func (t *resumeTool) Call(ctx context.Context, call core.ToolCall) tools.Result {
	var args struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}
	snap, err := t.reg.Resume(ctx, args.TaskID, args.Message)
	if err != nil {
		return tools.ErrorResult("resume failed: %v", err)
	}
	raw, _ := json.Marshal(snap)
	return tools.Result{Content: string(raw)}
}

// ----------------------------------------------------------------- output

type outputTool struct{ reg *Registry }

func (t *outputTool) Name() string { return "task_output" }

func (t *outputTool) Description() string {
	return "Read a background task's status and journal records (mode: status, latest or all)."
}

func (t *outputTool) CanParallel() bool { return true }

func (t *outputTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string","description":"Task id, or empty to list all tasks."},"mode":{"type":"string","enum":["status","latest","all"],"description":"Defaults to status."}}}`)
}

func (t *outputTool) Call(_ context.Context, call core.ToolCall) tools.Result {
	var args struct {
		TaskID string `json:"task_id"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(defaultJSON(call.Arguments)), &args); err != nil {
		return tools.ErrorResult("invalid arguments: %v", err)
	}

	if args.TaskID == "" {
		raw, _ := json.Marshal(t.reg.List())
		return tools.Result{Content: string(raw)}
	}

	snap, err := t.reg.Snapshot(args.TaskID)
	if err != nil {
		return tools.ErrorResult("task_output failed: %v", err)
	}
	if args.Mode == "" || args.Mode == "status" {
		raw, _ := json.Marshal(snap)
		return tools.Result{Content: string(raw)}
	}
	records, err := t.reg.Records(args.TaskID, args.Mode)
	if err != nil {
		return tools.ErrorResult("task_output failed: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"task": snap, "records": records})
	return tools.Result{Content: string(raw)}
}
